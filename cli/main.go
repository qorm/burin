package main

import (
	"fmt"
	"os"
	"time"

	"burin/client"

	"github.com/spf13/cobra"
)

var (
	burinClient *client.BurinClient
	address     string
	database    string
	timeout     time.Duration
	username    string
	password    string

	rootCmd = &cobra.Command{
		Use:   "burin-cli",
		Short: "Burin 命令行工具",
		Long: "Burin 是一个分布式缓存和存储系统的命令行客户端工具\n" +
			"不带任何命令参数时，将自动进入交互式模式",
		RunE: func(cmd *cobra.Command, args []string) error {
			// 如果没有指定子命令，进入交互式模式
			return interactiveCmd.RunE(cmd, args)
		},
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Name() == "version" || cmd.Name() == "help" || cmd.Name() == "interactive" || cmd.Name() == "burin-cli" {
				return nil
			}
			config := client.NewDefaultConfig()
			config.Connection.Endpoint = address
			config.Connection.DialTimeout = 5 * time.Second
			config.Connection.ReadTimeout = timeout
			config.Connection.WriteTimeout = timeout
			config.Connection.RetryCount = 3
			config.Connection.RetryDelay = 100 * time.Millisecond
			config.Cache.DefaultDatabase = database
			config.Auth.Username = username
			config.Auth.Password = password

			var err error
			burinClient, err = client.NewClient(config)
			if err != nil {
				return fmt.Errorf("创建客户端失败: %v", err)
			}

			if err := burinClient.Connect(); err != nil {
				return fmt.Errorf("连接服务器失败: %v", err)
			}

			return nil
		},
		PersistentPostRunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Name() == "version" || cmd.Name() == "help" || cmd.Name() == "interactive" || cmd.Name() == "burin-cli" {
				return nil
			}

			if burinClient != nil {
				return burinClient.Disconnect()
			}
			return nil
		},
	}
)

func init() {
	rootCmd.PersistentFlags().StringVarP(&address, "addr", "a", "127.0.0.1:8099", "服务器地址")
	rootCmd.PersistentFlags().StringVarP(&database, "database", "d", "default", "默认数据库")
	rootCmd.PersistentFlags().DurationVarP(&timeout, "timeout", "t", 10*time.Second, "操作超时时间")
	rootCmd.PersistentFlags().StringVarP(&username, "username", "u", "burin", "用户名")
	rootCmd.PersistentFlags().StringVarP(&password, "password", "p", "burin@secret", "密码")

	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(pingCmd)
	rootCmd.AddCommand(getCmd)
	rootCmd.AddCommand(setCmd)
	rootCmd.AddCommand(delCmd)
	rootCmd.AddCommand(existsCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(countCmd)
	rootCmd.AddCommand(geoCmd)
	rootCmd.AddCommand(clusterCmd)
	rootCmd.AddCommand(healthCmd)
	rootCmd.AddCommand(userCmd)
	rootCmd.AddCommand(interactiveCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		os.Exit(1)
	}
}
