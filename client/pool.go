package client

import (
	"context"
	"errors"
	"sync"
	"time"
)

var (
	// ErrPoolClosed 连接池已关闭
	ErrPoolClosed = errors.New("connection pool is closed")
	// ErrPoolEmpty 连接池为空
	ErrPoolEmpty = errors.New("connection pool is empty")
)

// PooledClient 池化的客户端连接
type PooledClient struct {
	client     *BurinClient
	pool       *ClientPool
	createTime time.Time
	lastUsed   time.Time
	inUse      bool
	mu         sync.Mutex
}

// Get 获取底层的 BurinClient
func (pc *PooledClient) Get() *BurinClient {
	pc.mu.Lock()
	pc.lastUsed = time.Now()
	pc.mu.Unlock()
	return pc.client
}

// Close 归还连接到池中
func (pc *PooledClient) Close() error {
	return pc.pool.Put(pc)
}

// IsHealthy 检查客户端是否健康
func (pc *PooledClient) IsHealthy() bool {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	if pc.client == nil {
		return false
	}

	// 简单检查：尝试获取集群信息
	// 如果超时则认为不健康
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	_, err := pc.client.GetClusterInfo(ctx)
	return err == nil
}

// realClose 真正关闭底层客户端
func (pc *PooledClient) realClose() error {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	if pc.client != nil {
		return pc.client.Disconnect()
	}
	return nil
}

// ClientPool 客户端连接池
type ClientPool struct {
	mu          sync.RWMutex
	config      *ClientConfig
	clients     chan *PooledClient
	factory     func() (*BurinClient, error)
	maxSize     int
	minSize     int
	currentSize int
	idleTimeout time.Duration
	maxLifetime time.Duration
	closed      bool
}

// NewClientPool 创建新的客户端连接池
func NewClientPool(config *ClientConfig, maxSize, minSize int, idleTimeout, maxLifetime time.Duration) (*ClientPool, error) {
	if config == nil {
		return nil, errors.New("config cannot be nil")
	}

	if maxSize <= 0 {
		maxSize = 10
	}
	if minSize < 0 {
		minSize = 0
	}
	if minSize > maxSize {
		minSize = maxSize
	}
	if idleTimeout <= 0 {
		idleTimeout = 5 * time.Minute
	}
	if maxLifetime <= 0 {
		maxLifetime = 30 * time.Minute
	}

	pool := &ClientPool{
		config:      config,
		clients:     make(chan *PooledClient, maxSize),
		maxSize:     maxSize,
		minSize:     minSize,
		currentSize: 0,
		idleTimeout: idleTimeout,
		maxLifetime: maxLifetime,
		closed:      false,
	}

	// 设置客户端工厂函数
	pool.factory = func() (*BurinClient, error) {
		client, err := NewClient(config)
		if err != nil {
			return nil, err
		}
		if err := client.Connect(); err != nil {
			return nil, err
		}
		return client, nil
	}

	// 预创建最小数量的连接
	for i := 0; i < minSize; i++ {
		pc, err := pool.createClient()
		if err != nil {
			// 清理已创建的连接
			pool.Close()
			return nil, err
		}
		pool.currentSize++
		pool.clients <- pc
	}

	// 启动清理 goroutine
	go pool.cleanupLoop()

	return pool, nil
}

// Get 从池中获取一个客户端
func (p *ClientPool) Get() (*PooledClient, error) {
	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return nil, ErrPoolClosed
	}
	p.mu.RUnlock()

	// 尝试从池中获取
	select {
	case pc := <-p.clients:
		// 检查连接是否过期或不健康
		if p.isExpired(pc) || !pc.IsHealthy() {
			pc.realClose()
			p.decrementSize()
			// 尝试创建新连接
			return p.createNewClient()
		}

		pc.mu.Lock()
		pc.inUse = true
		pc.lastUsed = time.Now()
		pc.mu.Unlock()

		return pc, nil
	default:
		// 池中没有可用连接，尝试创建新的
		return p.createNewClient()
	}
}

// Put 归还客户端到池中
func (p *ClientPool) Put(pc *PooledClient) error {
	if pc == nil {
		return nil
	}

	p.mu.RLock()
	closed := p.closed
	p.mu.RUnlock()

	pc.mu.Lock()
	pc.inUse = false
	pc.lastUsed = time.Now()
	pc.mu.Unlock()

	if closed {
		return pc.realClose()
	}

	// 检查连接是否过期或不健康
	if p.isExpired(pc) || !pc.IsHealthy() {
		pc.realClose()
		p.decrementSize()
		return nil
	}

	// 尝试归还到池中
	select {
	case p.clients <- pc:
		return nil
	default:
		// 池已满，关闭连接
		pc.realClose()
		p.decrementSize()
		return nil
	}
}

// createNewClient 创建新客户端
func (p *ClientPool) createNewClient() (*PooledClient, error) {
	p.mu.Lock()
	if p.currentSize >= p.maxSize {
		p.mu.Unlock()
		return nil, ErrPoolEmpty
	}
	p.currentSize++
	p.mu.Unlock()

	pc, err := p.createClient()
	if err != nil {
		p.decrementSize()
		return nil, err
	}

	pc.mu.Lock()
	pc.inUse = true
	pc.mu.Unlock()

	return pc, nil
}

// createClient 创建底层客户端
func (p *ClientPool) createClient() (*PooledClient, error) {
	client, err := p.factory()
	if err != nil {
		return nil, err
	}

	return &PooledClient{
		client:     client,
		pool:       p,
		createTime: time.Now(),
		lastUsed:   time.Now(),
		inUse:      false,
	}, nil
}

// isExpired 检查客户端是否过期
func (p *ClientPool) isExpired(pc *PooledClient) bool {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	now := time.Now()

	// 检查最大生命周期
	if now.Sub(pc.createTime) > p.maxLifetime {
		return true
	}

	// 检查空闲超时
	if !pc.inUse && now.Sub(pc.lastUsed) > p.idleTimeout {
		return true
	}

	return false
}

// decrementSize 减少当前连接数
func (p *ClientPool) decrementSize() {
	p.mu.Lock()
	p.currentSize--
	if p.currentSize < 0 {
		p.currentSize = 0
	}
	p.mu.Unlock()
}

// cleanupLoop 定期清理过期连接
func (p *ClientPool) cleanupLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.cleanup()
		}

		p.mu.RLock()
		if p.closed {
			p.mu.RUnlock()
			return
		}
		p.mu.RUnlock()
	}
}

// cleanup 清理过期连接
func (p *ClientPool) cleanup() {
	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return
	}
	p.mu.RUnlock()

	// 获取所有可用连接并检查
	var toRemove []*PooledClient
	var toKeep []*PooledClient

	// 从 channel 中取出所有连接
	for {
		select {
		case pc := <-p.clients:
			if p.isExpired(pc) {
				toRemove = append(toRemove, pc)
			} else {
				toKeep = append(toKeep, pc)
			}
		default:
			// channel 为空
			goto cleanup_done
		}
	}

cleanup_done:
	// 关闭过期连接
	for _, pc := range toRemove {
		pc.realClose()
		p.decrementSize()
	}

	// 归还未过期的连接
	for _, pc := range toKeep {
		select {
		case p.clients <- pc:
		default:
			// 不应该发生，但为了安全
			pc.realClose()
			p.decrementSize()
		}
	}

	// 确保最小连接数
	p.mu.Lock()
	needed := p.minSize - p.currentSize
	p.mu.Unlock()

	for i := 0; i < needed; i++ {
		pc, err := p.createClient()
		if err != nil {
			break
		}
		p.mu.Lock()
		p.currentSize++
		p.mu.Unlock()
		p.clients <- pc
	}
}

// Close 关闭连接池
func (p *ClientPool) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	p.mu.Unlock()

	// 关闭 channel 并清理所有连接
	close(p.clients)
	for pc := range p.clients {
		pc.realClose()
	}

	p.mu.Lock()
	p.currentSize = 0
	p.mu.Unlock()

	return nil
}

// Stats 获取连接池统计信息
func (p *ClientPool) Stats() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return map[string]interface{}{
		"max_size":     p.maxSize,
		"min_size":     p.minSize,
		"current_size": p.currentSize,
		"available":    len(p.clients),
		"in_use":       p.currentSize - len(p.clients),
		"idle_timeout": p.idleTimeout,
		"max_lifetime": p.maxLifetime,
		"closed":       p.closed,
	}
}
