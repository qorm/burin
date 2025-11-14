package business

import (
	"fmt"
	"time"

	"github.com/bytedance/sonic"
)

// ============================================
// 自定义类型
// ============================================

// TTLDuration 自定义Duration类型，支持整数秒数和标准Duration格式
type TTLDuration time.Duration

// UnmarshalJSON 自定义JSON解析，整数视为秒数
func (d *TTLDuration) UnmarshalJSON(data []byte) error {
	// 处理null值
	if string(data) == "null" {
		*d = TTLDuration(0)
		return nil
	}

	// 尝试解析为客户端格式：{"seconds":N,"unit":"seconds"}
	var clientFormat struct {
		Seconds int64  `json:"seconds"`
		Unit    string `json:"unit,omitempty"`
	}
	if err := sonic.Unmarshal(data, &clientFormat); err == nil {
		*d = TTLDuration(time.Duration(clientFormat.Seconds) * time.Second)
		return nil
	}

	// 尝试解析为数字（秒）
	var seconds float64
	if err := sonic.Unmarshal(data, &seconds); err == nil {
		*d = TTLDuration(time.Duration(seconds) * time.Second)
		return nil
	}

	// 尝试解析为字符串（标准Duration格式）
	var str string
	if err := sonic.Unmarshal(data, &str); err == nil {
		duration, err := time.ParseDuration(str)
		if err != nil {
			return err
		}
		*d = TTLDuration(duration)
		return nil
	}

	return fmt.Errorf("invalid TTL format, received: %s", string(data))
}

// MarshalJSON 实现JSON序列化
func (d TTLDuration) MarshalJSON() ([]byte, error) {
	if d == 0 {
		return sonic.Marshal(nil)
	}

	// 序列化为客户端兼容格式
	seconds := int64(time.Duration(d).Seconds())
	result := map[string]interface{}{
		"seconds": seconds,
		"unit":    "seconds",
	}
	return sonic.Marshal(result)
}

// Duration 转换为标准Duration
func (d TTLDuration) Duration() time.Duration {
	return time.Duration(d)
}

// ============================================
// 事务相关类型
// ============================================

// TransactionRequest 事务请求
type TransactionRequest struct {
	Operation     string                 `json:"operation"` // begin, commit, rollback, execute, status
	TransactionID string                 `json:"transaction_id,omitempty"`
	Type          string                 `json:"type,omitempty"`         // read-only, read-write
	Participants  []string               `json:"participants,omitempty"` // 参与者节点列表
	Commands      []TransactionCommand   `json:"commands,omitempty"`     // 事务内的命令
	Timeout       int64                  `json:"timeout,omitempty"`      // 超时时间(毫秒)
	Metadata      map[string]interface{} `json:"metadata,omitempty"`     // 附加元数据
}

// TransactionCommand 事务命令
type TransactionCommand struct {
	Type     string                 `json:"type"`               // set, get, delete
	Database string                 `json:"database,omitempty"` // 数据库名
	Key      string                 `json:"key"`
	Value    []byte                 `json:"value,omitempty"`
	TTL      TTLDuration            `json:"ttl,omitempty"`
	Meta     map[string]interface{} `json:"meta,omitempty"`
}

// TransactionResponse 事务响应
type TransactionResponse struct {
	TransactionID string                 `json:"transaction_id"`
	Status        string                 `json:"status"` // success, error, pending, committed, aborted
	Results       []TransactionResult    `json:"results,omitempty"`
	Error         string                 `json:"error,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

// TransactionResult 事务结果
type TransactionResult struct {
	Key     string `json:"key"`
	Value   []byte `json:"value,omitempty"`
	Found   bool   `json:"found"`
	Error   string `json:"error,omitempty"`
	Success bool   `json:"success"`
}

// ============================================
// 缓存相关类型
// ============================================

// CacheRequest 缓存请求数据结构
type CacheRequest struct {
	Database     string      `json:"database,omitempty"` // 数据库名
	Key          string      `json:"key"`
	Value        string      `json:"value,omitempty"` // 改为string类型
	TTL          TTLDuration `json:"ttl,omitempty"`
	IsReplicated bool        `json:"_replicated,omitempty"` // 是否为复制请求
}

// CacheResponse 缓存响应数据结构
type CacheResponse struct {
	Key    string `json:"key"`
	Value  string `json:"value,omitempty"`
	Found  bool   `json:"found"`
	Error  string `json:"error,omitempty"`
	Status string `json:"status"`
}

// ============================================
// 统计和列表相关类型
// ============================================

// CountRequest 统计请求数据结构
type CountRequest struct {
	Database string `json:"database,omitempty"` // 数据库名，默认为"default"
	Prefix   string `json:"prefix,omitempty"`   // 键前缀，为空则统计所有键
}

// CountResponse 统计响应数据结构
type CountResponse struct {
	Database string `json:"database"`
	Prefix   string `json:"prefix"`
	Count    int64  `json:"count"`
	Status   string `json:"status"`
	Error    string `json:"error,omitempty"`
}

// ListRequest 列表请求数据结构
type ListRequest struct {
	Database string `json:"database,omitempty"` // 数据库名，默认为"default"
	Prefix   string `json:"prefix,omitempty"`   // 键前缀，为空则列出所有键
	Offset   int    `json:"offset,omitempty"`   // 偏移量，默认为0
	Limit    int    `json:"limit,omitempty"`    // 限制数量，默认为-1(全部)
}

// ListResponse 列表响应数据结构
type ListResponse struct {
	Database string   `json:"database"`
	Prefix   string   `json:"prefix"`
	Keys     []string `json:"keys"`
	Total    int64    `json:"total"`    // 总数量(不受分页限制)
	Offset   int      `json:"offset"`   // 当前偏移量
	Limit    int      `json:"limit"`    // 当前限制数量
	HasMore  bool     `json:"has_more"` // 是否还有更多数据
	Status   string   `json:"status"`
	Error    string   `json:"error,omitempty"`
}

// ============================================
// 辅助函数
// ============================================

// parseCacheRequest 解析缓存请求
func parseCacheRequest(data []byte) (*CacheRequest, error) {
	var req CacheRequest
	if err := sonic.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("failed to parse cache request: %w", err)
	}
	return &req, nil
}

// parseCountRequest 解析统计请求
func parseCountRequest(data []byte) (*CountRequest, error) {
	var req CountRequest
	if err := sonic.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("failed to parse count request: %w", err)
	}
	return &req, nil
}

// parseListRequest 解析列表请求
func parseListRequest(data []byte) (*ListRequest, error) {
	var req ListRequest
	if err := sonic.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("failed to parse list request: %w", err)
	}
	return &req, nil
}

// buildCacheResponse 构建缓存响应
func buildCacheResponse(key string, value []byte, found bool, err error) *CacheResponse {
	resp := &CacheResponse{
		Key:   key,
		Found: found,
	}

	// 将[]byte直接转换为string，避免base64编码
	if value != nil {
		resp.Value = string(value)
	}

	if err != nil {
		resp.Error = err.Error()
		resp.Status = "error"
	} else if found {
		resp.Status = "success"
	} else {
		resp.Status = "not_found"
	}

	return resp
}
