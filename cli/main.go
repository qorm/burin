package main

import (
	"fmt"
	"os"
	"time"

	"github.com/qorm/burin/cli/i18n"
	"github.com/qorm/burin/client"

	"github.com/spf13/cobra"
)

var (
	burinClient *client.BurinClient
	address     string
	database    string
	timeout     time.Duration
	username    string
	password    string
	language    string

	rootCmd = &cobra.Command{
		Use: "burin-cli",
		RunE: func(cmd *cobra.Command, args []string) error {
			// 如果没有指定子命令,进入交互式模式
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
				return fmt.Errorf(i18n.T(i18n.ErrCreateClient, err))
			}

			if err := burinClient.Connect(); err != nil {
				return fmt.Errorf(i18n.T(i18n.ErrConnect, err))
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
	// 第一步: 注册flags
	rootCmd.PersistentFlags().StringVarP(&address, "addr", "a", "127.0.0.1:8099", "Server address")
	rootCmd.PersistentFlags().StringVarP(&database, "database", "d", "default", "Default database")
	rootCmd.PersistentFlags().DurationVarP(&timeout, "timeout", "t", 10*time.Second, "Operation timeout")
	rootCmd.PersistentFlags().StringVarP(&username, "username", "u", "burin", "Username")
	rootCmd.PersistentFlags().StringVarP(&password, "password", "p", "burin@secret", "Password")
	rootCmd.PersistentFlags().StringVarP(&language, "lang", "l", "", "Language (zh/en, default: auto-detect from system)")

	// 第二步: 提前解析language参数以初始化i18n
	// 从命令行参数中提取language参数
	langFromArgs := ""
	for i, arg := range os.Args {
		if arg == "--lang" || arg == "-l" {
			if i+1 < len(os.Args) {
				langFromArgs = os.Args[i+1]
				break
			}
		}
	}

	// 初始化i18n
	i18n.Init(langFromArgs)

	// 第三步: 设置命令描述 (在i18n初始化之后)
	rootCmd.Short = i18n.T(i18n.CliShort)
	rootCmd.Long = i18n.T(i18n.CliLong)
	versionCmd.Short = i18n.T(i18n.CmdVersion)
	pingCmd.Short = i18n.T(i18n.CmdPing)
	getCmd.Short = i18n.T(i18n.CmdGet)
	setCmd.Short = i18n.T(i18n.CmdSet)
	delCmd.Short = i18n.T(i18n.CmdDel)
	existsCmd.Short = i18n.T(i18n.CmdExists)
	listCmd.Short = i18n.T(i18n.CmdList)
	countCmd.Short = i18n.T(i18n.CmdCount)
	geoCmd.Short = i18n.T(i18n.CmdGeo)
	geoAddCmd.Short = i18n.T(i18n.CmdGeoAdd)
	geoDistCmd.Short = i18n.T(i18n.CmdGeoDist)
	geoRadiusCmd.Short = i18n.T(i18n.CmdGeoRadius)
	geoPosCmd.Short = i18n.T(i18n.CmdGeoPos)
	clusterCmd.Short = i18n.T(i18n.CmdCluster)
	healthCmd.Short = i18n.T(i18n.CmdHealth)
	interactiveCmd.Short = i18n.T(i18n.CmdInteractive)

	// 第四步: 更新flag描述
	rootCmd.PersistentFlags().Lookup("addr").Usage = i18n.T(i18n.FlagAddress)
	rootCmd.PersistentFlags().Lookup("database").Usage = i18n.T(i18n.FlagDatabase)
	rootCmd.PersistentFlags().Lookup("timeout").Usage = i18n.T(i18n.FlagTimeout)
	rootCmd.PersistentFlags().Lookup("username").Usage = i18n.T(i18n.FlagUsername)
	rootCmd.PersistentFlags().Lookup("password").Usage = i18n.T(i18n.FlagPassword)

	setCmd.Flags().Lookup("ttl").Usage = i18n.T(i18n.FlagTTL)
	listCmd.Flags().Lookup("prefix").Usage = i18n.T(i18n.FlagPrefix)
	listCmd.Flags().Lookup("limit").Usage = i18n.T(i18n.FlagLimit)
	listCmd.Flags().Lookup("offset").Usage = i18n.T(i18n.FlagOffset)
	countCmd.Flags().Lookup("prefix").Usage = i18n.T(i18n.FlagPrefix)

	// 第五步: 添加子命令
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
		fmt.Fprintf(os.Stderr, "%s: %v\n", i18n.T(i18n.MsgError), err)
		os.Exit(1)
	}
}
