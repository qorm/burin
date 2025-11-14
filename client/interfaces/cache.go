package interfaces

import (
	"time"
)

// CacheInterface 缓存操作接口
type CacheInterface interface {
	// 基本操作
	Get(key string, opts ...CacheOption) (*CacheResponse, error)
	Set(key string, value []byte, opts ...CacheOption) error
	Delete(key string, opts ...CacheOption) error
	Exists(key string, opts ...CacheOption) (bool, error)

	// 批量操作
	MGet(keys []string, opts ...CacheOption) (map[string]*CacheResponse, error)
	MSet(keyValues map[string][]byte, opts ...CacheOption) error
	MDelete(keys []string, opts ...CacheOption) error

	// 列表操作 - 支持通过 CacheOption 传递参数
	// 可以通过 WithPrefix, WithOffset, WithLimit 等选项设置参数
	ListKeys(opts ...CacheOption) ([]string, int64, error)
	CountKeys(opts ...CacheOption) (int64, error)
}

// CacheOption 缓存操作选项
type CacheOption func(*CacheOptions)

// CacheOptions 缓存操作选项结构
type CacheOptions struct {
	TTL        *time.Duration
	Database   string
	Metadata   map[string]string
	Consistent bool

	// 列表操作参数
	Prefix *string
	Offset *int
	Limit  *int
}

// CacheResponse 缓存响应
type CacheResponse struct {
	Key       string            `json:"key"`
	Value     []byte            `json:"value"`
	Found     bool              `json:"found"`
	TTL       time.Duration     `json:"ttl"`
	Database  string            `json:"database"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}
