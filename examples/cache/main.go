package main

import (
	"burin/client"
	"fmt"
	"log"
	"time"
)

// RunCacheDemo 运行缓存示例
func main() {
	config := client.NewDefaultConfig()

	// 设置日志级别为 Debug
	config.Logging.Level = "debug"

	// 配置多个端点，包括在线和离线的，测试故障转移
	config.Connection.Endpoint = "127.0.0.1:8099"

	// 配置认证信息（设置用户名密码后会自动登录）
	config.Auth.Username = "burin"
	config.Auth.Password = "burin@secret"

	// 创建客户端
	burinClient, err := client.NewClient(config)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	// 连接到服务器（会自动登录）
	if err := burinClient.Connect(); err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer burinClient.Disconnect()

	// 设置缓存
	if err := burinClient.Set("user:100111", []byte(`{"name":"Alice","age":25}`), client.WithTTL(10*time.Second)); err != nil {
		log.Printf("Set failed: %v", err)
	} else {
		fmt.Println("✓ Set cache successfully")
	}

	// 获取缓存
	resp, err := burinClient.Get("user:100111")
	if err != nil {
		log.Printf("Get failed: %v", err)
	} else if resp.Found {
		fmt.Printf("✓ Get cache: %s\n", string(resp.Value))
	}

	// 检查存在
	exists, err := burinClient.Exists("user:100111")
	if err != nil {
		log.Printf("Exists check failed: %v", err)
	} else {
		fmt.Printf("✓ Cache exists: %v\n", exists)
	}

	// 批量操作
	keyValues := map[string][]byte{
		"user:1002": []byte(`{"name":"Bob","age":30}`),
		"user:1003": []byte(`{"name":"Charlie","age":35}`),
	}
	err = burinClient.MSet(keyValues)
	if err != nil {
		log.Printf("MSet failed: %v", err)
	} else {
		fmt.Println("✓ MSet cache successfully")
	}

	// 批量获取
	results, err := burinClient.MGet([]string{"user:100111", "user:1002", "user:1003"})
	if err != nil {
		log.Printf("MGet failed: %v", err)
	} else {
		fmt.Printf("✓ MGet returned %d results\n", len(results))
		for key, resp := range results {
			if resp.Found {
				fmt.Printf("  %s: %s\n", key, string(resp.Value))
			}
		}
	}
	lists, total, err := burinClient.ListKeys(client.WithPrefix("user:"), client.WithOffset(0), client.WithLimit(2))
	if err != nil {
		log.Printf("ListKeys failed: %v", err)
	} else {
		fmt.Printf("✓ ListKeys returned total %d keys\n", total)
		for _, key := range lists {
			fmt.Printf("  %s\n", key)
		}
	}

	count, err := burinClient.CountKeys(client.WithPrefix("user:"))
	if err != nil {
		log.Printf("CountKeys failed: %v", err)
	} else {
		fmt.Printf("✓ CountKeys returned count %d\n", count)
	}

	// 指定数据库操作
	// err = burinClient.set("analytics", "event:100111", []byte(`{"event":"click"}`))
	// if err != nil {
	// 	log.Printf("SetWithDatabase failed: %v", err)
	// } else {
	// 	fmt.Println("✓ Set cache in analytics database")
	// }

	// 删除缓存
	err = burinClient.Delete("user:100111")
	if err != nil {
		log.Printf("Delete failed: %v", err)
	} else {
		fmt.Println("✓ Delete cache successfully")
	}

	// // 获取监控指标
	// if concreteClient, ok := burinClient.(*cache.Client); ok {
	// 	if metrics := concreteClient.GetMetrics(); metrics != nil {
	// 		fmt.Println("\n📊 Metrics:")
	// 		for k, v := range metrics {
	// 			fmt.Printf("  %s: %v\n", k, v)
	// 		}
	// 	}
	// }
}
