package cProtocol

import (
	"fmt"
)

// ErrorCode 错误码定义
type ErrorCode int

const (
	// 连接相关错误 (1xxx)
	ErrCodeConnectionFailed  ErrorCode = 1001
	ErrCodeConnectionClosed  ErrorCode = 1002
	ErrCodeConnectionTimeout ErrorCode = 1003
	ErrCodeNoEndpoints       ErrorCode = 1004
	ErrCodeMaxConnsReached   ErrorCode = 1005

	// 请求相关错误 (2xxx)
	ErrCodeInvalidRequest        ErrorCode = 2001
	ErrCodeRequestTimeout        ErrorCode = 2002
	ErrCodeRequestTooLarge       ErrorCode = 2003
	ErrCodeInvalidProtocolHeader ErrorCode = 2004

	// 响应相关错误 (3xxx)
	ErrCodeInvalidResponse ErrorCode = 3001
	ErrCodeResponseTimeout ErrorCode = 3002
	ErrCodeServerError     ErrorCode = 3003

	// 路由相关错误 (4xxx)
	ErrCodeUnknownCommand      ErrorCode = 4001
	ErrCodeNoHandler           ErrorCode = 4002
	ErrCodeRouterNotConfigured ErrorCode = 4003

	// 重试相关错误 (5xxx)
	ErrCodeMaxRetriesExceeded ErrorCode = 5001
	ErrCodeRetryTimeout       ErrorCode = 5002

	// 熔断器相关错误 (6xxx)
	ErrCodeCircuitOpen     ErrorCode = 6001
	ErrCodeCircuitHalfOpen ErrorCode = 6002

	// 通用错误 (9xxx)
	ErrCodeInternal       ErrorCode = 9001
	ErrCodeNotRunning     ErrorCode = 9002
	ErrCodeAlreadyRunning ErrorCode = 9003
)

var errorMessages = map[ErrorCode]string{
	// 连接相关
	ErrCodeConnectionFailed:  "failed to establish connection",
	ErrCodeConnectionClosed:  "connection closed",
	ErrCodeConnectionTimeout: "connection timeout",
	ErrCodeNoEndpoints:       "no endpoints available",
	ErrCodeMaxConnsReached:   "maximum connections reached",

	// 请求相关
	ErrCodeInvalidRequest:        "invalid request format",
	ErrCodeRequestTimeout:        "request timeout",
	ErrCodeRequestTooLarge:       "request too large",
	ErrCodeInvalidProtocolHeader: "invalid protocol header",

	// 响应相关
	ErrCodeInvalidResponse: "invalid response format",
	ErrCodeResponseTimeout: "response timeout",
	ErrCodeServerError:     "server internal error",

	// 路由相关
	ErrCodeUnknownCommand:      "unknown command",
	ErrCodeNoHandler:           "no handler registered",
	ErrCodeRouterNotConfigured: "router not configured",

	// 重试相关
	ErrCodeMaxRetriesExceeded: "maximum retries exceeded",
	ErrCodeRetryTimeout:       "retry timeout",

	// 熔断器相关
	ErrCodeCircuitOpen:     "circuit breaker open",
	ErrCodeCircuitHalfOpen: "circuit breaker half-open",

	// 通用
	ErrCodeInternal:       "internal error",
	ErrCodeNotRunning:     "not running",
	ErrCodeAlreadyRunning: "already running",
}

// ProtocolError 协议错误
type ProtocolError struct {
	Code    ErrorCode
	Message string
	Cause   error
}

func (e *ProtocolError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%d] %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%d] %s", e.Code, e.Message)
}

func (e *ProtocolError) Unwrap() error {
	return e.Cause
}

// NewError 创建新的协议错误
func NewError(code ErrorCode, cause error) *ProtocolError {
	message := errorMessages[code]
	if message == "" {
		message = "unknown error"
	}

	return &ProtocolError{
		Code:    code,
		Message: message,
		Cause:   cause,
	}
}

// NewErrorWithMessage 创建带自定义消息的协议错误
func NewErrorWithMessage(code ErrorCode, message string, cause error) *ProtocolError {
	return &ProtocolError{
		Code:    code,
		Message: message,
		Cause:   cause,
	}
}

// IsRetriable 判断错误是否可重试
func IsRetriable(err error) bool {
	if err == nil {
		return false
	}

	// 检查是否是ProtocolError
	if pe, ok := err.(*ProtocolError); ok {
		switch pe.Code {
		case ErrCodeConnectionFailed,
			ErrCodeConnectionTimeout,
			ErrCodeRequestTimeout,
			ErrCodeResponseTimeout,
			ErrCodeServerError:
			return true
		default:
			return false
		}
	}

	// 其他错误默认不可重试
	return false
}

// IsCircuitBreakerError 判断是否是熔断器错误
func IsCircuitBreakerError(err error) bool {
	if err == nil {
		return false
	}

	if pe, ok := err.(*ProtocolError); ok {
		return pe.Code == ErrCodeCircuitOpen || pe.Code == ErrCodeCircuitHalfOpen
	}

	return false
}

// GetErrorCode 获取错误码
func GetErrorCode(err error) ErrorCode {
	if err == nil {
		return 0
	}

	if pe, ok := err.(*ProtocolError); ok {
		return pe.Code
	}

	return ErrCodeInternal
}
