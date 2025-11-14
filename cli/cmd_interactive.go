package main

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"burin/client"
	"burin/client/interfaces"

	"github.com/chzyer/readline"
	"github.com/spf13/cobra"
)

var interactiveCmd = &cobra.Command{
	Use:   "interactive",
	Short: "进入交互式模式",
	Long:  "连接到 Burin 服务器并进入交互式命令行模式",
	RunE:  runInteractive,
}

func init() {
	// 交互模式不需要 PersistentPreRun/PostRun，自己管理连接
}

// executeWithTiming 执行函数并显示执行时间
func executeWithTiming(fn func()) {
	start := time.Now()
	fn()
	elapsed := time.Since(start)
	fmt.Printf("(耗时: %v)\n", elapsed)
}

func runInteractive(cmd *cobra.Command, args []string) error {

	// 创建客户端配置
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

	// 创建客户端
	c, err := client.NewClient(config)
	if err != nil {
		return fmt.Errorf("创建客户端失败: %v", err)
	}

	// 连接服务器
	if err := c.Connect(); err != nil {
		return fmt.Errorf("连接服务器失败: %v", err)
	}
	defer c.Disconnect()

	fmt.Printf("已连接到 Burin 服务器: %s\n", address)
	fmt.Printf("当前用户: %s\n", username)
	fmt.Printf("当前数据库: %s\n", database)
	fmt.Println("输入 'help' 查看可用命令，输入 'quit' 或 'exit' 退出")
	fmt.Println()

	// 创建命令补全器
	completer := createCompleter()

	// 创建 readline 实例
	currentDB := database
	currentAddr := address

	rl, err := readline.NewEx(&readline.Config{
		Prompt:          fmt.Sprintf("%s@%s[%s]> ", username, currentAddr, currentDB),
		HistoryFile:     "./burin-cli-history.txt",
		AutoComplete:    completer,
		InterruptPrompt: "^C",
		EOFPrompt:       "exit",
	})
	if err != nil {
		return fmt.Errorf("创建 readline 失败: %v", err)
	}
	defer rl.Close()

	// 交互式循环
	for {
		// 更新提示符
		rl.SetPrompt(fmt.Sprintf("%s@%s[%s]> ", username, currentAddr, currentDB))

		// 读取用户输入
		input, err := rl.Readline()
		if err != nil {
			if err == readline.ErrInterrupt {
				if len(input) == 0 {
					fmt.Println("\n再见!")
					return nil
				}
				continue
			} else if err == io.EOF {
				fmt.Println("\n再见!")
				return nil
			}
			fmt.Printf("读取输入失败: %v\n", err)
			continue
		}

		// 去除首尾空白
		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}

		// 分割命令和参数
		parts := strings.Fields(input)
		if len(parts) == 0 {
			continue
		}

		command := strings.ToLower(parts[0])
		subcommand := ""
		if len(parts) > 1 {
			subcommand = strings.ToLower(parts[1])
		}

		// 处理命令
		switch command {
		case "quit", "exit":
			fmt.Println("再见!")
			return nil

		case "help":
			if len(parts) > 1 {
				// 显示特定模块的帮助
				module := strings.ToLower(parts[1])
				switch module {
				case "cache":
					printCacheHelp()
				case "db":
					printDBHelp()
				case "geo":
					printGeoHelp()
				case "user":
					printUserHelp()
				case "tx", "transaction":
					printTxHelp()
				case "node":
					printNodeHelp()
				default:
					fmt.Printf("未知模块: %s\n", module)
					fmt.Println("可用模块: cache, db, geo, user, tx, node")
				}
			} else {
				// 显示完整帮助
				printHelp()
			}

		case "ping":
			handlePing(c)

		// NODE 节点切换
		case "node", "n":
			if len(parts) < 2 {
				fmt.Printf("当前节点: %s\n", currentAddr)
				continue
			}

			switch subcommand {
			case "help":
				printNodeHelp()
				continue

			case "list":
				executeWithTiming(func() {
					handleClusterInfo(c)
				})

			case "info":
				executeWithTiming(func() {
					handleHealth(c)
				})

			case "switch":
				if len(parts) < 3 {
					fmt.Println("用法: node switch <address>")
					continue
				}
				newAddr := parts[2]
				// 断开旧连接
				c.Disconnect()

				// 创建新配置
				newConfig := client.NewDefaultConfig()
				newConfig.Connection.Endpoint = newAddr
				newConfig.Connection.DialTimeout = 5 * time.Second
				newConfig.Connection.ReadTimeout = timeout
				newConfig.Connection.WriteTimeout = timeout
				newConfig.Connection.RetryCount = 3
				newConfig.Connection.RetryDelay = 100 * time.Millisecond
				newConfig.Cache.DefaultDatabase = currentDB
				newConfig.Auth.Username = username
				newConfig.Auth.Password = password

				// 创建新客户端
				newClient, err := client.NewClient(newConfig)
				if err != nil {
					fmt.Printf("创建客户端失败: %v\n", err)
					fmt.Println("保持当前连接")
					// 重新连接旧客户端
					if err := c.Connect(); err != nil {
						fmt.Printf("重新连接失败: %v\n", err)
						return fmt.Errorf("连接中断")
					}
					continue
				}

				// 连接新服务器
				if err := newClient.Connect(); err != nil {
					fmt.Printf("连接新节点失败: %v\n", err)
					fmt.Println("保持当前连接")
					// 重新连接旧客户端
					if err := c.Connect(); err != nil {
						fmt.Printf("重新连接失败: %v\n", err)
						return fmt.Errorf("连接中断")
					}
					continue
				}

				// 切换成功
				c = newClient
				currentAddr = newAddr
				fmt.Printf("已切换到节点: %s\n", currentAddr)

			default:
				fmt.Printf("未知子命令: node %s\n", subcommand)
				fmt.Println("可用子命令: list, switch, info, help")
			}

		// Cache 缓存操作
		case "cache", "c":
			if len(parts) < 2 {
				fmt.Println("用法: cache <subcommand> [args...]")
				fmt.Println("子命令: get, set, del, exists, list, count, help")
				continue
			}

			switch subcommand {
			case "help":
				printCacheHelp()
				continue

			case "get":
				if len(parts) < 3 {
					fmt.Println("用法: cache get <key>")
					continue
				}
				executeWithTiming(func() {
					handleGet(c, parts[2], currentDB)
				})

			case "set":
				if len(parts) < 4 {
					fmt.Println("用法: cache set <key> <value> [ttl_seconds]")
					continue
				}
				ttl := 0
				if len(parts) >= 5 {
					fmt.Sscanf(parts[4], "%d", &ttl)
				}
				executeWithTiming(func() {
					handleSet(c, parts[2], parts[3], currentDB, ttl)
				})

			case "del", "delete":
				if len(parts) < 3 {
					fmt.Println("用法: cache del <key>")
					continue
				}
				executeWithTiming(func() {
					handleDel(c, parts[2], currentDB)
				})

			case "exists":
				if len(parts) < 3 {
					fmt.Println("用法: cache exists <key>")
					continue
				}
				executeWithTiming(func() {
					handleExists(c, parts[2], currentDB)
				})

			case "list":
				prefix := ""
				offset := 0
				limit := 100 // 默认限制100条

				// 解析参数: cache list [prefix] [offset] [limit]
				if len(parts) >= 3 {
					prefix = parts[2]
				}
				if len(parts) >= 4 {
					if o, err := strconv.Atoi(parts[3]); err == nil {
						offset = o
					}
				}
				if len(parts) >= 5 {
					if l, err := strconv.Atoi(parts[4]); err == nil {
						limit = l
					}
				}
				executeWithTiming(func() {
					handleList(c, prefix, offset, limit, currentDB)
				})

			case "count":
				prefix := ""
				if len(parts) >= 3 {
					prefix = parts[2]
				}
				executeWithTiming(func() {
					handleCount(c, prefix, currentDB)
				})

			default:
				fmt.Printf("未知子命令: cache %s\n", subcommand)
			}

		// DB 数据库操作
		case "db":
			if len(parts) < 2 {
				// 显示当前数据库
				fmt.Printf("当前数据库: %s\n", currentDB)
				continue
			}

			switch subcommand {
			case "help":
				printDBHelp()
				continue

			case "use":
				if len(parts) < 3 {
					fmt.Println("用法: db use <database>")
					continue
				}
				newDB := parts[2]
				if strings.HasPrefix(newDB, "__burin_") {
					fmt.Printf("错误: database '__burin_*' is reserved for system use\n")
					continue
				}
				// 调用服务端验证数据库是否存在
				if handleDBUse(c, newDB) {
					currentDB = newDB
					fmt.Printf("已切换到数据库: %s\n", currentDB)
				}

			case "create":
				if len(parts) < 3 {
					fmt.Println("用法: db create <database>")
					continue
				}
				executeWithTiming(func() {
					handleDBCreate(c, parts[2])
				})

			case "list":
				executeWithTiming(func() {
					handleDBList(c)
				})

			case "delete", "del":
				if len(parts) < 3 {
					fmt.Println("用法: db delete <database>")
					continue
				}
				executeWithTiming(func() {
					handleDBDelete(c, parts[2])
				})

			case "exists":
				if len(parts) < 3 {
					fmt.Println("用法: db exists <database>")
					continue
				}
				executeWithTiming(func() {
					handleDBExists(c, parts[2])
				})

			case "info":
				if len(parts) < 3 {
					fmt.Println("用法: db info <database>")
					continue
				}
				executeWithTiming(func() {
					handleDBInfo(c, parts[2])
				})

			default:
				fmt.Printf("未知子命令: db %s\n", subcommand)
			}

		// GEO 地理位置操作
		case "geo", "g":
			if len(parts) < 2 {
				fmt.Println("用法: geo <subcommand> [args...]")
				fmt.Println("子命令: add, dist, radius, hash, pos, get, del, help")
				continue
			}

			switch subcommand {
			case "help":
				printGeoHelp()
				continue

			case "add":
				if len(parts) < 6 {
					fmt.Println("用法: geo add <key> <lon> <lat> <member> [metadata_key:value ...]")
					continue
				}
				executeWithTiming(func() {
					handleGeoAdd(c, parts[1:], currentDB)
				})

			case "dist":
				if len(parts) < 5 {
					fmt.Println("用法: geo dist <key> <member1> <member2> [unit]")
					continue
				}
				unit := "m"
				if len(parts) >= 6 {
					unit = parts[5]
				}
				executeWithTiming(func() {
					handleGeoDist(c, parts[2], parts[3], parts[4], unit, currentDB)
				})

			case "radius":
				if len(parts) < 7 {
					fmt.Println("用法: geo radius <key> <lon> <lat> <radius> <unit>")
					continue
				}
				executeWithTiming(func() {
					handleGeoRadius(c, parts[1:], currentDB)
				})

			case "hash":
				if len(parts) < 4 {
					fmt.Println("用法: geo hash <key> <member> [member ...]")
					continue
				}
				executeWithTiming(func() {
					handleGeoHash(c, parts[2], parts[3:], currentDB)
				})

			case "pos":
				if len(parts) < 4 {
					fmt.Println("用法: geo pos <key> <member> [member ...]")
					continue
				}
				executeWithTiming(func() {
					handleGeoPos(c, parts[2], parts[3:], currentDB)
				})

			case "get":
				if len(parts) < 4 {
					fmt.Println("用法: geo get <key> <member>")
					continue
				}
				executeWithTiming(func() {
					handleGeoGet(c, parts[2], parts[3], currentDB)
				})

			case "del", "delete", "remove":
				if len(parts) < 4 {
					fmt.Println("用法: geo del <key> <member> [member ...]")
					continue
				}
				executeWithTiming(func() {
					handleGeoDel(c, parts[2], parts[3:], currentDB)
				})

			default:
				fmt.Printf("未知子命令: geo %s\n", subcommand)
			}

		// USER 用户管理操作
		case "user", "u":
			if len(parts) < 2 {
				fmt.Println("用法: user <subcommand> [args...]")
				fmt.Println("子命令: create, delete, list, info, grant, revoke, passwd, help")
				continue
			}

			switch subcommand {
			case "help":
				printUserHelp()
				continue

			case "create":
				if len(parts) < 5 {
					fmt.Println("用法: user create <username> <password> <role>")
					fmt.Println("角色: superadmin, admin, readwrite, readonly")
					continue
				}
				executeWithTiming(func() {
					handleUserCreate(c, parts[2], parts[3], parts[4])
				})

			case "delete", "del":
				if len(parts) < 3 {
					fmt.Println("用法: user delete <username>")
					continue
				}
				executeWithTiming(func() {
					handleUserDelete(c, parts[2])
				})

			case "list":
				executeWithTiming(func() {
					handleUserList(c)
				})

			case "info":
				if len(parts) < 3 {
					fmt.Println("用法: user info <username>")
					continue
				}
				executeWithTiming(func() {
					handleUserInfo(c, parts[2])
				})

			case "grant":
				if len(parts) < 5 {
					fmt.Println("用法: user grant <username> <database> <permissions>")
					fmt.Println("权限 (逗号分隔): read, write, delete, admin")
					continue
				}
				executeWithTiming(func() {
					handleUserGrant(c, parts[2], parts[3], parts[4])
				})

			case "revoke":
				if len(parts) < 4 {
					fmt.Println("用法: user revoke <username> <database>")
					continue
				}
				executeWithTiming(func() {
					handleUserRevoke(c, parts[2], parts[3])
				})

			case "passwd", "password":
				if len(parts) < 4 {
					fmt.Println("用法: user passwd <username> <new_password>")
					continue
				}
				executeWithTiming(func() {
					handleUserPasswd(c, parts[2], parts[3])
				})

			default:
				fmt.Printf("未知子命令: user %s\n", subcommand)
			}

		// TRANSACTION 事务操作
		case "tx", "transaction":
			if len(parts) < 2 {
				fmt.Println("用法: tx <subcommand> [args...]")
				fmt.Println("子命令: begin, commit, rollback, get, set, del, status, help")
				continue
			}

			switch subcommand {
			case "help":
				printTxHelp()
				continue

			case "begin":
				executeWithTiming(func() {
					handleTxBegin(c, currentDB)
				})

			case "commit":
				executeWithTiming(func() {
					handleTxCommit()
				})

			case "rollback":
				executeWithTiming(func() {
					handleTxRollback()
				})

			case "get":
				if len(parts) < 3 {
					fmt.Println("用法: tx get <key>")
					continue
				}
				executeWithTiming(func() {
					handleTxGet(parts[2])
				})

			case "set":
				if len(parts) < 4 {
					fmt.Println("用法: tx set <key> <value>")
					continue
				}
				executeWithTiming(func() {
					handleTxSet(parts[2], parts[3])
				})

			case "del", "delete":
				if len(parts) < 3 {
					fmt.Println("用法: tx del <key>")
					continue
				}
				executeWithTiming(func() {
					handleTxDel(parts[2])
				})

			case "status":
				executeWithTiming(func() {
					handleTxStatus()
				})

			default:
				fmt.Printf("未知子命令: tx %s\n", subcommand)
			}

		default:
			fmt.Printf("未知命令: %s (输入 'help' 查看可用命令)\n", command)
		}
	}
}

func printHelp() {
	fmt.Println("================================================================================")
	fmt.Println("                          Burin 交互式命令帮助")
	fmt.Println("================================================================================")
	fmt.Println()
	fmt.Println("【缓存操作 - cache(c)】")
	fmt.Println("  c get <key>                                - 获取指定键的值")
	fmt.Println("  c set <key> <value> [ttl]                  - 设置键值对，可选过期时间(秒)")
	fmt.Println("  c del <key>                                - 删除指定键")
	fmt.Println("  c exists <key>                             - 检查键是否存在")
	fmt.Println("  c list [prefix] [offset] [limit]           - 列出键名列表，支持前缀过滤和分页")
	fmt.Println("  c count [prefix]                           - 统计键数量，可选前缀过滤")
	fmt.Println("  c help                                     - 查看缓存操作详细帮助")
	fmt.Println()
	fmt.Println("【数据库操作 - db】")
	fmt.Println("  db                                             - 显示当前使用的数据库")
	fmt.Println("  db use <database>                              - 切换到指定数据库")
	fmt.Println("  db create <database>                           - 创建新数据库")
	fmt.Println("  db list                                        - 列出所有数据库")
	fmt.Println("  db delete <database>                           - 删除指定数据库")
	fmt.Println("  db exists <database>                           - 检查数据库是否存在")
	fmt.Println("  db info <database>                             - 查看数据库详细信息")
	fmt.Println("  db help                                        - 查看数据库操作详细帮助")
	fmt.Println()
	fmt.Println("【地理位置操作 - geo(g)】")
	fmt.Println("  g add <key> <lon> <lat> <member> [meta...]   - 添加地理位置点到集合")
	fmt.Println("  g dist <key> <member1> <member2> [unit]      - 计算两个成员之间的距离")
	fmt.Println("  g radius <key> <lon> <lat> <radius> <unit>   - 按中心坐标和半径查询成员")
	fmt.Println("  g hash <key> <member> [member...]            - 获取成员的GeoHash值")
	fmt.Println("  g pos <key> <member> [member...]             - 获取成员的经纬度坐标")
	fmt.Println("  g get <key> <member>                         - 获取成员的完整信息(含元数据)")
	fmt.Println("  g del <key> <member> [member...]             - 删除指定成员")
	fmt.Println("  g help                                       - 查看地理位置操作详细帮助")
	fmt.Println()
	fmt.Println("【用户管理 - user(u)】")
	fmt.Println("  u create <username> <password> <role>       - 创建新用户")
	fmt.Println("  u delete <username>                         - 删除指定用户")
	fmt.Println("  u list                                      - 列出所有用户")
	fmt.Println("  u info <username>                           - 查看用户详细信息")
	fmt.Println("  u grant <username> <database> <perms>       - 授予用户数据库权限")
	fmt.Println("  u revoke <username> <database>              - 撤销用户在指定数据库的权限")
	fmt.Println("  u passwd <username> <new_password>          - 修改用户密码")
	fmt.Println("  u help                                      - 查看用户管理详细帮助")
	fmt.Println()
	fmt.Println("【事务操作 - transaction(tx)】")
	fmt.Println("  tx begin                                       - 开始一个新事务")
	fmt.Println("  tx get <key>                                   - 在事务中读取数据")
	fmt.Println("  tx set <key> <value>                           - 在事务中写入数据")
	fmt.Println("  tx del <key>                                   - 在事务中删除数据")
	fmt.Println("  tx commit                                      - 提交当前事务")
	fmt.Println("  tx rollback                                    - 回滚当前事务")
	fmt.Println("  tx status                                      - 查看当前事务状态")
	fmt.Println("  tx help                                        - 查看事务操作详细帮助")
	fmt.Println()
	fmt.Println("【节点操作 - node(n)】")
	fmt.Println("  n                                           - 显示当前连接的节点")
	fmt.Println("  n list                                      - 查看集群所有节点信息")
	fmt.Println("  n info                                      - 检查当前节点健康状态")
	fmt.Println("  n switch <address>                          - 切换到指定节点(格式: host:port)")
	fmt.Println("  n help                                      - 查看节点操作详细帮助")
	fmt.Println()
	fmt.Println("【其他】")
	fmt.Println("  ping                                           - 测试服务器连接和响应时间")
	fmt.Println("  help [module]                                  - 显示帮助信息(可选模块: cache/db/geo/user/tx/node)")
	fmt.Println("  quit / exit                                    - 退出交互式命令行")
	fmt.Println()
	fmt.Println("提示: 使用 'help <module>' 或 '<module> help' 查看特定模块的详细帮助")
	fmt.Println("================================================================================")
	fmt.Println()
}

func handlePing(c *client.BurinClient) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	_, err := c.GetClusterInfo(ctx)
	duration := time.Since(start)

	if err != nil {
		fmt.Printf("PONG (失败: %v)\n", err)
	} else {
		fmt.Printf("PONG (%v)\n", duration)
	}
}

func handleGet(c *client.BurinClient, key, database string) {
	resp, err := c.Get(key, client.WithDatabase(database))
	if err != nil {
		fmt.Printf("错误: %v\n", err)
		return
	}
	fmt.Printf("%s\n", string(resp.Value))
}

func handleSet(c *client.BurinClient, key, value, database string, ttl int) {
	opts := []interface{}{}

	// 添加数据库选项
	if database != "" {
		opts = append(opts, client.WithDatabase(database))
	}

	// 添加 TTL 选项
	if ttl > 0 {
		opts = append(opts, client.WithTTL(time.Duration(ttl)*time.Second))
	}

	// 转换为 CacheOption
	var cacheOpts []func(interface{})
	for _, opt := range opts {
		if co, ok := opt.(func(interface{})); ok {
			cacheOpts = append(cacheOpts, co)
		}
	}

	err := c.Set(key, []byte(value), client.WithDatabase(database))
	if ttl > 0 {
		err = c.Set(key, []byte(value), client.WithDatabase(database), client.WithTTL(time.Duration(ttl)*time.Second))
	}

	if err != nil {
		fmt.Printf("错误: %v\n", err)
		return
	}
	fmt.Println("OK")
}

func handleDel(c *client.BurinClient, key, database string) {
	err := c.Delete(key, client.WithDatabase(database))
	if err != nil {
		fmt.Printf("错误: %v\n", err)
		return
	}
	fmt.Println("OK")
}

func handleExists(c *client.BurinClient, key, database string) {
	exists, err := c.Exists(key, client.WithDatabase(database))
	if err != nil {
		fmt.Printf("错误: %v\n", err)
		return
	}
	if exists {
		fmt.Println("存在")
	} else {
		fmt.Println("不存在")
	}
}

func handleList(c *client.BurinClient, prefix string, offset, limit int, database string) {
	opts := []interfaces.CacheOption{client.WithDatabase(database)}
	if prefix != "" {
		opts = append(opts, client.WithPrefix(prefix))
	}
	opts = append(opts, client.WithOffset(offset), client.WithLimit(limit))

	keys, total, err := c.ListKeys(opts...)
	if err != nil {
		fmt.Printf("错误: %v\n", err)
		return
	}

	fmt.Printf("显示 %d/%d 个键 (offset=%d, limit=%d):\n", len(keys), total, offset, limit)
	for _, key := range keys {
		fmt.Printf("  %s\n", key)
	}
	if int64(offset+len(keys)) < total {
		fmt.Printf("提示: 还有 %d 个键未显示,使用 cache list [prefix] [offset] [limit] 查看更多\n", total-int64(offset+len(keys)))
	}
}

func handleCount(c *client.BurinClient, prefix, database string) {
	opts := []interfaces.CacheOption{client.WithDatabase(database)}
	if prefix != "" {
		opts = append(opts, client.WithPrefix(prefix))
	}

	count, err := c.CountKeys(opts...)
	if err != nil {
		fmt.Printf("错误: %v\n", err)
		return
	}
	fmt.Printf("键数量: %d\n", count)
}

func handleClusterInfo(c *client.BurinClient) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	info, err := c.GetClusterInfo(ctx)
	if err != nil {
		fmt.Printf("错误: %v\n", err)
		return
	}

	// 解析集群信息
	fmt.Println("================================================================================")
	fmt.Println("                            集群节点信息")
	fmt.Println("================================================================================")
	fmt.Printf("%-20s %-24s %-18s %-6s %-6s\n", "节点ID", "Raft地址", "客户端地址", "状态", "角色")
	fmt.Println("--------------------------------------------------------------------------------")

	if nodes, ok := info["nodes"].([]interface{}); ok {
		for _, nodeData := range nodes {
			if node, ok := nodeData.(map[string]interface{}); ok {
				id := getStringValue(node, "id")
				address := getStringValue(node, "address")
				clientAddr := getStringValue(node, "client_addr")
				role := getStringValue(node, "role")

				// 确定状态
				status := ""
				if isCurrent, ok := node["is_current"].(bool); ok && isCurrent {
					status = "在线"
					role += "⭐"
				} else if isActive, ok := node["is_active"].(bool); ok {
					if isActive {
						status = "在线"
					} else {
						status = "离线"
					}
				}

				fmt.Printf("%-20s %-24s %-18s %-6s %-6s\n",
					id, address, clientAddr, status, role)
			}
		}
	}

	fmt.Println("================================================================================")
	fmt.Println()
}

func handleHealth(c *client.BurinClient) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	info, err := c.GetHealth(ctx)
	if err != nil {
		fmt.Printf("错误: %v\n", err)
		return
	}

	fmt.Println("健康状态:")

	// 只显示健康相关的关键信息
	if status, ok := info["status"].(string); ok {
		fmt.Printf("  状态: %s\n", status)
	}

	if nodeID, ok := info["node_id"].(string); ok {
		fmt.Printf("  节点ID: %s\n", nodeID)
	}

	if uptime, ok := info["uptime"].(float64); ok {
		fmt.Printf("  运行时间: %.2f 秒\n", uptime)
	}

	if components, ok := info["components"].(map[string]interface{}); ok {
		fmt.Println("  组件状态:")
		for name, status := range components {
			statusStr := "❌"
			if val, ok := status.(bool); ok && val {
				statusStr = "✓"
			}
			fmt.Printf("    %s %s\n", statusStr, name)
		}
	}

	if stats, ok := info["stats"].(map[string]interface{}); ok {
		if ops, ok := stats["total_operations"].(float64); ok {
			fmt.Printf("  总操作数: %.0f\n", ops)
		}
	}
}

// 辅助函数：安全获取字符串值
func getStringValue(m map[string]interface{}, key string) string {
	if val, ok := m[key].(string); ok {
		return val
	}
	return ""
}

// createCompleter 创建命令补全器
func createCompleter() *readline.PrefixCompleter {
	return readline.NewPrefixCompleter(
		// 基础命令
		readline.PcItem("help"),
		readline.PcItem("ping"),
		readline.PcItem("quit"),
		readline.PcItem("exit"),

		// node 命令
		readline.PcItem("node",
			readline.PcItem("list"),
			readline.PcItem("info"),
			readline.PcItem("switch"),
			readline.PcItem("current"),
			readline.PcItem("help"),
		),
		// node 命令
		readline.PcItem("n",
			readline.PcItem("list"),
			readline.PcItem("info"),
			readline.PcItem("switch"),
			readline.PcItem("current"),
			readline.PcItem("help"),
		),

		// cache 命令
		readline.PcItem("cache",
			readline.PcItem("get"),
			readline.PcItem("set"),
			readline.PcItem("del"),
			readline.PcItem("delete"),
			readline.PcItem("exists"),
			readline.PcItem("list"),
			readline.PcItem("count"),
			readline.PcItem("help"),
		),

		// cache 命令
		readline.PcItem("c",
			readline.PcItem("get"),
			readline.PcItem("set"),
			readline.PcItem("del"),
			readline.PcItem("delete"),
			readline.PcItem("exists"),
			readline.PcItem("list"),
			readline.PcItem("count"),
			readline.PcItem("help"),
		),

		// db 命令
		readline.PcItem("db",
			readline.PcItem("use"),
			readline.PcItem("create"),
			readline.PcItem("list"),
			readline.PcItem("delete"),
			readline.PcItem("del"),
			readline.PcItem("exists"),
			readline.PcItem("info"),
			readline.PcItem("help"),
		),

		// geo 命令
		readline.PcItem("geo",
			readline.PcItem("add"),
			readline.PcItem("dist"),
			readline.PcItem("radius"),
			readline.PcItem("hash"),
			readline.PcItem("pos"),
			readline.PcItem("get"),
			readline.PcItem("del"),
			readline.PcItem("delete"),
			readline.PcItem("remove"),
			readline.PcItem("help"),
		),
		readline.PcItem("g",
			readline.PcItem("add"),
			readline.PcItem("dist"),
			readline.PcItem("radius"),
			readline.PcItem("hash"),
			readline.PcItem("pos"),
			readline.PcItem("get"),
			readline.PcItem("del"),
			readline.PcItem("delete"),
			readline.PcItem("remove"),
			readline.PcItem("help"),
		),

		// user 命令
		readline.PcItem("user",
			readline.PcItem("create"),
			readline.PcItem("delete"),
			readline.PcItem("del"),
			readline.PcItem("list"),
			readline.PcItem("info"),
			readline.PcItem("grant"),
			readline.PcItem("revoke"),
			readline.PcItem("passwd"),
			readline.PcItem("password"),
			readline.PcItem("help"),
		),
		readline.PcItem("u",
			readline.PcItem("create"),
			readline.PcItem("delete"),
			readline.PcItem("del"),
			readline.PcItem("list"),
			readline.PcItem("info"),
			readline.PcItem("grant"),
			readline.PcItem("revoke"),
			readline.PcItem("passwd"),
			readline.PcItem("password"),
			readline.PcItem("help"),
		),

		// tx 命令
		readline.PcItem("tx",
			readline.PcItem("begin"),
			readline.PcItem("get"),
			readline.PcItem("set"),
			readline.PcItem("del"),
			readline.PcItem("delete"),
			readline.PcItem("commit"),
			readline.PcItem("rollback"),
			readline.PcItem("status"),
			readline.PcItem("help"),
		),
		readline.PcItem("transaction",
			readline.PcItem("begin"),
			readline.PcItem("get"),
			readline.PcItem("set"),
			readline.PcItem("del"),
			readline.PcItem("delete"),
			readline.PcItem("commit"),
			readline.PcItem("rollback"),
			readline.PcItem("status"),
			readline.PcItem("help"),
		),
	)
}

// printCacheHelp 显示缓存操作帮助
func printCacheHelp() {
	fmt.Println("================================================================================")
	fmt.Println("                          缓存操作命令帮助 cache(c)")
	fmt.Println("================================================================================")
	fmt.Println()
	fmt.Println("  cache get <key>                                - 获取指定键的值")
	fmt.Println("    参数: <key> 缓存键名")
	fmt.Println("    示例: cache get mykey")
	fmt.Println()
	fmt.Println("  cache set <key> <value> [ttl]                  - 设置键值对，可选过期时间(秒)")
	fmt.Println("    参数: <key> 缓存键名, <value> 缓存值, [ttl] 过期时间(秒,0表示永不过期)")
	fmt.Println("    示例: cache set mykey \"hello world\" 60")
	fmt.Println()
	fmt.Println("  cache del <key>                                - 删除指定键")
	fmt.Println("    参数: <key> 缓存键名")
	fmt.Println("    示例: cache del mykey")
	fmt.Println()
	fmt.Println("  cache exists <key>                             - 检查键是否存在")
	fmt.Println("    参数: <key> 缓存键名")
	fmt.Println("    示例: cache exists mykey")
	fmt.Println()
	fmt.Println("  cache list [prefix] [offset] [limit]           - 列出键名列表，支持前缀过滤和分页")
	fmt.Println("    参数: [prefix] 键名前缀过滤, [offset] 跳过记录数(默认0), [limit] 返回记录数(默认100)")
	fmt.Println("    示例: cache list user: 0 50")
	fmt.Println()
	fmt.Println("  cache count [prefix]                           - 统计键数量，可选前缀过滤")
	fmt.Println("    参数: [prefix] 键名前缀过滤")
	fmt.Println("    示例: cache count user:")
	fmt.Println()
	fmt.Println("================================================================================")
	fmt.Println()
}

// printDBHelp 显示数据库操作帮助
func printDBHelp() {
	fmt.Println("================================================================================")
	fmt.Println("                          数据库操作命令帮助 db")
	fmt.Println("================================================================================")
	fmt.Println()
	fmt.Println("  db                                             - 显示当前使用的数据库")
	fmt.Println()
	fmt.Println("  db use <database>                              - 切换到指定数据库")
	fmt.Println("    参数: <database> 数据库名称(不能以__burin_开头)")
	fmt.Println("    示例: db use mydb")
	fmt.Println()
	fmt.Println("  db create <database>                           - 创建新数据库")
	fmt.Println("    参数: <database> 数据库名称")
	fmt.Println("    示例: db create mydb")
	fmt.Println()
	fmt.Println("  db list                                        - 列出所有数据库")
	fmt.Println()
	fmt.Println("  db delete <database>                           - 删除指定数据库")
	fmt.Println("    参数: <database> 数据库名称")
	fmt.Println("    示例: db delete mydb")
	fmt.Println()
	fmt.Println("  db exists <database>                           - 检查数据库是否存在")
	fmt.Println("    参数: <database> 数据库名称")
	fmt.Println("    示例: db exists mydb")
	fmt.Println()
	fmt.Println("  db info <database>                             - 查看数据库详细信息")
	fmt.Println("    参数: <database> 数据库名称")
	fmt.Println("    示例: db info mydb")
	fmt.Println()
	fmt.Println("================================================================================")
	fmt.Println()
}

// printGeoHelp 显示地理位置操作帮助
func printGeoHelp() {
	fmt.Println("================================================================================")
	fmt.Println("                        地理位置操作命令帮助 geo(g)")
	fmt.Println("================================================================================")
	fmt.Println()
	fmt.Println("  geo add <key> <lon> <lat> <member> [meta...]   - 添加地理位置点到集合，可选元数据(key:value)")
	fmt.Println("    参数: <key> GEO集合键名, <lon> 经度(-180~180), <lat> 纬度(-90~90), <member> 成员名称, [meta...] 元数据(格式:key:value)")
	fmt.Println("    示例: geo add places 116.404 39.915 beijing city:北京")
	fmt.Println()
	fmt.Println("  geo dist <key> <member1> <member2> [unit]      - 计算两个成员之间的距离，可选单位(m/km/mi/ft)")
	fmt.Println("    参数: <key> GEO集合键名, <member1> 第一个成员, <member2> 第二个成员, [unit] 距离单位(m/km/mi/ft,默认m)")
	fmt.Println("    示例: geo dist places beijing shanghai km")
	fmt.Println()
	fmt.Println("  geo radius <key> <lon> <lat> <radius> <unit>   - 按中心坐标和半径查询范围内的成员")
	fmt.Println("    参数: <key> GEO集合键名, <lon> 中心点经度, <lat> 中心点纬度, <radius> 查询半径, <unit> 距离单位(m/km/mi/ft)")
	fmt.Println("    示例: geo radius places 116.404 39.915 1000 km")
	fmt.Println()
	fmt.Println("  geo hash <key> <member> [member...]            - 获取成员的GeoHash值")
	fmt.Println("    参数: <key> GEO集合键名, <member> 一个或多个成员名")
	fmt.Println("    示例: geo hash places beijing shanghai")
	fmt.Println()
	fmt.Println("  geo pos <key> <member> [member...]             - 获取成员的经纬度坐标")
	fmt.Println("    参数: <key> GEO集合键名, <member> 一个或多个成员名")
	fmt.Println("    示例: geo pos places beijing shanghai")
	fmt.Println()
	fmt.Println("  geo get <key> <member>                         - 获取成员的完整信息(含元数据)")
	fmt.Println("    参数: <key> GEO集合键名, <member> 成员名称")
	fmt.Println("    示例: geo get places beijing")
	fmt.Println()
	fmt.Println("  geo del <key> <member> [member...]             - 删除指定成员")
	fmt.Println("    参数: <key> GEO集合键名, <member> 一个或多个成员名")
	fmt.Println("    示例: geo del places beijing shanghai")
	fmt.Println()
	fmt.Println("================================================================================")
	fmt.Println()
}

// printUserHelp 显示用户管理帮助
func printUserHelp() {
	fmt.Println("================================================================================")
	fmt.Println("                          用户管理命令帮助 user(u)")
	fmt.Println("================================================================================")
	fmt.Println()
	fmt.Println("  user create <username> <password> <role>       - 创建新用户(角色: superadmin/admin/readwrite/readonly)")
	fmt.Println("    参数: <username> 用户名, <password> 密码, <role> 角色(superadmin超级管理员/admin管理员/readwrite读写/readonly只读)")
	fmt.Println("    示例: user create john pass123 readwrite")
	fmt.Println()
	fmt.Println("  user delete <username>                         - 删除指定用户")
	fmt.Println("    参数: <username> 用户名")
	fmt.Println("    示例: user delete john")
	fmt.Println()
	fmt.Println("  user list                                      - 列出所有用户")
	fmt.Println()
	fmt.Println("  user info <username>                           - 查看用户详细信息")
	fmt.Println("    参数: <username> 用户名")
	fmt.Println("    示例: user info john")
	fmt.Println()
	fmt.Println("  user grant <username> <database> <perms>       - 授予用户数据库权限(read,write,delete,admin)")
	fmt.Println("    参数: <username> 用户名, <database> 数据库名, <perms> 权限列表(逗号分隔:read,write,delete,admin)")
	fmt.Println("    示例: user grant john mydb read,write")
	fmt.Println()
	fmt.Println("  user revoke <username> <database>              - 撤销用户在指定数据库的权限")
	fmt.Println("    参数: <username> 用户名, <database> 数据库名")
	fmt.Println("    示例: user revoke john mydb")
	fmt.Println()
	fmt.Println("  user passwd <username> <new_password>          - 修改用户密码")
	fmt.Println("    参数: <username> 用户名, <new_password> 新密码")
	fmt.Println("    示例: user passwd john newpass456")
	fmt.Println()
	fmt.Println("================================================================================")
	fmt.Println()
}

// printTxHelp 显示事务操作帮助
func printTxHelp() {
	fmt.Println("================================================================================")
	fmt.Println("                          事务操作命令帮助 transaction(tx)")
	fmt.Println("================================================================================")
	fmt.Println()
	fmt.Println("  tx begin                                       - 开始一个新事务")
	fmt.Println("    示例: tx begin")
	fmt.Println()
	fmt.Println("  tx get <key>                                   - 在事务中读取数据")
	fmt.Println("    参数: <key> 缓存键名")
	fmt.Println("    示例: tx get mykey")
	fmt.Println()
	fmt.Println("  tx set <key> <value>                           - 在事务中写入数据")
	fmt.Println("    参数: <key> 缓存键名, <value> 缓存值")
	fmt.Println("    示例: tx set mykey \"hello\"")
	fmt.Println()
	fmt.Println("  tx del <key>                                   - 在事务中删除数据")
	fmt.Println("    参数: <key> 缓存键名")
	fmt.Println("    示例: tx del mykey")
	fmt.Println()
	fmt.Println("  tx commit                                      - 提交当前事务")
	fmt.Println("    示例: tx commit")
	fmt.Println()
	fmt.Println("  tx rollback                                    - 回滚当前事务")
	fmt.Println("    示例: tx rollback")
	fmt.Println()
	fmt.Println("  tx status                                      - 查看当前事务状态")
	fmt.Println("    示例: tx status")
	fmt.Println()
	fmt.Println("提示: 必须先使用 'tx begin' 开始事务，才能执行 get/set/del 操作")
	fmt.Println("================================================================================")
	fmt.Println()
}

// printNodeHelp 显示节点操作帮助
func printNodeHelp() {
	fmt.Println("================================================================================")
	fmt.Println("                          节点操作命令帮助 node(n)")
	fmt.Println("================================================================================")
	fmt.Println()
	fmt.Println("  node                                           - 显示当前连接的节点")
	fmt.Println()
	fmt.Println("  node list                                      - 查看集群所有节点信息")
	fmt.Println()
	fmt.Println("  node info                                      - 检查当前节点健康状态")
	fmt.Println()
	fmt.Println("  node switch <address>                          - 切换到指定节点(格式: host:port)")
	fmt.Println("    参数: <address> 节点地址(格式:host:port, 如localhost:9001)")
	fmt.Println("    示例: node switch localhost:9002")
	fmt.Println()
	fmt.Println("================================================================================")
	fmt.Println()
}

// 全局变量存储当前事务
var currentTx interfaces.Transaction

// handleTxBegin 开始一个新事务
func handleTxBegin(c *client.BurinClient, database string) {
	if currentTx != nil {
		fmt.Printf("错误: 已存在活跃事务 (ID: %s)\n", currentTx.ID())
		fmt.Println("请先提交或回滚当前事务")
		return
	}

	tx, err := c.BeginTransaction(interfaces.WithTxDatabase(database))
	if err != nil {
		fmt.Printf("错误: %v\n", err)
		return
	}

	currentTx = tx
	fmt.Printf("事务已开始 (ID: %s)\n", tx.ID())
}

// handleTxCommit 提交当前事务
func handleTxCommit() {
	if currentTx == nil {
		fmt.Println("错误: 没有活跃的事务")
		return
	}

	err := currentTx.Commit()
	if err != nil {
		fmt.Printf("错误: %v\n", err)
		return
	}

	fmt.Printf("事务已提交 (ID: %s)\n", currentTx.ID())
	currentTx = nil
}

// handleTxRollback 回滚当前事务
func handleTxRollback() {
	if currentTx == nil {
		fmt.Println("错误: 没有活跃的事务")
		return
	}

	err := currentTx.Rollback()
	if err != nil {
		fmt.Printf("错误: %v\n", err)
		return
	}

	fmt.Printf("事务已回滚 (ID: %s)\n", currentTx.ID())
	currentTx = nil
}

// handleTxGet 在事务中读取数据
func handleTxGet(key string) {
	if currentTx == nil {
		fmt.Println("错误: 没有活跃的事务，请先使用 'tx begin' 开始事务")
		return
	}

	value, err := currentTx.Get(key)
	if err != nil {
		fmt.Printf("错误: %v\n", err)
		return
	}

	fmt.Printf("%s\n", string(value))
}

// handleTxSet 在事务中写入数据
func handleTxSet(key, value string) {
	if currentTx == nil {
		fmt.Println("错误: 没有活跃的事务，请先使用 'tx begin' 开始事务")
		return
	}

	err := currentTx.Set(key, []byte(value))
	if err != nil {
		fmt.Printf("错误: %v\n", err)
		return
	}

	fmt.Println("OK")
}

// handleTxDel 在事务中删除数据
func handleTxDel(key string) {
	if currentTx == nil {
		fmt.Println("错误: 没有活跃的事务，请先使用 'tx begin' 开始事务")
		return
	}

	err := currentTx.Delete(key)
	if err != nil {
		fmt.Printf("错误: %v\n", err)
		return
	}

	fmt.Println("OK")
}

// handleTxStatus 查看当前事务状态
func handleTxStatus() {
	if currentTx == nil {
		fmt.Println("无活跃事务")
		return
	}

	fmt.Printf("事务ID: %s\n", currentTx.ID())
	fmt.Printf("状态: %v\n", currentTx.Status())
}
