package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/qorm/burin/cProtocol"
	"github.com/qorm/burin/client/interfaces"

	"github.com/bytedance/sonic"
	"github.com/sirupsen/logrus"
)

// Client 缓存客户端实现
type Client struct {
	protocolClient *cProtocol.Client
	config         *Config
	logger         *logrus.Logger
	metrics        *MetricsCollector
}

// Config 缓存配置
type Config struct {
	DefaultDatabase string        `json:"default_database" yaml:"default_database"`
	DefaultTTL      time.Duration `json:"default_ttl" yaml:"default_ttl"`
	MaxKeySize      int           `json:"max_key_size" yaml:"max_key_size"`
	MaxValueSize    int           `json:"max_value_size" yaml:"max_value_size"`
	EnableMetrics   bool          `json:"enable_metrics" yaml:"enable_metrics"`
	RetryCount      int           `json:"retry_count" yaml:"retry_count"`
	RetryDelay      time.Duration `json:"retry_delay" yaml:"retry_delay"`
}

// DefaultConfig 返回默认缓存配置
func DefaultConfig() *Config {
	return &Config{
		DefaultDatabase: "default",
		DefaultTTL:      0,
		MaxKeySize:      1024,
		MaxValueSize:    1024 * 1024, // 1MB
		EnableMetrics:   true,
		RetryCount:      3,
		RetryDelay:      100 * time.Millisecond,
	}
}

// MetricsCollector 监控指标收集器
type MetricsCollector struct {
	enabled     bool
	operations  map[string]int64
	errors      map[string]int64
	latencies   map[string]time.Duration
	lastUpdated time.Time
}

// newMetricsCollector 创建监控收集器
func newMetricsCollector(enabled bool) *MetricsCollector {
	return &MetricsCollector{
		enabled:     enabled,
		operations:  make(map[string]int64),
		errors:      make(map[string]int64),
		latencies:   make(map[string]time.Duration),
		lastUpdated: time.Now(),
	}
}

// NewClient 创建缓存客户端
func NewClient(protocolClient *cProtocol.Client, config *Config, logger *logrus.Logger) interfaces.CacheInterface {
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
		metrics:        newMetricsCollector(config.EnableMetrics),
	}
}

// 缓存请求结构体（兼容现有协议）
type cacheRequest struct {
	Key      string            `json:"key"`
	Value    string            `json:"value,omitempty"`
	Database string            `json:"database,omitempty"`
	TTL      map[string]int64  `json:"ttl,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// 缓存响应结构体（兼容现有协议）
type cacheResponse struct {
	Key      string                 `json:"key"`
	Value    string                 `json:"value"`
	Found    bool                   `json:"found"`
	Database string                 `json:"database,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// Get 获取缓存值
func (c *Client) Get(key string, opts ...interfaces.CacheOption) (*interfaces.CacheResponse, error) {
	start := time.Now()
	defer c.recordMetrics("get", start)

	if err := c.validateKey(key); err != nil {
		return nil, fmt.Errorf("invalid key: %w", err)
	}

	options := c.buildOptions(opts...)
	database := c.getDatabase(options.Database)

	// 构建请求
	req := &cacheRequest{
		Key:      key,
		Database: database,
		Metadata: options.Metadata,
	}

	// 序列化请求
	reqData, err := sonic.Marshal(req)
	if err != nil {
		c.recordError("get", err)
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	c.logger.Debugf("Get cache: key=%s, database=%s", key, database)

	// 创建协议请求
	protocolReq, err := c.createProtocolRequest(cProtocol.CmdGet, reqData, "get", database)
	if err != nil {
		return nil, fmt.Errorf("failed to create protocol request: %w", err)
	}

	// 执行请求
	ctx := context.Background()
	resp, err := c.executeWithRetry(ctx, protocolReq)
	if err != nil {
		c.recordError("get", err)
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}

	// 解析响应
	var cacheResp cacheResponse
	if err := sonic.Unmarshal(resp.Data, &cacheResp); err != nil {
		c.recordError("get", err)
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	// 构建结果
	result := &interfaces.CacheResponse{
		Key:       key,
		Value:     []byte(cacheResp.Value),
		Found:     cacheResp.Found,
		Database:  database,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// 处理元数据
	if cacheResp.Metadata != nil {
		result.Metadata = make(map[string]string)
		for k, v := range cacheResp.Metadata {
			if str, ok := v.(string); ok {
				result.Metadata[k] = str
			}
		}
	}

	c.logger.Debugf("Get cache completed: key=%s, found=%t", key, result.Found)
	return result, nil
}

// Set 设置缓存值
func (c *Client) Set(key string, value []byte, opts ...interfaces.CacheOption) error {
	start := time.Now()
	defer c.recordMetrics("set", start)

	if err := c.validateKey(key); err != nil {
		return fmt.Errorf("invalid key: %w", err)
	}

	if err := c.validateValue(value); err != nil {
		return fmt.Errorf("invalid value: %w", err)
	}

	options := c.buildOptions(opts...)
	database := c.getDatabase(options.Database)

	// 构建请求
	req := &cacheRequest{
		Key:      key,
		Value:    string(value),
		Database: database,
		Metadata: options.Metadata,
	}

	// 设置TTL
	if options.TTL != nil && *options.TTL > 0 {
		req.TTL = map[string]int64{
			"seconds": int64(options.TTL.Seconds()),
		}
	}

	// 序列化请求
	reqData, err := sonic.Marshal(req)
	if err != nil {
		c.recordError("set", err)
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	c.logger.Debugf("Set cache: key=%s, database=%s, size=%d", key, database, len(value))

	// 创建协议请求
	protocolReq, err := c.createProtocolRequest(cProtocol.CmdSet, reqData, "set", database)
	if err != nil {
		return fmt.Errorf("failed to create protocol request: %w", err)
	}

	// 执行请求
	ctx := context.Background()
	_, err = c.executeWithRetry(ctx, protocolReq)
	if err != nil {
		c.recordError("set", err)
		return fmt.Errorf("failed to execute request: %w", err)
	}

	c.logger.Debugf("Set cache completed: key=%s", key)
	return nil
}

// Delete 删除缓存值
func (c *Client) Delete(key string, opts ...interfaces.CacheOption) error {
	start := time.Now()
	defer c.recordMetrics("delete", start)

	if err := c.validateKey(key); err != nil {
		return fmt.Errorf("invalid key: %w", err)
	}

	options := c.buildOptions(opts...)
	database := c.getDatabase(options.Database)

	// 构建请求
	req := &cacheRequest{
		Key:      key,
		Database: database,
	}

	// 序列化请求
	reqData, err := sonic.Marshal(req)
	if err != nil {
		c.recordError("delete", err)
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	c.logger.Debugf("Delete cache: key=%s, database=%s", key, database)

	// 创建协议请求
	protocolReq, err := c.createProtocolRequest(cProtocol.CmdDelete, reqData, "delete", database)
	if err != nil {
		return fmt.Errorf("failed to create protocol request: %w", err)
	}

	// 执行请求
	ctx := context.Background()
	_, err = c.executeWithRetry(ctx, protocolReq)
	if err != nil {
		c.recordError("delete", err)
		return fmt.Errorf("failed to execute request: %w", err)
	}

	c.logger.Debugf("Delete cache completed: key=%s", key)
	return nil
}

// Exists 检查缓存是否存在
func (c *Client) Exists(key string, opts ...interfaces.CacheOption) (bool, error) {
	start := time.Now()
	defer c.recordMetrics("exists", start)

	if err := c.validateKey(key); err != nil {
		return false, fmt.Errorf("invalid key: %w", err)
	}

	options := c.buildOptions(opts...)
	database := c.getDatabase(options.Database)

	// 构建请求
	req := &cacheRequest{
		Key:      key,
		Database: database,
	}

	// 序列化请求
	reqData, err := sonic.Marshal(req)
	if err != nil {
		c.recordError("exists", err)
		return false, fmt.Errorf("failed to marshal request: %w", err)
	}

	c.logger.Debugf("Check cache exists: key=%s, database=%s", key, database)

	// 创建协议请求
	protocolReq, err := c.createProtocolRequest(cProtocol.CmdExists, reqData, "exists", database)
	if err != nil {
		return false, fmt.Errorf("failed to create protocol request: %w", err)
	}

	// 执行请求 - EXISTS特殊处理
	// 协议层会把404当作错误返回，我们需要特别处理
	ctx := context.Background()
	_, err = c.protocolClient.SendRequest(ctx, protocolReq)

	if err != nil {
		// 检查是否是404错误（键不存在）
		if strings.Contains(err.Error(), "status 404") {
			c.logger.Debugf("Check cache exists completed: key=%s, exists=false", key)
			return false, nil
		}
		// 其他错误
		c.recordError("exists", err)
		return false, fmt.Errorf("failed to check exists: %w", err)
	}

	// 成功响应（状态码200）表示键存在
	c.logger.Debugf("Check cache exists completed: key=%s, exists=true", key)
	return true, nil
}

// MGet 批量获取缓存
func (c *Client) MGet(keys []string, opts ...interfaces.CacheOption) (map[string]*interfaces.CacheResponse, error) {
	results := make(map[string]*interfaces.CacheResponse)

	for _, key := range keys {
		resp, err := c.Get(key, opts...)
		if err != nil {
			c.logger.Warnf("MGet failed for key %s: %v", key, err)
			results[key] = &interfaces.CacheResponse{
				Key:   key,
				Found: false,
			}
		} else {
			results[key] = resp
		}
	}

	return results, nil
}

// MSet 批量设置缓存
func (c *Client) MSet(keyValues map[string][]byte, opts ...interfaces.CacheOption) error {
	for key, value := range keyValues {
		if err := c.Set(key, value, opts...); err != nil {
			return fmt.Errorf("MSet failed for key %s: %w", key, err)
		}
	}
	return nil
}

// MDelete 批量删除缓存
func (c *Client) MDelete(keys []string, opts ...interfaces.CacheOption) error {
	for _, key := range keys {
		if err := c.Delete(key, opts...); err != nil {
			return fmt.Errorf("MDelete failed for key %s: %w", key, err)
		}
	}
	return nil
}

// ListKeysWithDatabase 列出指定数据库中匹配前缀的键
func (c *Client) ListKeys(opts ...interfaces.CacheOption) ([]string, int64, error) {
	start := time.Now()
	defer c.recordMetrics("list_keys", start)

	// 构建选项
	options := c.buildOptions(opts...)

	// 获取数据库
	database := c.getDatabase(options.Database)

	// 获取列表参数（从 options 中获取）
	prefix := ""
	if options.Prefix != nil {
		prefix = *options.Prefix
	}

	offset := 0
	if options.Offset != nil {
		offset = *options.Offset
	}

	limit := 10 // 默认每页 10 条
	if options.Limit != nil {
		limit = *options.Limit
	}

	// 构建请求
	type listRequest struct {
		Database string `json:"database"`
		Prefix   string `json:"prefix"`
		Offset   int    `json:"offset"`
		Limit    int    `json:"limit"`
	}

	req := listRequest{
		Database: database,
		Prefix:   prefix,
		Offset:   offset,
		Limit:    limit,
	}

	// 序列化请求
	reqData, err := sonic.Marshal(req)
	if err != nil {
		c.recordError("list_keys", err)
		return nil, 0, fmt.Errorf("failed to marshal request: %w", err)
	}

	c.logger.Debugf("List keys: database=%s, prefix=%s, offset=%d, limit=%d", database, prefix, offset, limit)

	// 创建协议请求
	protocolReq, err := c.createProtocolRequest(cProtocol.CmdList, reqData, "list_keys", database)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create protocol request: %w", err)
	}

	// 执行请求
	ctx := context.Background()
	resp, err := c.executeWithRetry(ctx, protocolReq)
	if err != nil {
		c.recordError("list_keys", err)
		return nil, 0, fmt.Errorf("failed to execute request: %w", err)
	}

	// 解析响应
	type listResponse struct {
		Keys     []string `json:"keys"`
		Total    int64    `json:"total"`
		Offset   int      `json:"offset"`
		Limit    int      `json:"limit"`
		HasMore  bool     `json:"has_more"`
		Database string   `json:"database"`
		Prefix   string   `json:"prefix"`
		Error    string   `json:"error,omitempty"`
		Status   string   `json:"status"`
	}

	var listResp listResponse
	if err := sonic.Unmarshal(resp.Data, &listResp); err != nil {
		c.recordError("list_keys", err)
		return nil, 0, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if listResp.Status != "success" && listResp.Error != "" {
		return nil, 0, fmt.Errorf("list keys failed: %s", listResp.Error)
	}

	c.logger.Debugf("List keys completed: found=%d, total=%d", len(listResp.Keys), listResp.Total)
	return listResp.Keys, listResp.Total, nil
}

func (c *Client) CountKeys(opts ...interfaces.CacheOption) (int64, error) {
	start := time.Now()
	defer c.recordMetrics("count_keys", start)

	options := c.buildOptions(opts...)
	database := c.getDatabase(options.Database)
	// 获取前缀参数（从 options 中获取）
	prefix := ""
	if options.Prefix != nil {
		prefix = *options.Prefix
	}

	// 构建请求
	type countRequest struct {
		Database string `json:"database"`
		Prefix   string `json:"prefix"`
	}

	req := countRequest{
		Database: database,
		Prefix:   prefix,
	}

	// 序列化请求
	reqData, err := sonic.Marshal(req)
	if err != nil {
		c.recordError("count_keys", err)
		return 0, fmt.Errorf("failed to marshal request: %w", err)
	}

	c.logger.Debugf("Count keys: database=%s, prefix=%s", database, prefix)

	// 创建协议请求
	protocolReq, err := c.createProtocolRequest(cProtocol.CmdCount, reqData, "count_keys", database)
	if err != nil {
		return 0, fmt.Errorf("failed to create protocol request: %w", err)
	}

	// 执行请求
	ctx := context.Background()
	resp, err := c.executeWithRetry(ctx, protocolReq)
	if err != nil {
		c.recordError("count_keys", err)
		return 0, fmt.Errorf("failed to execute request: %w", err)
	}

	// 解析响应
	type countResponse struct {
		Count    int64  `json:"count"`
		Database string `json:"database"`
		Prefix   string `json:"prefix"`
		Error    string `json:"error,omitempty"`
		Status   string `json:"status"`
	}

	var countResp countResponse
	if err := sonic.Unmarshal(resp.Data, &countResp); err != nil {
		c.recordError("count_keys", err)
		return 0, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if countResp.Status != "success" && countResp.Error != "" {
		return 0, fmt.Errorf("count keys failed: %s", countResp.Error)
	}

	c.logger.Debugf("Count keys completed: count=%d", countResp.Count)
	return countResp.Count, nil
}

// 选项函数

// WithTTL 设置TTL
func WithTTL(ttl time.Duration) interfaces.CacheOption {
	return func(o *interfaces.CacheOptions) {
		o.TTL = &ttl
	}
}

// WithMetadata 设置元数据
func WithMetadata(metadata map[string]string) interfaces.CacheOption {
	return func(o *interfaces.CacheOptions) {
		o.Metadata = metadata
	}
}

// WithConsistentRead 设置强一致性读
func WithConsistentRead() interfaces.CacheOption {
	return func(o *interfaces.CacheOptions) {
		o.Consistent = true
	}
}

// 辅助方法

// buildOptions 构建操作选项
func (c *Client) buildOptions(opts ...interfaces.CacheOption) *interfaces.CacheOptions {
	options := &interfaces.CacheOptions{
		Database: c.config.DefaultDatabase,
	}

	for _, opt := range opts {
		opt(options)
	}

	// 设置默认TTL
	if options.TTL == nil && c.config.DefaultTTL > 0 {
		options.TTL = &c.config.DefaultTTL
	}

	return options
}

// getDatabase 获取数据库名称
func (c *Client) getDatabase(database string) string {
	if database == "" {
		return c.config.DefaultDatabase
	}
	return database
}

// validateKey 验证key
func (c *Client) validateKey(key string) error {
	if key == "" {
		return fmt.Errorf("key cannot be empty")
	}
	if len(key) > c.config.MaxKeySize {
		return fmt.Errorf("key length %d exceeds limit %d", len(key), c.config.MaxKeySize)
	}
	return nil
}

// validateValue 验证value
func (c *Client) validateValue(value []byte) error {
	if len(value) > c.config.MaxValueSize {
		return fmt.Errorf("value length %d exceeds limit %d", len(value), c.config.MaxValueSize)
	}
	return nil
}

// createProtocolRequest 创建协议请求（统一的请求创建逻辑）
func (c *Client) createProtocolRequest(cmd cProtocol.Command, reqData []byte, operation, database string) (*cProtocol.ProtocolRequest, error) {
	// 使用 CreateClientCommandHead 创建正确的协议头
	head := cProtocol.CreateClientCommandHead(cmd)

	protocolReq := &cProtocol.ProtocolRequest{
		Head: head,
		Data: reqData,
		Meta: map[string]string{
			"operation": operation,
			"database":  database,
		},
	}

	return protocolReq, nil
}

// executeWithRetry 执行请求并重试
func (c *Client) executeWithRetry(ctx context.Context, request *cProtocol.ProtocolRequest) (*cProtocol.ProtocolResponse, error) {
	var lastErr error

	for attempt := 0; attempt <= c.config.RetryCount; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(c.config.RetryDelay):
				c.logger.Debugf("Retry attempt %d", attempt)
			}
		}

		response, err := c.protocolClient.SendRequest(ctx, request)
		if err == nil {
			// 协议层已经处理了状态码，直接返回成功响应
			return response, nil
		}

		lastErr = err

		// 某些错误不需要重试
		if !c.shouldRetry(err) {
			break
		}
	}

	return nil, lastErr
}

// shouldRetry 判断是否应该重试
func (c *Client) shouldRetry(err error) bool {
	if err == nil {
		return false
	}

	errMsg := strings.ToLower(err.Error())

	// 认证错误不重试
	if strings.Contains(errMsg, "authentication") ||
		strings.Contains(errMsg, "unauthorized") ||
		strings.Contains(errMsg, "login") {
		return false
	}

	// 网络错误重试
	return true
}

// recordMetrics 记录监控指标
func (c *Client) recordMetrics(operation string, startTime time.Time) {
	if !c.metrics.enabled {
		return
	}

	latency := time.Since(startTime)
	c.metrics.operations[operation]++
	c.metrics.latencies[operation] = latency
	c.metrics.lastUpdated = time.Now()
}

// recordError 记录错误
func (c *Client) recordError(operation string, err error) {
	if !c.metrics.enabled {
		return
	}

	c.metrics.errors[operation]++
	c.logger.Warnf("Cache operation failed: %s, error: %v", operation, err)
}

// GetMetrics 获取监控指标
func (c *Client) GetMetrics() map[string]interface{} {
	if !c.metrics.enabled {
		return nil
	}

	return map[string]interface{}{
		"operations":   c.metrics.operations,
		"latencies":    c.metrics.latencies,
		"errors":       c.metrics.errors,
		"last_updated": c.metrics.lastUpdated,
	}
}
