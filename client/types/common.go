package types

import (
    "fmt"
    "time"
)

// TTL 统一的TTL类型定义，解决重复定义问题
type TTL struct {
    Duration time.Duration
    IsZero   bool
}

// NewTTL 创建TTL实例
func NewTTL(d time.Duration) TTL {
    return TTL{
        Duration: d,
        IsZero:   d == 0,
    }
}

// ToDuration 转换为time.Duration
func (t TTL) ToDuration() time.Duration {
    if t.IsZero {
        return 0
    }
    return t.Duration
}

// String 返回字符串表示
func (t TTL) String() string {
    if t.IsZero {
        return "never"
    }
    return t.Duration.String()
}

// ErrorCode 错误码类型
type ErrorCode string

const (
    ErrInvalidKey     ErrorCode = "INVALID_KEY"
    ErrKeyNotFound    ErrorCode = "KEY_NOT_FOUND"
    ErrConnectionFail ErrorCode = "CONNECTION_FAIL"
    ErrTimeout        ErrorCode = "TIMEOUT"
    ErrInvalidValue   ErrorCode = "INVALID_VALUE"
    ErrDatabaseNotFound ErrorCode = "DATABASE_NOT_FOUND"
    ErrQueueNotFound    ErrorCode = "QUEUE_NOT_FOUND"
    ErrTransactionFail  ErrorCode = "TRANSACTION_FAIL"
)

// BurinError 统一错误类型
type BurinError struct {
    Code      ErrorCode         `json:"code"`
    Message   string           `json:"message"`
    Operation string           `json:"operation"`
    Context   map[string]string `json:"context,omitempty"`
    Cause     error            `json:"-"`
}

func (e *BurinError) Error() string {
    return fmt.Sprintf("[%s] %s: %s", e.Code, e.Operation, e.Message)
}

func (e *BurinError) Unwrap() error {
    return e.Cause
}

// NewError 创建新的Burin错误
func NewError(code ErrorCode, operation, message string) *BurinError {
    return &BurinError{
        Code:      code,
        Operation: operation,
        Message:   message,
        Context:   make(map[string]string),
    }
}

// WithCause 添加原因错误
func (e *BurinError) WithCause(cause error) *BurinError {
    e.Cause = cause
    return e
}

// WithContext 添加上下文信息
func (e *BurinError) WithContext(key, value string) *BurinError {
    if e.Context == nil {
        e.Context = make(map[string]string)
    }
    e.Context[key] = value
    return e
}
