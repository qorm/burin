package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/qorm/burin/business"
	"github.com/qorm/burin/config"

	"github.com/sirupsen/logrus"
)

var (
	configFile = flag.String("config", "", "Path to configuration file")
	genConfig  = flag.Bool("generate-config", false, "Generate default configuration file")
	version    = flag.Bool("version", false, "Show version information")
	help       = flag.Bool("help", false, "Show help information")
)

const (
	appName    = "Burin"
	appVersion = "1.0.0"
	appDesc    = "High-performance distributed cache system"
)

func main() {
	flag.Parse()

	// 显示帮助信息
	if *help {
		showHelp()
		return
	}

	// 显示版本信息
	if *version {
		showVersion()
		return
	}

	// 生成默认配置文件
	if *genConfig {
		generateDefaultConfig()
		return
	}

	// 初始化日志
	logger := logrus.New()
	logger.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
		DisableColors: false,
	})

	// 启动应用
	if err := runApp(logger); err != nil {
		logger.Fatalf("Application failed: %v", err)
	}
}

// runApp 运行主应用程序
func runApp(logger *logrus.Logger) error {
	logger.Info("Starting Burin distributed cache system...")

	// 创建配置管理器
	configManager := config.NewManager(*configFile, logger)

	// 加载配置
	cfg, err := configManager.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// 调试：打印配置信息
	logger.Infof("Debug: App NodeID: '%s'", cfg.App.NodeID)
	logger.Infof("Debug: Consensus NodeID: '%s'", cfg.Consensus.NodeID)
	logger.Infof("Debug: Consensus BindAddr: '%s'", cfg.Consensus.BindAddr)
	logger.Infof("Debug: Cache DataDir: '%s'", cfg.Cache.DataDir)

	// 配置日志
	if err := configureLogger(logger, &cfg.Log); err != nil {
		return fmt.Errorf("failed to configure logger: %w", err)
	}

	logger.Infof("Loaded configuration for environment: %s", cfg.App.Environment)

	// 启动配置文件监控（如果不是生产环境）
	if cfg.App.Environment != "production" {
		if err := configManager.WatchConfig(); err != nil {
			logger.Warnf("Failed to watch config file: %v", err)
		} else {
			logger.Info("Configuration file watching enabled")
		}
	}

	// 确定节点ID，如果App层没有设置，则使用Consensus层的ID
	nodeID := cfg.App.NodeID
	if nodeID == "" {
		nodeID = cfg.Consensus.NodeID
		logger.Infof("Using consensus node ID as app node ID: %s", nodeID)
	}

	// 创建业务引擎配置
	engineConfig := &business.EngineConfig{
		NodeID:            nodeID,
		CacheConfig:       &cfg.Cache,
		ConsensusConfig:   &cfg.Consensus,
		TransactionConfig: &cfg.Transaction,
		BurinConfig:       &cfg.Burin,
		AuthConfig:        nil, // 使用默认配置
	}

	// 创建业务引擎
	engine, err := business.NewEngine(engineConfig, logger)
	if err != nil {
		return fmt.Errorf("failed to create business engine: %w", err)
	}

	// 设置配置变化回调
	configManager.OnConfigChange(func(newConfig *config.Config) {
		logger.Info("Configuration changed, considering restart...")
		// 在生产环境中，可能需要重启服务
		// 这里只是记录日志
	})

	// 启动引擎
	if err := engine.Start(); err != nil {
		return fmt.Errorf("failed to start engine: %w", err)
	}

	logger.Infof("%s v%s started successfully", appName, appVersion)
	logger.Infof("Node ID: %s", engine.GetNodeID())
	logger.Infof("Burin server listening on: %s", cfg.Burin.Address)

	// 设置优雅关闭
	return gracefulShutdown(engine, logger)
}

// configureLogger 配置日志器
func configureLogger(logger *logrus.Logger, cfg *config.LogConfig) error {
	// 设置日志级别
	level, err := logrus.ParseLevel(cfg.Level)
	if err != nil {
		return fmt.Errorf("invalid log level: %s", cfg.Level)
	}
	logger.SetLevel(level)

	// 设置日志格式
	switch cfg.Format {
	case "json":
		logger.SetFormatter(&logrus.JSONFormatter{
			TimestampFormat: time.RFC3339,
		})
	case "text":
		logger.SetFormatter(&logrus.TextFormatter{
			FullTimestamp:   true,
			TimestampFormat: time.RFC3339,
		})
	default:
		return fmt.Errorf("unsupported log format: %s", cfg.Format)
	}

	// 设置日志输出
	switch cfg.Output {
	case "stdout":
		logger.SetOutput(os.Stdout)
	case "file":
		if cfg.File == "" {
			return fmt.Errorf("log file path not specified")
		}

		// 确保日志目录存在
		dir := filepath.Dir(cfg.File)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create log directory: %w", err)
		}

		file, err := os.OpenFile(cfg.File, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			return fmt.Errorf("failed to open log file: %w", err)
		}
		logger.SetOutput(file)
	default:
		return fmt.Errorf("unsupported log output: %s", cfg.Output)
	}

	return nil
}

// gracefulShutdown 优雅关闭
func gracefulShutdown(engine *business.Engine, logger *logrus.Logger) error {
	// 监听系统信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// 等待信号
	sig := <-sigChan
	logger.Infof("Received signal: %v, initiating graceful shutdown...", sig)

	// 创建关闭上下文，设置超时
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 关闭引擎
	done := make(chan error, 1)
	go func() {
		done <- engine.Stop()
	}()

	// 等待关闭完成或超时
	select {
	case err := <-done:
		if err != nil {
			logger.Errorf("Error during shutdown: %v", err)
			return err
		}
		logger.Info("Graceful shutdown completed")
		return nil
	case <-ctx.Done():
		logger.Warn("Shutdown timeout exceeded, forcing exit")
		return fmt.Errorf("shutdown timeout")
	}
}

// showHelp 显示帮助信息
func showHelp() {
	fmt.Printf("%s v%s - %s\n\n", appName, appVersion, appDesc)
	fmt.Println("Usage:")
	fmt.Printf("  %s [options]\n\n", os.Args[0])
	fmt.Println("Options:")
	flag.PrintDefaults()
	fmt.Println("\nExamples:")
	fmt.Printf("  %s                                    # Use default config\n", os.Args[0])
	fmt.Printf("  %s -config=/etc/burin/config.yaml    # Use specific config file\n", os.Args[0])
	fmt.Printf("  %s -generate-config                  # Generate default config\n", os.Args[0])
	fmt.Printf("  %s -version                          # Show version\n", os.Args[0])
}

// showVersion 显示版本信息
func showVersion() {
	fmt.Printf("%s version %s\n", appName, appVersion)
	fmt.Printf("Description: %s\n", appDesc)
}

// generateDefaultConfig 生成默认配置文件
func generateDefaultConfig() {
	configPath := "burin.yaml"
	if *configFile != "" {
		configPath = *configFile
	}

	if err := config.GenerateDefaultConfig(configPath); err != nil {
		fmt.Printf("Error generating config file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Default configuration file generated: %s\n", configPath)
	fmt.Println("You can now edit this file and run the application with:")
	fmt.Printf("  %s -config=%s\n", os.Args[0], configPath)
}
