package main

import (
	"fmt"
	"time"

	"github.com/qorm/burin/client"
	"github.com/qorm/burin/client/interfaces"
)

// RunTransactionDemo 运行事务演示
func main() {
	fmt.Println("===========================================")
	fmt.Println("Burin ACID 事务测试示例")
	fmt.Println("===========================================")
	fmt.Println()

	// 创建客户端配置
	config := client.NewDefaultConfig()
	config.Connection.Endpoint = "127.0.0.1:8099"
	config.Auth.Username = "burin"
	config.Auth.Password = "burin@secret"

	// 创建客户端
	burinClient, err := client.NewClient(config)
	if err != nil {
		panic(fmt.Sprintf("创建客户端失败: %v", err))
	}

	// 连接到服务器
	if err := burinClient.Connect(); err != nil {
		panic(fmt.Sprintf("连接失败: %v", err))
	}
	defer burinClient.Disconnect()

	fmt.Println("✓ 已连接到 Burin 服务器")
	fmt.Println()

	// 演示1: 成功提交的事务
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("示例1: 成功提交的事务")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	// 开始事务
	tx, err := burinClient.BeginTransaction(
		interfaces.WithIsolationLevel(interfaces.RepeatableRead),
		interfaces.WithTxTimeout(30*time.Second),
	)
	if err != nil {
		fmt.Printf("✗ 开始事务失败: %v\n", err)
	} else {
		fmt.Printf("✓ 事务已开始: %s\n", tx.ID())

		// 在事务中执行操作
		fmt.Println("\n→ 执行事务操作:")

		// 写入账户余额
		if err := tx.Set("account:alice", []byte("1000")); err != nil {
			fmt.Printf("  ✗ 设置 account:alice 失败: %v\n", err)
		} else {
			fmt.Println("  ✓ 设置 account:alice = 1000")
		}

		if err := tx.Set("account:bob", []byte("500")); err != nil {
			fmt.Printf("  ✗ 设置 account:bob 失败: %v\n", err)
		} else {
			fmt.Println("  ✓ 设置 account:bob = 500")
		}

		// 读取数据验证
		val, err := tx.Get("account:alice")
		if err != nil {
			fmt.Printf("  ✗ 读取 account:alice 失败: %v\n", err)
		} else {
			fmt.Printf("  ✓ 读取 account:alice = %s\n", string(val))
		}

		// 提交事务
		fmt.Println("\n→ 提交事务...")
		if err := tx.Commit(); err != nil {
			fmt.Printf("✗ 提交失败: %v\n", err)
		} else {
			fmt.Printf("✓ 事务 %s 已成功提交\n", tx.ID())
		}
	}

	// 演示2: 回滚的事务
	fmt.Println("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("示例2: 回滚的事务")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	tx2, err := burinClient.BeginTransaction(
		interfaces.WithIsolationLevel(interfaces.RepeatableRead),
	)
	if err != nil {
		fmt.Printf("✗ 开始事务失败: %v\n", err)
	} else {
		fmt.Printf("✓ 事务已开始: %s\n", tx2.ID())

		fmt.Println("\n→ 执行事务操作:")

		// 修改数据
		if err := tx2.Set("account:alice", []byte("2000")); err != nil {
			fmt.Printf("  ✗ 设置 account:alice 失败: %v\n", err)
		} else {
			fmt.Println("  ✓ 设置 account:alice = 2000 (将被回滚)")
		}

		if err := tx2.Delete("account:bob"); err != nil {
			fmt.Printf("  ✗ 删除 account:bob 失败: %v\n", err)
		} else {
			fmt.Println("  ✓ 删除 account:bob (将被回滚)")
		}

		// 回滚事务
		fmt.Println("\n→ 回滚事务...")
		if err := tx2.Rollback(); err != nil {
			fmt.Printf("✗ 回滚失败: %v\n", err)
		} else {
			fmt.Printf("✓ 事务 %s 已回滚\n", tx2.ID())
		}
	}

	// 演示3: 验证数据一致性
	fmt.Println("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("示例3: 验证数据一致性")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	fmt.Println("\n→ 读取最终数据:")

	// 验证第一个事务的数据已提交
	resp, err := burinClient.Get("account:alice")
	if err != nil {
		fmt.Printf("  ✗ 读取 account:alice 失败: %v\n", err)
	} else {
		fmt.Printf("  ✓ account:alice = %s (应该是1000)\n", string(resp.Value))
	}

	resp, err = burinClient.Get("account:bob")
	if err != nil {
		fmt.Printf("  ✗ 读取 account:bob 失败: %v\n", err)
	} else {
		fmt.Printf("  ✓ account:bob = %s (应该是500)\n", string(resp.Value))
	}

	// 演示4: 转账事务（完整的ACID场景）
	fmt.Println("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("示例4: 转账事务 (Alice -> Bob: 200)")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	tx3, err := burinClient.BeginTransaction(
		interfaces.WithIsolationLevel(interfaces.Serializable),
	)
	if err != nil {
		fmt.Printf("✗ 开始转账事务失败: %v\n", err)
	} else {
		fmt.Printf("✓ 转账事务已开始: %s\n", tx3.ID())

		fmt.Println("\n→ 执行转账操作:")

		// 读取当前余额
		aliceVal, err1 := tx3.Get("account:alice")
		bobVal, err2 := tx3.Get("account:bob")

		if err1 != nil || err2 != nil {
			fmt.Println("  ✗ 读取账户余额失败，回滚事务")
			tx3.Rollback()
		} else {
			fmt.Printf("  ✓ Alice 当前余额: %s\n", string(aliceVal))
			fmt.Printf("  ✓ Bob 当前余额: %s\n", string(bobVal))

			// 执行转账（简化版，实际应该解析数字并计算）
			fmt.Println("\n  → 转账 200 从 Alice 到 Bob")

			// 扣除Alice的余额
			if err := tx3.Set("account:alice", []byte("800")); err != nil {
				fmt.Printf("  ✗ 更新 Alice 余额失败: %v\n", err)
				tx3.Rollback()
			} else {
				fmt.Println("  ✓ Alice 新余额: 800")

				// 增加Bob的余额
				if err := tx3.Set("account:bob", []byte("700")); err != nil {
					fmt.Printf("  ✗ 更新 Bob 余额失败: %v\n", err)
					tx3.Rollback()
				} else {
					fmt.Println("  ✓ Bob 新余额: 700")

					// 提交事务
					fmt.Println("\n→ 提交转账事务...")
					if err := tx3.Commit(); err != nil {
						fmt.Printf("✗ 转账失败: %v\n", err)
					} else {
						fmt.Printf("✓ 转账事务 %s 已成功完成\n", tx3.ID())
					}
				}
			}
		}
	}

	// 最终验证
	fmt.Println("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("最终验证")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	resp, _ = burinClient.Get("account:alice")
	fmt.Printf("✓ 最终 Alice 余额: %s (预期: 800)\n", string(resp.Value))

	resp, _ = burinClient.Get("account:bob")
	fmt.Printf("✓ 最终 Bob 余额: %s (预期: 700)\n", string(resp.Value))

	fmt.Println("\n===========================================")
	fmt.Println("事务测试完成")
	fmt.Println("===========================================")
}
