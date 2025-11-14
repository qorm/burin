package cProtocol

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
)

// Connection 连接包装器
type Connection struct {
	conn         net.Conn
	endpoint     string
	lastUsed     time.Time
	requestCount int64
	mu           sync.RWMutex
}

func (c *Connection) GetConn() net.Conn {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastUsed = time.Now()
	atomic.AddInt64(&c.requestCount, 1)
	return c.conn
}

func (c *Connection) IsHealthy(timeout time.Duration) bool {
	if c.conn == nil {
		return false
	}

	// 简单的健康检查：设置短超时并尝试写入
	c.conn.SetWriteDeadline(time.Now().Add(timeout))
	_, err := c.conn.Write([]byte{})
	return err == nil
}

func (c *Connection) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// Client 协议客户端
type Client struct {
	mu         sync.RWMutex
	endpoint   string
	connection *Connection
	logger     *logrus.Logger

	// 配置
	dialTimeout         time.Duration
	readTimeout         time.Duration
	writeTimeout        time.Duration
	healthCheckInterval time.Duration

	// 连接初始化回调（用于登录等操作）
	onNewConnection func(conn net.Conn) error

	// 状态
	running int32
	doneCh  chan struct{}
}

// ClientConfig 客户端配置
type ClientConfig struct {
	Endpoint            string
	MaxConnsPerEndpoint int // 保留此字段以保持 API 兼容性，但不再使用
	DialTimeout         time.Duration
	ReadTimeout         time.Duration
	WriteTimeout        time.Duration
	HealthCheckInterval time.Duration
}

// NewClient 创建新的协议客户端
func NewClient(config *ClientConfig, logger *logrus.Logger) *Client {
	if config.DialTimeout == 0 {
		config.DialTimeout = 5 * time.Second
	}
	if config.ReadTimeout == 0 {
		config.ReadTimeout = 30 * time.Second
	}
	if config.WriteTimeout == 0 {
		config.WriteTimeout = 30 * time.Second
	}
	if config.HealthCheckInterval == 0 {
		config.HealthCheckInterval = 30 * time.Second
	}

	return &Client{
		endpoint:            config.Endpoint,
		connection:          nil, // 将在 Start() 中初始化
		dialTimeout:         config.DialTimeout,
		readTimeout:         config.ReadTimeout,
		writeTimeout:        config.WriteTimeout,
		healthCheckInterval: config.HealthCheckInterval,
		logger:              logger,
		doneCh:              make(chan struct{}),
	}
}

// Start 启动客户端
func (c *Client) Start() error {
	if !atomic.CompareAndSwapInt32(&c.running, 0, 1) {
		return errors.New("client already running")
	}

	// 创建连接
	conn, err := net.DialTimeout("tcp", c.endpoint, c.dialTimeout)
	if err != nil {
		atomic.StoreInt32(&c.running, 0)
		return fmt.Errorf("failed to connect to %s: %w", c.endpoint, err)
	}

	// 如果设置了连接初始化回调，执行它
	if c.onNewConnection != nil {
		if err := c.onNewConnection(conn); err != nil {
			conn.Close()
			atomic.StoreInt32(&c.running, 0)
			return fmt.Errorf("connection initialization failed: %w", err)
		}
	}

	c.mu.Lock()
	c.connection = &Connection{
		conn:     conn,
		endpoint: c.endpoint,
		lastUsed: time.Now(),
	}
	c.mu.Unlock()

	// 启动健康检查
	go c.healthCheckLoop()

	c.logger.Infof("cProtocol client connected to %s", c.endpoint)
	return nil
}

// SetOnNewConnection 设置新连接创建时的回调函数
func (c *Client) SetOnNewConnection(callback func(conn net.Conn) error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onNewConnection = callback
}

// Stop 停止客户端
func (c *Client) Stop() error {
	if !atomic.CompareAndSwapInt32(&c.running, 1, 0) {
		return errors.New("client not running")
	}

	close(c.doneCh)

	// 关闭连接
	c.mu.Lock()
	if c.connection != nil {
		c.connection.Close()
		c.connection = nil
	}
	c.mu.Unlock()

	c.logger.Info("cProtocol client stopped")
	return nil
}

// SendRequest 发送请求
func (c *Client) SendRequest(ctx context.Context, req *ProtocolRequest) (*ProtocolResponse, error) {
	if atomic.LoadInt32(&c.running) != 1 {
		return nil, errors.New("client not running")
	}

	return c.sendRequestToEndpoint(ctx, req, c.endpoint)
}

// sendRequestToEndpoint 向指定端点发送请求
func (c *Client) sendRequestToEndpoint(ctx context.Context, req *ProtocolRequest, endpoint string) (*ProtocolResponse, error) {
	// 获取连接
	conn, err := c.getConnection()
	if err != nil {
		return nil, fmt.Errorf("failed to get connection to %s: %w", endpoint, err)
	}

	// 发送请求
	if err := c.sendRequest(conn, req); err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	// 接收响应
	resp, err := c.receiveResponse(conn)
	if err != nil {
		return nil, fmt.Errorf("failed to receive response: %w", err)
	}

	// 协议层统一处理状态码
	// 非 2xx 状态码视为错误，直接返回错误而不是响应对象
	if resp.Status < 200 || resp.Status >= 300 {
		if resp.Error != "" {
			return nil, fmt.Errorf("request failed with status %d: %s", resp.Status, resp.Error)
		}
		return nil, fmt.Errorf("request failed with status %d", resp.Status)
	}

	return resp, nil
}

// getConnection 获取连接
func (c *Client) getConnection() (net.Conn, error) {
	c.mu.RLock()
	connection := c.connection
	c.mu.RUnlock()

	if connection == nil {
		return nil, errors.New("connection not initialized")
	}

	return connection.GetConn(), nil
}

// sendRequest 发送请求
func (c *Client) sendRequest(conn net.Conn, req *ProtocolRequest) error {
	// 使用协议头（必须存在）
	if req.Head == nil {
		return fmt.Errorf("protocol head is required")
	}

	head := req.Head

	// 确保设置正确的数据长度
	head.SetContentLength(uint32(len(req.Data)))

	// 设置写超时
	conn.SetWriteDeadline(time.Now().Add(c.writeTimeout))

	// 发送头部
	headBytes := head.GetBytes()
	totalWritten := 0
	for totalWritten < len(headBytes) {
		n, err := conn.Write(headBytes[totalWritten:])
		if err != nil {
			return err
		}
		totalWritten += n
	}

	// 发送数据（处理大数据包）
	if len(req.Data) > 0 {
		totalWritten = 0
		for totalWritten < len(req.Data) {
			n, err := conn.Write(req.Data[totalWritten:])
			if err != nil {
				return err
			}
			totalWritten += n
		}
	}

	return nil
}

// receiveResponse 接收响应
func (c *Client) receiveResponse(conn net.Conn) (*ProtocolResponse, error) {
	// 设置读超时
	conn.SetReadDeadline(time.Now().Add(c.readTimeout))

	// 读取协议头部
	buffer := make([]byte, 6)
	totalRead := 0
	for totalRead < 6 {
		n, err := conn.Read(buffer[totalRead:])
		if err != nil {
			return nil, err
		}
		totalRead += n
	}

	// 解析头部
	head, err := HeadFromBytes(buffer)
	if err != nil {
		return nil, err
	}

	// 读取数据（处理大数据包）
	dataLen := head.GetContentLength()
	var data []byte
	if dataLen > 0 {
		data = make([]byte, dataLen)
		totalRead = 0
		for totalRead < int(dataLen) {
			n, err := conn.Read(data[totalRead:])
			if err != nil {
				return nil, err
			}
			totalRead += n
		}
	}

	// 优化协议：所有响应统一格式
	// 前2字节：状态码（Big Endian）
	// 后续字节：业务数据（状态码200）或错误消息（其他状态码）

	status := 200
	errorMsg := ""
	var responseData []byte

	if len(data) >= 2 {
		// 解析状态码（Big Endian）
		status = int(data[0])<<8 | int(data[1])

		if status == 200 {
			// 正常响应：提取业务数据（跳过前2字节状态码）
			if len(data) > 2 {
				responseData = data[2:]
			} else {
				responseData = []byte{}
			}
		} else {
			// 错误响应：提取错误消息（跳过前2字节状态码）
			if len(data) > 2 {
				errorMsg = string(data[2:])
			}
			responseData = nil
		}
	} else {
		// 数据不足2字节，视为空响应
		responseData = data
	}

	return &ProtocolResponse{
		Head:   head,
		Status: status,
		Data:   responseData,
		Error:  errorMsg,
	}, nil
}

// healthCheckLoop 健康检查循环
func (c *Client) healthCheckLoop() {
	ticker := time.NewTicker(c.healthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.performHealthCheck()
		case <-c.doneCh:
			return
		}
	}
}

// performHealthCheck 执行健康检查
func (c *Client) performHealthCheck() {
	c.mu.RLock()
	connection := c.connection
	c.mu.RUnlock()

	if connection == nil {
		return
	}

	// 检查连接健康状态
	if !connection.IsHealthy(1 * time.Second) {
		c.logger.Warn("Connection unhealthy, attempting to reconnect...")
		c.reconnect()
	}
}

// reconnect 重新连接
func (c *Client) reconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 关闭旧连接
	if c.connection != nil {
		c.connection.Close()
		c.connection = nil
	}

	// 创建新连接
	conn, err := net.DialTimeout("tcp", c.endpoint, c.dialTimeout)
	if err != nil {
		c.logger.Errorf("Failed to reconnect: %v", err)
		return err
	}

	// 如果设置了连接初始化回调，执行它
	if c.onNewConnection != nil {
		if err := c.onNewConnection(conn); err != nil {
			conn.Close()
			c.logger.Errorf("Connection initialization failed: %v", err)
			return err
		}
	}

	c.connection = &Connection{
		conn:     conn,
		endpoint: c.endpoint,
		lastUsed: time.Now(),
	}

	c.logger.Info("Reconnected successfully")
	return nil
}

// GetStats 获取客户端统计信息
func (c *Client) GetStats() map[string]interface{} {
	c.mu.RLock()
	connection := c.connection
	c.mu.RUnlock()

	stats := make(map[string]interface{})
	stats["endpoint"] = c.endpoint
	stats["running"] = atomic.LoadInt32(&c.running) == 1

	if connection != nil {
		connection.mu.RLock()
		stats["connected"] = true
		stats["last_used"] = connection.lastUsed
		stats["request_count"] = atomic.LoadInt64(&connection.requestCount)
		connection.mu.RUnlock()
	} else {
		stats["connected"] = false
	}

	return stats
}
