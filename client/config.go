package client

import (
	"fmt"
	"time"

	"os"

	"gopkg.in/yaml.v2"
)

// ClientConfig 客户端配置
type ClientConfig struct {
	Connection  ConnectionConfig  `yaml:"connection" json:"connection"`
	Auth        AuthConfig        `yaml:"auth" json:"auth"`
	Cache       CacheConfig       `yaml:"cache" json:"cache"`
	Transaction TransactionConfig `yaml:"transaction" json:"transaction"`
	Cluster     ClusterConfig     `yaml:"cluster" json:"cluster"`
	Logging     LoggingConfig     `yaml:"logging" json:"logging"`
	Metrics     MetricsConfig     `yaml:"metrics" json:"metrics"`
}

// ConnectionConfig 连接配置
type ConnectionConfig struct {
	Endpoint            string        `yaml:"endpoint" json:"endpoint"`
	MaxConnsPerEndpoint int           `yaml:"max_conns_per_endpoint" json:"max_conns_per_endpoint"`
	DialTimeout         time.Duration `yaml:"dial_timeout" json:"dial_timeout"`
	ReadTimeout         time.Duration `yaml:"read_timeout" json:"read_timeout"`
	WriteTimeout        time.Duration `yaml:"write_timeout" json:"write_timeout"`
	KeepAlive           time.Duration `yaml:"keep_alive" json:"keep_alive"`
	RetryCount          int           `yaml:"retry_count" json:"retry_count"`
	RetryDelay          time.Duration `yaml:"retry_delay" json:"retry_delay"`
}

// AuthConfig 认证配置
type AuthConfig struct {
	Username string `yaml:"username" json:"username"` // 用户名（如果提供则自动登录）
	Password string `yaml:"password" json:"password"` // 密码
}

// CacheConfig 缓存配置
type CacheConfig struct {
	DefaultDatabase string        `yaml:"default_database" json:"default_database"`
	DefaultTTL      time.Duration `yaml:"default_ttl" json:"default_ttl"`
	MaxKeySize      int           `yaml:"max_key_size" json:"max_key_size"`
	MaxValueSize    int           `yaml:"max_value_size" json:"max_value_size"`
}

// TransactionConfig 事务配置
type TransactionConfig struct {
	Timeout        time.Duration `yaml:"timeout" json:"timeout"`
	RetryCount     int           `yaml:"retry_count" json:"retry_count"`
	IsolationLevel string        `yaml:"isolation_level" json:"isolation_level"`
}

// ClusterConfig 集群配置
type ClusterConfig struct {
	HealthCheckInterval time.Duration `yaml:"health_check_interval" json:"health_check_interval"`
}

// LoggingConfig 日志配置
type LoggingConfig struct {
	Level      string `yaml:"level" json:"level"`
	Format     string `yaml:"format" json:"format"`
	Output     string `yaml:"output" json:"output"`
	Structured bool   `yaml:"structured" json:"structured"`
}

// MetricsConfig 监控配置
type MetricsConfig struct {
	Enabled         bool          `yaml:"enabled" json:"enabled"`
	CollectInterval time.Duration `yaml:"collect_interval" json:"collect_interval"`
	Endpoint        string        `yaml:"endpoint" json:"endpoint"`
}

// NewDefaultConfig 创建默认配置
func NewDefaultConfig() *ClientConfig {
	return &ClientConfig{
		Connection: ConnectionConfig{
			Endpoint:            "localhost:8099",
			MaxConnsPerEndpoint: 10,
			DialTimeout:         5 * time.Second,
			ReadTimeout:         10 * time.Second,
			WriteTimeout:        10 * time.Second,
			KeepAlive:           30 * time.Second,
			RetryCount:          3,
			RetryDelay:          1 * time.Second,
		},
		Auth: AuthConfig{
			Username: "", // 留空表示不登录
			Password: "",
		},
		Cache: CacheConfig{
			DefaultDatabase: "default",
			DefaultTTL:      0,
			MaxKeySize:      1024,
			MaxValueSize:    1024 * 1024, // 1MB
		},
		Transaction: TransactionConfig{
			Timeout:        30 * time.Second,
			RetryCount:     3,
			IsolationLevel: "read_committed",
		},
		Cluster: ClusterConfig{
			HealthCheckInterval: 5 * time.Second,
		},
		Logging: LoggingConfig{
			Level:      "info",
			Format:     "text",
			Output:     "stdout",
			Structured: false,
		},
		Metrics: MetricsConfig{
			Enabled:         true,
			CollectInterval: 30 * time.Second,
			Endpoint:        "",
		},
	}
}

// LoadFromYAML 从YAML文件加载配置
func (c *ClientConfig) LoadFromYAML(filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("读取配置文件失败: %w", err)
	}

	if err := yaml.Unmarshal(data, c); err != nil {
		return fmt.Errorf("解析YAML配置失败: %w", err)
	}

	return c.Validate()
}

// Validate 验证配置
func (c *ClientConfig) Validate() error {
	if c.Connection.Endpoint == "" {
		return fmt.Errorf("需要指定服务器端点")
	}

	if c.Connection.DialTimeout <= 0 {
		return fmt.Errorf("连接超时必须大于0")
	}

	if c.Cache.MaxKeySize <= 0 {
		return fmt.Errorf("最大Key大小必须大于0")
	}

	return nil
}

// Merge 合并配置
func (c *ClientConfig) Merge(other *ClientConfig) *ClientConfig {
	// 创建副本
	merged := *c

	// 合并非零值
	if other.Connection.Endpoint != "" {
		merged.Connection.Endpoint = other.Connection.Endpoint
	}

	if other.Connection.DialTimeout > 0 {
		merged.Connection.DialTimeout = other.Connection.DialTimeout
	}

	// TODO: 实现完整的合并逻辑

	return &merged
}
