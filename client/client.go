package client

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"burin/cProtocol"
	"burin/client/interfaces"
	"burin/client/internal/geo"
	"burin/client/internal/store"
	"burin/client/internal/transaction"

	"github.com/bytedance/sonic"
	"github.com/sirupsen/logrus"
)

// BurinClient 统一的 Burin 客户端
type BurinClient struct {
	config         *ClientConfig
	protocolClient *cProtocol.Client
	logger         *logrus.Logger
	ctx            context.Context // 内部上下文

	// 子客户端
	cacheClient       interfaces.CacheInterface
	transactionClient interfaces.TransactionInterface
	geoClient         interfaces.GeoInterface
}

// NewClient 创建新的 Burin 客户端
func NewClient(config *ClientConfig) (*BurinClient, error) {
	if config == nil {
		config = NewDefaultConfig()
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// 禁止使用系统保留数据库作为默认数据库
	if strings.HasPrefix(config.Cache.DefaultDatabase, "__burin_") {
		return nil, fmt.Errorf("database '__burin_*' is reserved for system use and cannot be used as default database")
	}

	// 创建日志器
	logger := logrus.New()
	level, err := logrus.ParseLevel(config.Logging.Level)
	if err != nil {
		level = logrus.InfoLevel
	}
	logger.SetLevel(level)

	// 创建协议客户端
	protocolConfig := &cProtocol.ClientConfig{
		Endpoint:            config.Connection.Endpoint,
		MaxConnsPerEndpoint: config.Connection.MaxConnsPerEndpoint,
		DialTimeout:         config.Connection.DialTimeout,
		ReadTimeout:         config.Connection.ReadTimeout,
		WriteTimeout:        config.Connection.WriteTimeout,
		HealthCheckInterval: config.Cluster.HealthCheckInterval,
	}

	protocolClient := cProtocol.NewClient(protocolConfig, logger)

	// 创建缓存客户端
	storeConfig := &store.Config{
		DefaultDatabase: config.Cache.DefaultDatabase,
		DefaultTTL:      config.Cache.DefaultTTL,
		MaxKeySize:      config.Cache.MaxKeySize,
		MaxValueSize:    config.Cache.MaxValueSize,
		EnableMetrics:   config.Metrics.Enabled,
		RetryCount:      config.Connection.RetryCount,
		RetryDelay:      config.Connection.RetryDelay,
	}
	cacheClient := store.NewClient(protocolClient, storeConfig, logger)

	// 创建事务客户端
	txConfig := &transaction.Config{
		DefaultTimeout:        30 * time.Second,
		DefaultIsolationLevel: interfaces.RepeatableRead,
		MaxConcurrentTxs:      100,
	}
	transactionClient := transaction.NewClient(protocolClient, txConfig)

	// 创建 GEO 客户端
	geoConfig := &geo.Config{
		DefaultDatabase: config.Cache.DefaultDatabase,
		DefaultTimeout:  10 * time.Second,
		RetryCount:      config.Connection.RetryCount,
		RetryDelay:      config.Connection.RetryDelay,
	}
	geoClient := geo.NewClient(protocolClient, geoConfig, logger)

	client := &BurinClient{
		config:            config,
		protocolClient:    protocolClient,
		logger:            logger,
		ctx:               context.Background(), // 初始化默认上下文
		cacheClient:       cacheClient,
		transactionClient: transactionClient,
		geoClient:         geoClient,
	}

	return client, nil
}

// WithContext 设置或更新客户端上下文
func (c *BurinClient) WithContext(ctx context.Context) *BurinClient {
	c.ctx = ctx
	return c
}

// Context 获取当前上下文
func (c *BurinClient) Context() context.Context {
	return c.ctx
}

// Connect 连接到 Burin 服务器（不再需要ctx参数）
func (c *BurinClient) Connect() error {
	// 如果提供了用户名密码，设置新连接时自动登录的回调
	if c.config.Auth.Username != "" && c.config.Auth.Password != "" {
		c.protocolClient.SetOnNewConnection(func(conn net.Conn) error {
			return c.loginOnConnection(conn, c.config.Auth.Username, c.config.Auth.Password)
		})
	}

	if err := c.protocolClient.Start(); err != nil {
		return fmt.Errorf("failed to start protocol client: %w", err)
	}

	c.logger.Info("Burin client connected successfully")
	return nil
}

// loginOnConnection 在指定连接上执行登录
func (c *BurinClient) loginOnConnection(conn net.Conn, username, password string) error {
	c.logger.Debugf("Attempting to login on connection: %s", conn.RemoteAddr().String())

	// 构造登录请求
	head := cProtocol.CreateAuthCommandHead(cProtocol.AuthCommandLogin)

	loginReq := map[string]string{
		"username": username,
		"password": password,
	}

	reqData, err := sonic.Marshal(loginReq)
	if err != nil {
		return fmt.Errorf("failed to marshal login data: %w", err)
	}

	c.logger.Debugf("Login request data: %s", string(reqData))

	// 确保设置正确的数据长度
	head.SetContentLength(uint32(len(reqData)))

	// 发送头部
	if _, err := conn.Write(head.GetBytes()); err != nil {
		return fmt.Errorf("failed to send login head: %w", err)
	}

	// 发送数据
	if len(reqData) > 0 {
		if _, err := conn.Write(reqData); err != nil {
			return fmt.Errorf("failed to send login data: %w", err)
		}
	}

	c.logger.Debugf("Login request sent, waiting for response...")

	// 设置读取超时
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	// 读取响应头
	buffer := make([]byte, 6)
	if _, err := conn.Read(buffer); err != nil {
		return fmt.Errorf("failed to read login response head: %w", err)
	}

	respHead, err := cProtocol.HeadFromBytes(buffer)
	if err != nil {
		return fmt.Errorf("failed to parse login response head: %w", err)
	}

	// 读取响应数据
	respLen := respHead.GetContentLength()
	c.logger.Debugf("Login response length: %d", respLen)

	respData := make([]byte, respLen)
	if respLen > 0 {
		if _, err := conn.Read(respData); err != nil {
			return fmt.Errorf("failed to read login response data: %w", err)
		}
	}

	c.logger.Debugf("Login response data: %s", string(respData))

	// 优化协议：所有响应前2字节为状态码
	if len(respData) < 2 {
		return fmt.Errorf("invalid login response: data too short")
	}

	// 解析状态码（Big Endian）
	status := int(respData[0])<<8 | int(respData[1])

	if status != 200 {
		// 错误响应
		errorMsg := ""
		if len(respData) > 2 {
			errorMsg = string(respData[2:])
		}
		return fmt.Errorf("login failed with status %d: %s", status, errorMsg)
	}

	// 正常响应：解析业务数据（跳过前2字节状态码）
	var loginResp map[string]interface{}
	businessData := respData[2:]
	if err := sonic.Unmarshal(businessData, &loginResp); err != nil {
		return fmt.Errorf("failed to parse login response (data: %s): %w", string(businessData), err)
	}

	// 检查登录是否成功
	if success, ok := loginResp["success"].(bool); !ok || !success {
		if errMsg, ok := loginResp["error"].(string); ok && errMsg != "" {
			return fmt.Errorf("login failed: %s", errMsg)
		}
		return fmt.Errorf("login failed: response=%v", loginResp)
	}

	c.logger.Infof("Connection authenticated as user: %s", username)
	return nil
}

// Login 执行用户登录
func (c *BurinClient) Login(ctx context.Context, username, password string) error {
	// 构造登录请求
	head := cProtocol.CreateAuthCommandHead(cProtocol.AuthCommandLogin)

	loginData := map[string]string{
		"username": username,
		"password": password,
	}

	data, err := sonic.Marshal(loginData)
	if err != nil {
		return fmt.Errorf("failed to marshal login data: %w", err)
	}

	req := &cProtocol.ProtocolRequest{
		Head: head,
		Data: data,
	}

	resp, err := c.protocolClient.SendRequest(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to send login request: %w", err)
	}

	if resp.Status != 200 {
		return fmt.Errorf("login failed: %s", resp.Error)
	}

	// 解析登录响应
	var loginResp map[string]interface{}
	if err := sonic.Unmarshal(resp.Data, &loginResp); err != nil {
		return fmt.Errorf("failed to parse login response: %w", err)
	}

	if success, ok := loginResp["success"].(bool); !ok || !success {
		if msg, ok := loginResp["message"].(string); ok {
			return fmt.Errorf("login failed: %s", msg)
		}
		return fmt.Errorf("login failed")
	}

	c.logger.Info("Login successful")
	return nil
}

// GetLeader 获取当前集群的 Leader 节点信息
func (c *BurinClient) GetLeader(ctx context.Context) (string, error) {
	// 向端点查询集群状态
	leader, err := c.queryLeaderFromEndpoint(ctx, c.config.Connection.Endpoint)
	if err == nil && leader != "" {
		return leader, nil
	}
	return "", fmt.Errorf("failed to discover leader from endpoint: %w", err)
}

// queryLeaderFromEndpoint 从指定端点查询 Leader 信息
func (c *BurinClient) queryLeaderFromEndpoint(ctx context.Context, endpoint string) (string, error) {
	// 构造健康检查请求
	head := cProtocol.CreateClientCommandHead(cProtocol.CmdHealth)

	req := &cProtocol.ProtocolRequest{
		Head: head,
		Data: nil,
	}

	resp, err := c.protocolClient.SendRequest(ctx, req)
	if err != nil {
		return "", err
	}

	// 解析响应获取leader信息
	var healthData map[string]interface{}
	if err := sonic.Unmarshal(resp.Data, &healthData); err != nil {
		return "", err
	}

	// 检查是否包含leader信息
	if leaderInfo, ok := healthData["leader"]; ok {
		if leaderAddr, ok := leaderInfo.(string); ok && leaderAddr != "" {
			return leaderAddr, nil
		}
	}

	// 检查当前节点是否是leader
	if isLeader, ok := healthData["is_leader"]; ok {
		if isLeaderBool, ok := isLeader.(bool); ok && isLeaderBool {
			return endpoint, nil
		}
	}

	return "", fmt.Errorf("no leader information found")
}

// GetClusterInfo 获取集群信息
func (c *BurinClient) GetClusterInfo(ctx context.Context) (map[string]interface{}, error) {
	// 构造集群状态查询请求
	head := cProtocol.CreateClusterCommandHead(cProtocol.ClusterCmdClusterStatus)

	req := &cProtocol.ProtocolRequest{
		Head: head,
		Data: nil,
	}

	resp, err := c.protocolClient.SendRequest(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster info: %w", err)
	}

	var clusterInfo map[string]interface{}
	if err := sonic.Unmarshal(resp.Data, &clusterInfo); err != nil {
		return nil, fmt.Errorf("failed to parse cluster info: %w", err)
	}

	return clusterInfo, nil
}

// GetHealth 获取节点健康状态
func (c *BurinClient) GetHealth(ctx context.Context) (map[string]interface{}, error) {
	// 构造健康检查请求
	head := cProtocol.CreateClientCommandHead(cProtocol.CmdHealth)

	req := &cProtocol.ProtocolRequest{
		Head: head,
		Data: nil,
	}

	resp, err := c.protocolClient.SendRequest(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get health status: %w", err)
	}

	var healthInfo map[string]interface{}
	if err := sonic.Unmarshal(resp.Data, &healthInfo); err != nil {
		return nil, fmt.Errorf("failed to parse health info: %w", err)
	}

	return healthInfo, nil
}

// Disconnect 断开连接
func (c *BurinClient) Disconnect() error {
	if err := c.protocolClient.Stop(); err != nil {
		return fmt.Errorf("failed to stop protocol client: %w", err)
	}

	c.logger.Info("Burin client disconnected")
	return nil
}

// Cache 返回缓存客户端
func (c *BurinClient) Cache() interfaces.CacheInterface {
	return c.cacheClient
}

// Geo 返回地理位置客户端
func (c *BurinClient) Geo() interfaces.GeoInterface {
	return c.geoClient
}

// ====== 便捷的缓存操作方法 ======

// Get 获取缓存值
func (c *BurinClient) Get(key string, opts ...interfaces.CacheOption) (*interfaces.CacheResponse, error) {
	return c.cacheClient.Get(key, opts...)
}

// Set 设置缓存值
func (c *BurinClient) Set(key string, value []byte, opts ...interfaces.CacheOption) error {
	return c.cacheClient.Set(key, value, opts...)
}

// Delete 删除缓存值
func (c *BurinClient) Delete(key string, opts ...interfaces.CacheOption) error {
	return c.cacheClient.Delete(key, opts...)
}

// Exists 检查缓存是否存在
func (c *BurinClient) Exists(key string, opts ...interfaces.CacheOption) (bool, error) {
	return c.cacheClient.Exists(key, opts...)
}

// MGet 批量获取缓存值
func (c *BurinClient) MGet(keys []string, opts ...interfaces.CacheOption) (map[string]*interfaces.CacheResponse, error) {
	return c.cacheClient.MGet(keys, opts...)
}

// MSet 批量设置缓存值
func (c *BurinClient) MSet(keyValues map[string][]byte, opts ...interfaces.CacheOption) error {
	return c.cacheClient.MSet(keyValues, opts...)
}

// ListKeys 列出默认数据库中匹配前缀的键
func (c *BurinClient) ListKeys(opts ...interfaces.CacheOption) ([]string, int64, error) {
	return c.cacheClient.ListKeys(opts...)
}

// CountKeys 统计默认数据库中匹配前缀的键数量
func (c *BurinClient) CountKeys(opts ...interfaces.CacheOption) (int64, error) {
	return c.cacheClient.CountKeys(opts...)
}

// ====== 缓存选项函数 ======

// WithTTL 设置TTL
func WithTTL(ttl time.Duration) interfaces.CacheOption {
	return func(opts *interfaces.CacheOptions) {
		opts.TTL = &ttl
	}
}

// WithMetadata 设置元数据
func WithMetadata(metadata map[string]string) interfaces.CacheOption {
	return func(opts *interfaces.CacheOptions) {
		opts.Metadata = metadata
	}
}

// WithDatabase 设置数据库
func WithDatabase(database string) interfaces.CacheOption {
	return func(opts *interfaces.CacheOptions) {
		opts.Database = database
	}
}

// ====== 列表操作选项函数 ======

// WithPrefix 设置键前缀
func WithPrefix(prefix string) interfaces.CacheOption {
	return func(opts *interfaces.CacheOptions) {
		opts.Prefix = &prefix
	}
}

// WithOffset 设置分页偏移量
func WithOffset(offset int) interfaces.CacheOption {
	return func(opts *interfaces.CacheOptions) {
		opts.Offset = &offset
	}
}

// WithLimit 设置分页限制
func WithLimit(limit int) interfaces.CacheOption {
	return func(opts *interfaces.CacheOptions) {
		opts.Limit = &limit
	}
}

// ====== 事务方法 ======

// ====== GEO 地理位置操作方法 ======

// GeoAdd 添加地理位置信息
func (c *BurinClient) GeoAdd(key string, members []interfaces.GeoMember, opts ...interfaces.GeoOption) error {
	return c.geoClient.GeoAdd(key, members, opts...)
}

// GeoDist 计算两个地理位置之间的距离
func (c *BurinClient) GeoDist(key, member1, member2, unit string, opts ...interfaces.GeoOption) (float64, error) {
	return c.geoClient.GeoDist(key, member1, member2, unit, opts...)
}

// GeoRadius 查询指定范围内的地理位置
func (c *BurinClient) GeoRadius(key string, lon, lat, radius float64, unit string, opts ...interfaces.GeoOption) ([]interfaces.GeoResult, error) {
	return c.geoClient.GeoRadius(key, lon, lat, radius, unit, opts...)
}

// GeoHash 获取地理位置的哈希值
func (c *BurinClient) GeoHash(key string, members []string, opts ...interfaces.GeoOption) ([]string, error) {
	return c.geoClient.GeoHash(key, members, opts...)
}

// GeoPos 获取地理位置的经纬度
func (c *BurinClient) GeoPos(key string, members []string, opts ...interfaces.GeoOption) ([]interfaces.GeoPosition, error) {
	return c.geoClient.GeoPos(key, members, opts...)
}

// GeoGet 获取地理位置信息
func (c *BurinClient) GeoGet(key, member string, opts ...interfaces.GeoOption) (*interfaces.GeoData, error) {
	return c.geoClient.GeoGet(key, member, opts...)
}

// GeoDel 删除地理位置
func (c *BurinClient) GeoDel(key string, members []string, opts ...interfaces.GeoOption) (int64, error) {
	return c.geoClient.GeoDel(key, members, opts...)
}

// GeoSearch 高级地理位置搜索
func (c *BurinClient) GeoSearch(key string, searchParams *interfaces.GeoSearchParams, opts ...interfaces.GeoOption) ([]interfaces.GeoResult, error) {
	return c.geoClient.GeoSearch(key, searchParams, opts...)
}

// BeginTransaction 开始一个新的ACID事务
func (c *BurinClient) BeginTransaction(opts ...interfaces.TransactionOption) (interfaces.Transaction, error) {
	return c.transactionClient.BeginTransaction(opts...)
}

// ====== 认证和用户管理操作方法 ======

// SendAuthRequest 发送认证相关的请求
func (c *BurinClient) SendAuthRequest(ctx context.Context, head *cProtocol.HEAD, data []byte) (*cProtocol.ProtocolResponse, error) {
	req := &cProtocol.ProtocolRequest{
		Head: head,
		Data: data,
	}

	resp, err := c.protocolClient.SendRequest(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to send auth request: %w", err)
	}

	return resp, nil
}
