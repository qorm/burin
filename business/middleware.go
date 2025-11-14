package business

import (
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// RequestValidator 请求验证器
type RequestValidator struct {
	logger *logrus.Logger
}

// NewRequestValidator 创建请求验证器
func NewRequestValidator(logger *logrus.Logger) *RequestValidator {
	return &RequestValidator{
		logger: logger,
	}
}

// ValidateCacheRequest 验证缓存请求
func (rv *RequestValidator) ValidateCacheRequest(req *CacheRequest) error {
	if req.Key == "" {
		return fmt.Errorf("key cannot be empty")
	}

	if len(req.Key) > 1024 {
		return fmt.Errorf("key too long: max 1024 bytes")
	}

	if req.Database != "" && len(req.Database) > 256 {
		return fmt.Errorf("database name too long: max 256 bytes")
	}

	if len(req.Value) > 10*1024*1024 { // 10MB
		return fmt.Errorf("value too large: max 10MB")
	}

	return nil
}

// ValidateTransactionRequest 验证事务请求
func (rv *RequestValidator) ValidateTransactionRequest(req *TransactionRequest) error {
	if req.Operation == "" {
		return fmt.Errorf("operation cannot be empty")
	}

	validOps := map[string]bool{
		"begin": true, "commit": true, "rollback": true,
		"execute": true, "status": true,
	}

	if !validOps[req.Operation] {
		return fmt.Errorf("invalid operation: %s", req.Operation)
	}

	if req.Operation == "execute" && len(req.Commands) == 0 {
		return fmt.Errorf("execute operation requires commands")
	}

	if req.Operation == "execute" && len(req.Commands) > 1000 {
		return fmt.Errorf("too many commands: max 1000")
	}

	return nil
}

// RateLimiter 速率限制器
type RateLimiter struct {
	mu              sync.RWMutex
	buckets         map[string]*TokenBucket
	logger          *logrus.Logger
	maxRate         int // 每秒最大请求数
	burstSize       int // 突发大小
	cleanupInterval time.Duration
}

// TokenBucket 令牌桶
type TokenBucket struct {
	tokens     float64
	maxTokens  float64
	refillRate float64
	lastRefill time.Time
	mu         sync.Mutex
}

// NewRateLimiter 创建速率限制器
func NewRateLimiter(maxRate, burstSize int, logger *logrus.Logger) *RateLimiter {
	rl := &RateLimiter{
		buckets:         make(map[string]*TokenBucket),
		logger:          logger,
		maxRate:         maxRate,
		burstSize:       burstSize,
		cleanupInterval: 5 * time.Minute,
	}

	// 启动清理协程
	go rl.cleanup()

	return rl
}

// Allow 检查是否允许请求
func (rl *RateLimiter) Allow(clientID string) bool {
	rl.mu.Lock()
	bucket, exists := rl.buckets[clientID]
	if !exists {
		bucket = &TokenBucket{
			tokens:     float64(rl.burstSize),
			maxTokens:  float64(rl.burstSize),
			refillRate: float64(rl.maxRate),
			lastRefill: time.Now(),
		}
		rl.buckets[clientID] = bucket
	}
	rl.mu.Unlock()

	return bucket.Take()
}

// Take 尝试获取一个令牌
func (tb *TokenBucket) Take() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(tb.lastRefill).Seconds()

	// 补充令牌
	tb.tokens += elapsed * tb.refillRate
	if tb.tokens > tb.maxTokens {
		tb.tokens = tb.maxTokens
	}

	tb.lastRefill = now

	// 尝试消费令牌
	if tb.tokens >= 1.0 {
		tb.tokens -= 1.0
		return true
	}

	return false
}

// cleanup 清理过期的令牌桶
func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(rl.cleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		rl.mu.Lock()

		now := time.Now()
		for id, bucket := range rl.buckets {
			bucket.mu.Lock()
			if now.Sub(bucket.lastRefill) > 10*time.Minute {
				delete(rl.buckets, id)
			}
			bucket.mu.Unlock()
		}

		rl.mu.Unlock()
	}
}

// CircuitBreaker 熔断器
type CircuitBreaker struct {
	mu              sync.RWMutex
	state           CircuitState
	failureCount    int
	successCount    int
	lastFailureTime time.Time
	lastStateChange time.Time

	// 配置
	failureThreshold int           // 失败阈值
	successThreshold int           // 成功阈值（半开状态）
	timeout          time.Duration // 超时时间

	logger *logrus.Logger
}

// CircuitState 熔断器状态
type CircuitState int

const (
	StateClosed CircuitState = iota
	StateOpen
	StateHalfOpen
)

// NewCircuitBreaker 创建熔断器
func NewCircuitBreaker(failureThreshold, successThreshold int, timeout time.Duration, logger *logrus.Logger) *CircuitBreaker {
	return &CircuitBreaker{
		state:            StateClosed,
		failureThreshold: failureThreshold,
		successThreshold: successThreshold,
		timeout:          timeout,
		logger:           logger,
		lastStateChange:  time.Now(),
	}
}

// Call 执行调用
func (cb *CircuitBreaker) Call(fn func() error) error {
	if !cb.AllowRequest() {
		return fmt.Errorf("circuit breaker is open")
	}

	err := fn()

	if err != nil {
		cb.RecordFailure()
		return err
	}

	cb.RecordSuccess()
	return nil
}

// AllowRequest 是否允许请求
func (cb *CircuitBreaker) AllowRequest() bool {
	cb.mu.RLock()
	state := cb.state
	lastStateChange := cb.lastStateChange
	cb.mu.RUnlock()

	switch state {
	case StateClosed:
		return true
	case StateOpen:
		// 检查是否到达超时时间，可以转为半开状态
		if time.Since(lastStateChange) > cb.timeout {
			cb.mu.Lock()
			if cb.state == StateOpen {
				cb.state = StateHalfOpen
				cb.successCount = 0
				cb.lastStateChange = time.Now()
				cb.logger.Info("Circuit breaker transitioning to half-open")
			}
			cb.mu.Unlock()
			return true
		}
		return false
	case StateHalfOpen:
		return true
	default:
		return true
	}
}

// RecordSuccess 记录成功
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failureCount = 0

	if cb.state == StateHalfOpen {
		cb.successCount++
		if cb.successCount >= cb.successThreshold {
			cb.state = StateClosed
			cb.lastStateChange = time.Now()
			cb.logger.Info("Circuit breaker closed")
		}
	}
}

// RecordFailure 记录失败
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failureCount++
	cb.lastFailureTime = time.Now()

	if cb.state == StateHalfOpen {
		cb.state = StateOpen
		cb.lastStateChange = time.Now()
		cb.logger.Warn("Circuit breaker opened from half-open")
		return
	}

	if cb.state == StateClosed && cb.failureCount >= cb.failureThreshold {
		cb.state = StateOpen
		cb.lastStateChange = time.Now()
		cb.logger.Warnf("Circuit breaker opened after %d failures", cb.failureCount)
	}
}

// GetState 获取状态
func (cb *CircuitBreaker) GetState() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// Reset 重置熔断器
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.state = StateClosed
	cb.failureCount = 0
	cb.successCount = 0
	cb.lastStateChange = time.Now()
	cb.logger.Info("Circuit breaker reset")
}

// AuthResult 认证结果
type AuthResult struct {
	Authenticated bool   // 是否已认证
	Username      string // 用户名
	Error         error  // 错误信息
}

// AuthError 认证错误
type AuthError struct {
	Code    int    // 错误代码：401=未认证，403=无权限
	Message string // 错误消息
}

func (e *AuthError) Error() string {
	return e.Message
}
