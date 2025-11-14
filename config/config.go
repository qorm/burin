package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"burin/cProtocol"
	"burin/consensus"
	"burin/store"
	"burin/transaction"

	"github.com/fsnotify/fsnotify"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

// Config 应用程序配置
type Config struct {
	// 应用信息
	App AppConfig `mapstructure:"app"`
	// 日志配置
	Log LogConfig `mapstructure:"log"`
	// 缓存配置
	Cache store.Config `mapstructure:"cache"`
	// 共识配置
	Consensus consensus.NodeConfig `mapstructure:"consensus"`
	// 事务配置
	Transaction transaction.TransactionConfig `mapstructure:"transaction"`
	// Burin服务器配置（客户端服务）
	Burin cProtocol.ServerConfig `mapstructure:"burin"`
}

// AppConfig 应用程序配置
type AppConfig struct {
	Name            string `mapstructure:"name"`
	Version         string `mapstructure:"version"`
	Environment     string `mapstructure:"environment"`
	NodeID          string `mapstructure:"node_id"`
	DataDir         string `mapstructure:"data_dir"`
	DefaultDatabase string `mapstructure:"default_database"` // 默认数据库名
}

// LogConfig 日志配置
type LogConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
	Output string `mapstructure:"output"`
	File   string `mapstructure:"file"`
}

// Manager 配置管理器
type Manager struct {
	viper      *viper.Viper
	configPath string
	logger     *logrus.Logger
	config     *Config
	callbacks  []func(*Config)
}

// NewManager 创建配置管理器
func NewManager(configPath string, logger *logrus.Logger) *Manager {
	v := viper.New()

	// 设置配置文件路径
	if configPath != "" {
		v.SetConfigFile(configPath)
	} else {
		// 默认配置文件搜索路径
		v.SetConfigName("burin")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		v.AddConfigPath("./config")
		v.AddConfigPath("$HOME/.burin")
		v.AddConfigPath("/etc/burin")
	}

	// 环境变量前缀
	v.SetEnvPrefix("BURIN")
	v.AutomaticEnv()

	return &Manager{
		viper:      v,
		configPath: configPath,
		logger:     logger,
		callbacks:  make([]func(*Config), 0),
	}
}

// LoadConfig 加载配置
func (m *Manager) LoadConfig() (*Config, error) {
	// 设置默认值
	m.setDefaults()

	// 读取配置文件
	if err := m.viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			m.logger.Warn("Config file not found, using defaults")
		} else {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
	} else {
		m.logger.Infof("Using config file: %s", m.viper.ConfigFileUsed())
	}

	// 解析配置
	config := &Config{}
	if err := m.viper.Unmarshal(config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// 验证配置
	if err := m.validateConfig(config); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	m.config = config
	m.logger.Info("Configuration loaded successfully")

	return config, nil
}

// setDefaults 设置默认配置值
func (m *Manager) setDefaults() {
	// 应用默认配置
	m.viper.SetDefault("app.name", "burin")
	m.viper.SetDefault("app.version", "1.0.0")
	m.viper.SetDefault("app.environment", "development")
	m.viper.SetDefault("app.data_dir", "./data")
	m.viper.SetDefault("app.default_database", "default") // 默认数据库名

	// 日志默认配置
	m.viper.SetDefault("log.level", "info")
	m.viper.SetDefault("log.format", "text")
	m.viper.SetDefault("log.output", "stdout")

	// 缓存默认配置
	m.viper.SetDefault("store.data_dir", "./data/cache")
	m.viper.SetDefault("store.max_cache_size", 1073741824) // 1GB
	m.viper.SetDefault("store.sync_writes", false)
	m.viper.SetDefault("store.shard_count", 16)
	m.viper.SetDefault("store.shard_size", 67108864) // 64MB

	// 共识默认配置
	m.viper.SetDefault("consensus.id", "")
	m.viper.SetDefault("consensus.address", "127.0.0.1:8300")
	m.viper.SetDefault("consensus.data_dir", "./data/consensus")
	m.viper.SetDefault("consensus.log_level", "INFO")

	// 事务默认配置
	m.viper.SetDefault("transaction.storage_path", "./data/transaction")
	m.viper.SetDefault("transaction.timeout", "30s")
	m.viper.SetDefault("transaction.max_concurrent", 1000)

	// Burin服务器默认配置
	m.viper.SetDefault("burin.address", "127.0.0.1:8099")
	m.viper.SetDefault("burin.read_timeout", "30s")
	m.viper.SetDefault("burin.write_timeout", "30s")
	m.viper.SetDefault("burin.max_conns", 10000)
}

// validateConfig 验证配置
func (m *Manager) validateConfig(config *Config) error {
	// 验证应用配置
	if config.App.Name == "" {
		return fmt.Errorf("app.name cannot be empty")
	}

	// 验证数据目录
	if config.App.DataDir == "" {
		return fmt.Errorf("app.data_dir cannot be empty")
	}

	// 创建数据目录
	if err := os.MkdirAll(config.App.DataDir, 0755); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	// 验证日志级别
	if _, err := logrus.ParseLevel(config.Log.Level); err != nil {
		return fmt.Errorf("invalid log level: %s", config.Log.Level)
	}

	return nil
}

// SaveConfig 保存配置到文件
func (m *Manager) SaveConfig() error {
	if m.configPath == "" {
		return fmt.Errorf("config path not specified")
	}

	// 确保目录存在
	dir := filepath.Dir(m.configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	if err := m.viper.WriteConfigAs(m.configPath); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	m.logger.Infof("Configuration saved to: %s", m.configPath)
	return nil
}

// WatchConfig 监控配置文件变化
func (m *Manager) WatchConfig() error {
	m.viper.WatchConfig()
	m.viper.OnConfigChange(func(e fsnotify.Event) {
		m.logger.Infof("Config file changed: %s", e.Name)

		// 重新加载配置
		config := &Config{}
		if err := m.viper.Unmarshal(config); err != nil {
			m.logger.Errorf("Failed to reload config: %v", err)
			return
		}

		// 验证配置
		if err := m.validateConfig(config); err != nil {
			m.logger.Errorf("Invalid config after reload: %v", err)
			return
		}

		m.config = config
		m.logger.Info("Configuration reloaded successfully")

		// 通知回调函数
		for _, callback := range m.callbacks {
			go callback(config)
		}
	})

	return nil
}

// OnConfigChange 注册配置变化回调
func (m *Manager) OnConfigChange(callback func(*Config)) {
	m.callbacks = append(m.callbacks, callback)
}

// GetConfig 获取当前配置
func (m *Manager) GetConfig() *Config {
	return m.config
}

// UpdateConfig 更新配置值
func (m *Manager) UpdateConfig(key string, value interface{}) error {
	m.viper.Set(key, value)

	// 重新解析配置
	config := &Config{}
	if err := m.viper.Unmarshal(config); err != nil {
		return fmt.Errorf("failed to unmarshal updated config: %w", err)
	}

	// 验证配置
	if err := m.validateConfig(config); err != nil {
		return fmt.Errorf("invalid updated config: %w", err)
	}

	m.config = config
	m.logger.Infof("Configuration updated: %s = %v", key, value)

	// 通知回调函数
	for _, callback := range m.callbacks {
		go callback(config)
	}

	return nil
}

// GetString 获取字符串配置值
func (m *Manager) GetString(key string) string {
	return m.viper.GetString(key)
}

// GetInt 获取整数配置值
func (m *Manager) GetInt(key string) int {
	return m.viper.GetInt(key)
}

// GetBool 获取布尔配置值
func (m *Manager) GetBool(key string) bool {
	return m.viper.GetBool(key)
}

// GetDuration 获取时间长度配置值
func (m *Manager) GetDuration(key string) time.Duration {
	return m.viper.GetDuration(key)
}

// GenerateDefaultConfig 生成默认配置文件
func GenerateDefaultConfig(path string) error {
	v := viper.New()

	// 设置默认配置
	defaultConfig := map[string]interface{}{
		"app": map[string]interface{}{
			"name":        "burin",
			"version":     "1.0.0",
			"environment": "development",
			"data_dir":    "./data",
		},
		"log": map[string]interface{}{
			"level":  "info",
			"format": "text",
			"output": "stdout",
		},
		"cache": map[string]interface{}{
			"data_dir":       "./data/cache",
			"max_cache_size": 1073741824,
			"sync_writes":    false,
			"shard_count":    16,
			"shard_size":     67108864,
		},
		"consensus": map[string]interface{}{
			"id":        "",
			"address":   "127.0.0.1:8300",
			"data_dir":  "./data/consensus",
			"log_level": "INFO",
		},
		"transaction": map[string]interface{}{
			"storage_path":   "./data/transaction",
			"timeout":        "30s",
			"max_concurrent": 1000,
		},
		"burin": map[string]interface{}{
			"address":       "127.0.0.1:8099",
			"read_timeout":  "30s",
			"write_timeout": "30s",
			"max_conns":     10000,
		},
	}

	// 设置所有默认值
	for key, value := range defaultConfig {
		v.Set(key, value)
	}

	// 确保目录存在
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// 写入配置文件
	if err := v.WriteConfigAs(path); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}
