package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"burin/client"
)

func main() {
	// 创建客户端配置
	config := client.NewDefaultConfig()
	config.Connection.Endpoint = "localhost:8520"
	config.Auth.Username = "admin"
	config.Auth.Password = "admin123"

	// 创建客户端连接池
	// maxSize: 最大连接数
	// minSize: 最小连接数（预创建）
	// idleTimeout: 空闲超时
	// maxLifetime: 最大生命周期
	pool, err := client.NewClientPool(
		config,
		10,             // 最大10个连接
		2,              // 预创建2个连接
		5*time.Minute,  // 空闲5分钟后回收
		30*time.Minute, // 最大存活30分钟
	)
	if err != nil {
		log.Fatalf("Failed to create client pool: %v", err)
	}
	defer pool.Close()

	fmt.Println("Client pool created successfully!")
	fmt.Printf("Initial pool stats: %+v\n\n", pool.Stats())

	// 示例1: 单次使用
	fmt.Println("=== Example 1: Single Use ===")
	if err := singleUse(pool); err != nil {
		log.Printf("Single use failed: %v", err)
	}
	fmt.Printf("Pool stats after single use: %+v\n\n", pool.Stats())

	// 示例2: 并发使用
	fmt.Println("=== Example 2: Concurrent Use ===")
	if err := concurrentUse(pool); err != nil {
		log.Printf("Concurrent use failed: %v", err)
	}
	fmt.Printf("Pool stats after concurrent use: %+v\n\n", pool.Stats())

	// 示例3: 长时间持有连接
	fmt.Println("=== Example 3: Long-running Connection ===")
	if err := longRunningUse(pool); err != nil {
		log.Printf("Long-running use failed: %v", err)
	}
	fmt.Printf("Final pool stats: %+v\n", pool.Stats())
}

// singleUse 演示单次使用连接池
func singleUse(pool *client.ClientPool) error {
	// 从池中获取客户端
	pc, err := pool.Get()
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}
	defer pc.Close() // 使用完后归还到池中

	// 获取底层客户端
	c := pc.Get()

	// 执行操作
	ctx := context.Background()
	err = c.Set("test_key", []byte("test_value"))
	if err != nil {
		return fmt.Errorf("failed to set key: %w", err)
	}

	resp, err := c.Get("test_key")
	if err != nil {
		return fmt.Errorf("failed to get key: %w", err)
	}

	fmt.Printf("Got value: %s\n", string(resp.Value))

	// 清理
	c.Delete("test_key")
	_ = ctx

	return nil
}

// concurrentUse 演示并发使用连接池
func concurrentUse(pool *client.ClientPool) error {
	const numWorkers = 5
	const numOpsPerWorker = 10

	var wg sync.WaitGroup
	errors := make(chan error, numWorkers)

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for j := 0; j < numOpsPerWorker; j++ {
				// 从池中获取客户端
				pc, err := pool.Get()
				if err != nil {
					errors <- fmt.Errorf("worker %d: failed to get client: %w", workerID, err)
					continue
				}

				// 使用客户端
				c := pc.Get()
				key := fmt.Sprintf("worker_%d_key_%d", workerID, j)
				value := fmt.Sprintf("value_%d", j)

				if err := c.Set(key, []byte(value)); err != nil {
					errors <- fmt.Errorf("worker %d: failed to set: %w", workerID, err)
					pc.Close()
					continue
				}

				// 模拟一些工作
				time.Sleep(50 * time.Millisecond)

				// 归还客户端到池中
				pc.Close()
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// 检查是否有错误
	for err := range errors {
		fmt.Printf("Error: %v\n", err)
	}

	fmt.Printf("Completed %d concurrent workers\n", numWorkers)
	return nil
}

// longRunningUse 演示长时间持有连接
func longRunningUse(pool *client.ClientPool) error {
	pc, err := pool.Get()
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}
	defer pc.Close()

	c := pc.Get()

	// 执行一系列操作
	for i := 0; i < 5; i++ {
		key := fmt.Sprintf("long_running_key_%d", i)
		value := fmt.Sprintf("value_%d", i)

		if err := c.Set(key, []byte(value)); err != nil {
			return fmt.Errorf("failed to set key %s: %w", key, err)
		}

		fmt.Printf("Set %s = %s\n", key, value)
		time.Sleep(200 * time.Millisecond)
	}

	fmt.Println("Long-running operations completed")
	return nil
}
