package geo

import (
	"context"
	"fmt"
	"time"

	"github.com/qorm/burin/cProtocol"
	"github.com/qorm/burin/client/interfaces"

	"github.com/bytedance/sonic"
	"github.com/sirupsen/logrus"
)

// Client GEO 客户端实现
type Client struct {
	protocolClient *cProtocol.Client
	config         *Config
	logger         *logrus.Logger
}

// Config GEO 客户端配置
type Config struct {
	DefaultDatabase string        `json:"default_database" yaml:"default_database"`
	DefaultTimeout  time.Duration `json:"default_timeout" yaml:"default_timeout"`
	RetryCount      int           `json:"retry_count" yaml:"retry_count"`
	RetryDelay      time.Duration `json:"retry_delay" yaml:"retry_delay"`
}

// DefaultConfig 返回默认 GEO 配置
func DefaultConfig() *Config {
	return &Config{
		DefaultDatabase: "default",
		DefaultTimeout:  10 * time.Second,
		RetryCount:      3,
		RetryDelay:      100 * time.Millisecond,
	}
}

// NewClient 创建 GEO 客户端
func NewClient(protocolClient *cProtocol.Client, config *Config, logger *logrus.Logger) interfaces.GeoInterface {
	if config == nil {
		config = DefaultConfig()
	}

	if logger == nil {
		logger = logrus.New()
	}

	return &Client{
		protocolClient: protocolClient,
		config:         config,
		logger:         logger,
	}
}

// parseGeoOptions 解析 GEO 选项
func (c *Client) parseGeoOptions(opts ...interfaces.GeoOption) *interfaces.GeoOptions {
	options := &interfaces.GeoOptions{
		Database:   c.config.DefaultDatabase,
		Timeout:    c.config.DefaultTimeout,
		Consistent: false,
	}

	for _, opt := range opts {
		opt(options)
	}

	return options
}

// GeoAdd 添加地理位置信息
func (c *Client) GeoAdd(key string, members []interfaces.GeoMember, opts ...interfaces.GeoOption) error {
	options := c.parseGeoOptions(opts...)

	// 转换成员类型
	geoMembers := make([]cProtocol.GeoMember, len(members))
	for i, m := range members {
		geoMembers[i] = cProtocol.GeoMember{
			Member:    m.Name,
			Longitude: m.Longitude,
			Latitude:  m.Latitude,
			Metadata:  convertMetadata(m.Metadata),
		}
	}

	// 构造 GEO 请求
	geoReq := &cProtocol.GeoRequest{
		Operation: "geoadd",
		Database:  options.Database,
		Key:       key,
		Members:   geoMembers,
	}

	data, err := sonic.Marshal(geoReq)
	if err != nil {
		c.logger.Errorf("failed to marshal geo request: %v", err)
		return fmt.Errorf("failed to marshal geo request: %w", err)
	}

	// 创建 GEO 命令头
	head := cProtocol.CreateGeoCommandHead(cProtocol.GeoCommandAdd)
	head.SetConfig(cProtocol.REQ, cProtocol.HaveResponse, uint8(cProtocol.CommandTypeGeo))

	protocolReq := &cProtocol.ProtocolRequest{
		Head: head,
		Data: data,
	}

	// 发送请求
	ctx, cancel := context.WithTimeout(context.Background(), options.Timeout)
	defer cancel()

	resp, err := c.protocolClient.SendRequest(ctx, protocolReq)
	if err != nil {
		c.logger.Errorf("failed to send geo add request: %v", err)
		return fmt.Errorf("failed to send geo add request: %w", err)
	}

	// 解析响应
	geoResp := &cProtocol.GeoResponse{}
	if err := sonic.Unmarshal(resp.Data, geoResp); err != nil {
		c.logger.Errorf("failed to unmarshal geo response: %v", err)
		return fmt.Errorf("failed to unmarshal geo response: %w", err)
	}

	if !geoResp.Success {
		return fmt.Errorf("geo add failed: %s", geoResp.Error)
	}

	return nil
}

// GeoDist 计算两个地理位置之间的距离
func (c *Client) GeoDist(key, member1, member2, unit string, opts ...interfaces.GeoOption) (float64, error) {
	options := c.parseGeoOptions(opts...)

	// 构造 GEO 请求
	geoReq := &cProtocol.GeoRequest{
		Operation: "geodist",
		Database:  options.Database,
		Key:       key,
		Members: []cProtocol.GeoMember{
			{Member: member1},
			{Member: member2},
		},
		Unit: unit,
	}

	data, err := sonic.Marshal(geoReq)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal geo request: %w", err)
	}

	// 创建 GEO 命令头
	head := cProtocol.CreateGeoCommandHead(cProtocol.GeoCommandDist)
	head.SetConfig(cProtocol.REQ, cProtocol.HaveResponse, uint8(cProtocol.CommandTypeGeo))

	protocolReq := &cProtocol.ProtocolRequest{
		Head: head,
		Data: data,
	}

	// 发送请求
	ctx, cancel := context.WithTimeout(context.Background(), options.Timeout)
	defer cancel()

	resp, err := c.protocolClient.SendRequest(ctx, protocolReq)
	if err != nil {
		return 0, fmt.Errorf("failed to send geo dist request: %w", err)
	}

	// 解析响应
	geoResp := &cProtocol.GeoResponse{}
	if err := sonic.Unmarshal(resp.Data, geoResp); err != nil {
		return 0, fmt.Errorf("failed to unmarshal geo response: %w", err)
	}

	if !geoResp.Success {
		return 0, fmt.Errorf("geo dist failed: %s", geoResp.Error)
	}

	// 提取距离值 - Result[0] 应该包含距离
	if len(geoResp.Result) > 0 && geoResp.Result[0].Distance > 0 {
		return geoResp.Result[0].Distance, nil
	}

	return 0, fmt.Errorf("invalid distance value")
}

// GeoRadius 查询指定范围内的地理位置
func (c *Client) GeoRadius(key string, lon, lat, radius float64, unit string, opts ...interfaces.GeoOption) ([]interfaces.GeoResult, error) {
	options := c.parseGeoOptions(opts...)

	// 构造 GEO 请求
	geoReq := &cProtocol.GeoRequest{
		Operation: "georadius",
		Database:  options.Database,
		Key:       key,
		Longitude: lon,
		Latitude:  lat,
		Radius:    radius,
		Unit:      unit,
		Prefix:    options.Prefix, // 添加前缀过滤
	}

	data, err := sonic.Marshal(geoReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal geo request: %w", err)
	}

	// 创建 GEO 命令头
	head := cProtocol.CreateGeoCommandHead(cProtocol.GeoCommandRadius)
	head.SetConfig(cProtocol.REQ, cProtocol.HaveResponse, uint8(cProtocol.CommandTypeGeo))

	protocolReq := &cProtocol.ProtocolRequest{
		Head: head,
		Data: data,
	}

	// 发送请求
	ctx, cancel := context.WithTimeout(context.Background(), options.Timeout)
	defer cancel()

	resp, err := c.protocolClient.SendRequest(ctx, protocolReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send geo radius request: %w", err)
	}

	// 解析响应
	geoResp := &cProtocol.GeoResponse{}
	if err := sonic.Unmarshal(resp.Data, geoResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal geo response: %w", err)
	}

	if !geoResp.Success {
		return nil, fmt.Errorf("geo radius failed: %s", geoResp.Error)
	}

	// 转换结果
	results := make([]interfaces.GeoResult, len(geoResp.Result))
	for i, r := range geoResp.Result {
		results[i] = interfaces.GeoResult{
			Name:      r.Member,
			Distance:  r.Distance,
			Unit:      unit,
			Longitude: r.Longitude,
			Latitude:  r.Latitude,
			Metadata:  convertMetadataToString(r.Metadata),
			Timestamp: time.Now(),
		}
	}

	return results, nil
}

// GeoHash 获取地理位置的哈希值
func (c *Client) GeoHash(key string, members []string, opts ...interfaces.GeoOption) ([]string, error) {
	options := c.parseGeoOptions(opts...)

	// 构造 GEO 请求
	geoMembers := make([]cProtocol.GeoMember, len(members))
	for i, member := range members {
		geoMembers[i].Member = member
	}

	geoReq := &cProtocol.GeoRequest{
		Operation: "geohash",
		Database:  options.Database,
		Key:       key,
		Members:   geoMembers,
	}

	data, err := sonic.Marshal(geoReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal geo request: %w", err)
	}

	// 创建 GEO 命令头
	head := cProtocol.CreateGeoCommandHead(cProtocol.GeoCommandHash)
	head.SetConfig(cProtocol.REQ, cProtocol.HaveResponse, uint8(cProtocol.CommandTypeGeo))

	protocolReq := &cProtocol.ProtocolRequest{
		Head: head,
		Data: data,
	}

	// 发送请求
	ctx, cancel := context.WithTimeout(context.Background(), options.Timeout)
	defer cancel()

	resp, err := c.protocolClient.SendRequest(ctx, protocolReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send geo hash request: %w", err)
	}

	// 解析响应
	geoResp := &cProtocol.GeoResponse{}
	if err := sonic.Unmarshal(resp.Data, geoResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal geo response: %w", err)
	}

	if !geoResp.Success {
		return nil, fmt.Errorf("geo hash failed: %s", geoResp.Error)
	}

	// 从结果中提取哈希值
	hashes := make([]string, len(geoResp.Result))
	for i, r := range geoResp.Result {
		hashes[i] = fmt.Sprintf("%d", r.Hash)
	}

	return hashes, nil
}

// GeoPos 获取地理位置的经纬度
func (c *Client) GeoPos(key string, members []string, opts ...interfaces.GeoOption) ([]interfaces.GeoPosition, error) {
	options := c.parseGeoOptions(opts...)

	// 构造 GEO 请求
	geoMembers := make([]cProtocol.GeoMember, len(members))
	for i, member := range members {
		geoMembers[i].Member = member
	}

	geoReq := &cProtocol.GeoRequest{
		Operation: "geopos",
		Database:  options.Database,
		Key:       key,
		Members:   geoMembers,
	}

	data, err := sonic.Marshal(geoReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal geo request: %w", err)
	}

	// 创建 GEO 命令头
	head := cProtocol.CreateGeoCommandHead(cProtocol.GeoCommandPos)
	head.SetConfig(cProtocol.REQ, cProtocol.HaveResponse, uint8(cProtocol.CommandTypeGeo))

	protocolReq := &cProtocol.ProtocolRequest{
		Head: head,
		Data: data,
	}

	// 发送请求
	ctx, cancel := context.WithTimeout(context.Background(), options.Timeout)
	defer cancel()

	resp, err := c.protocolClient.SendRequest(ctx, protocolReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send geo pos request: %w", err)
	}

	// 解析响应
	geoResp := &cProtocol.GeoResponse{}
	if err := sonic.Unmarshal(resp.Data, geoResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal geo response: %w", err)
	}

	if !geoResp.Success {
		return nil, fmt.Errorf("geo pos failed: %s", geoResp.Error)
	}

	// 转换结果
	positions := make([]interfaces.GeoPosition, len(geoResp.Result))
	for i, r := range geoResp.Result {
		positions[i] = interfaces.GeoPosition{
			Name:      r.Member,
			Longitude: r.Longitude,
			Latitude:  r.Latitude,
			Timestamp: time.Now(),
		}
	}

	return positions, nil
}

// GeoGet 获取地理位置信息
func (c *Client) GeoGet(key, member string, opts ...interfaces.GeoOption) (*interfaces.GeoData, error) {
	options := c.parseGeoOptions(opts...)

	// 构造 GEO 请求
	geoReq := &cProtocol.GeoRequest{
		Operation: "geoget",
		Database:  options.Database,
		Key:       key,
		Members: []cProtocol.GeoMember{
			{Member: member},
		},
	}

	data, err := sonic.Marshal(geoReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal geo request: %w", err)
	}

	// 创建 GEO 命令头
	head := cProtocol.CreateGeoCommandHead(cProtocol.GeoCommandGet)
	head.SetConfig(cProtocol.REQ, cProtocol.HaveResponse, uint8(cProtocol.CommandTypeGeo))

	protocolReq := &cProtocol.ProtocolRequest{
		Head: head,
		Data: data,
	}

	// 发送请求
	ctx, cancel := context.WithTimeout(context.Background(), options.Timeout)
	defer cancel()

	resp, err := c.protocolClient.SendRequest(ctx, protocolReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send geo get request: %w", err)
	}

	// 解析响应
	geoResp := &cProtocol.GeoResponse{}
	if err := sonic.Unmarshal(resp.Data, geoResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal geo response: %w", err)
	}

	if !geoResp.Success {
		return nil, fmt.Errorf("geo get failed: %s", geoResp.Error)
	}

	if len(geoResp.Result) == 0 {
		return nil, fmt.Errorf("member not found")
	}

	result := geoResp.Result[0]
	return &interfaces.GeoData{
		Name:      result.Member,
		Longitude: result.Longitude,
		Latitude:  result.Latitude,
		Metadata:  convertMetadataToString(result.Metadata),
		Timestamp: time.Now(),
	}, nil
}

// GeoDel 删除地理位置
func (c *Client) GeoDel(key string, members []string, opts ...interfaces.GeoOption) (int64, error) {
	options := c.parseGeoOptions(opts...)

	// 构造 GEO 请求
	geoMembers := make([]cProtocol.GeoMember, len(members))
	for i, member := range members {
		geoMembers[i].Member = member
	}

	geoReq := &cProtocol.GeoRequest{
		Operation: "geodel",
		Database:  options.Database,
		Key:       key,
		Members:   geoMembers,
	}

	data, err := sonic.Marshal(geoReq)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal geo request: %w", err)
	}

	// 创建 GEO 命令头
	head := cProtocol.CreateGeoCommandHead(cProtocol.GeoCommandDelete)
	head.SetConfig(cProtocol.REQ, cProtocol.HaveResponse, uint8(cProtocol.CommandTypeGeo))

	protocolReq := &cProtocol.ProtocolRequest{
		Head: head,
		Data: data,
	}

	// 发送请求
	ctx, cancel := context.WithTimeout(context.Background(), options.Timeout)
	defer cancel()

	resp, err := c.protocolClient.SendRequest(ctx, protocolReq)
	if err != nil {
		return 0, fmt.Errorf("failed to send geo del request: %w", err)
	}

	// 解析响应
	geoResp := &cProtocol.GeoResponse{}
	if err := sonic.Unmarshal(resp.Data, geoResp); err != nil {
		return 0, fmt.Errorf("failed to unmarshal geo response: %w", err)
	}

	if !geoResp.Success {
		return 0, fmt.Errorf("geo del failed: %s", geoResp.Error)
	}

	return int64(geoResp.Count), nil
}

// GeoSearch 高级地理位置搜索
func (c *Client) GeoSearch(key string, searchParams *interfaces.GeoSearchParams, opts ...interfaces.GeoOption) ([]interfaces.GeoResult, error) {
	options := c.parseGeoOptions(opts...)

	// 构造 GEO 请求
	geoReq := &cProtocol.GeoRequest{
		Operation: "geosearch",
		Database:  options.Database,
		Key:       key,
		Longitude: searchParams.Longitude,
		Latitude:  searchParams.Latitude,
		Radius:    searchParams.Radius,
		Unit:      searchParams.Unit,
		Count:     int(searchParams.Count),
	}

	data, err := sonic.Marshal(geoReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal geo request: %w", err)
	}

	// 创建 GEO 命令头
	head := cProtocol.CreateGeoCommandHead(cProtocol.GeoCommandSearch)
	head.SetConfig(cProtocol.REQ, cProtocol.HaveResponse, uint8(cProtocol.CommandTypeGeo))

	protocolReq := &cProtocol.ProtocolRequest{
		Head: head,
		Data: data,
	}

	// 发送请求
	ctx, cancel := context.WithTimeout(context.Background(), options.Timeout)
	defer cancel()

	resp, err := c.protocolClient.SendRequest(ctx, protocolReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send geo search request: %w", err)
	}

	// 解析响应
	geoResp := &cProtocol.GeoResponse{}
	if err := sonic.Unmarshal(resp.Data, geoResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal geo response: %w", err)
	}

	if !geoResp.Success {
		return nil, fmt.Errorf("geo search failed: %s", geoResp.Error)
	}

	// 转换结果
	results := make([]interfaces.GeoResult, len(geoResp.Result))
	for i, r := range geoResp.Result {
		results[i] = interfaces.GeoResult{
			Name:      r.Member,
			Distance:  r.Distance,
			Unit:      searchParams.Unit,
			Longitude: r.Longitude,
			Latitude:  r.Latitude,
			Metadata:  convertMetadataToString(r.Metadata),
			Timestamp: time.Now(),
		}
	}

	return results, nil
}

// 辅助函数: 转换元数据类型
func convertMetadata(m map[string]string) map[string]interface{} {
	if m == nil {
		return make(map[string]interface{})
	}
	result := make(map[string]interface{})
	for k, v := range m {
		result[k] = v
	}
	return result
}

// 辅助函数: 转换元数据类型
func convertMetadataToString(m map[string]interface{}) map[string]string {
	if m == nil {
		return make(map[string]string)
	}
	result := make(map[string]string)
	for k, v := range m {
		if str, ok := v.(string); ok {
			result[k] = str
		}
	}
	return result
}
