package client

import (
	"testing"
	"time"
)

func TestClientPool(t *testing.T) {
	// 创建测试配置
	config := NewDefaultConfig()
	config.Connection.Endpoint = "localhost:8520"

	// 创建连接池
	pool, err := NewClientPool(
		config,
		5, // maxSize
		2, // minSize
		1*time.Minute,
		5*time.Minute,
	)
	if err != nil {
		t.Skipf("Failed to create pool (server may not be running): %v", err)
	}
	defer pool.Close()

	// 测试1: 验证初始统计信息
	stats := pool.Stats()
	t.Logf("Initial pool stats: %+v", stats)

	if stats["current_size"].(int) < 2 {
		t.Errorf("Expected at least 2 initial connections, got %d", stats["current_size"])
	}

	// 测试2: 获取客户端
	pc1, err := pool.Get()
	if err != nil {
		t.Fatalf("Failed to get client: %v", err)
	}

	client := pc1.Get()
	if client == nil {
		t.Fatal("Got nil client from pooled client")
	}

	// 测试3: 归还客户端
	err = pc1.Close()
	if err != nil {
		t.Errorf("Failed to return client: %v", err)
	}

	// 测试4: 获取多个客户端
	clients := make([]*PooledClient, 4)
	for i := 0; i < 4; i++ {
		clients[i], err = pool.Get()
		if err != nil {
			t.Fatalf("Failed to get client %d: %v", i, err)
		}
	}

	// 测试5: 验证连接数增长
	stats = pool.Stats()
	if stats["current_size"].(int) < 4 {
		t.Errorf("Expected at least 4 connections, got %d", stats["current_size"])
	}
	t.Logf("Pool stats after getting 4 clients: %+v", stats)

	// 归还所有客户端
	for _, pc := range clients {
		pc.Close()
	}

	// 测试6: 验证最终统计信息
	stats = pool.Stats()
	t.Logf("Final pool stats: %+v", stats)
}

func TestClientPoolMaxSize(t *testing.T) {
	config := NewDefaultConfig()
	config.Connection.Endpoint = "localhost:8520"

	pool, err := NewClientPool(
		config,
		3, // maxSize
		0, // minSize
		1*time.Minute,
		5*time.Minute,
	)
	if err != nil {
		t.Skipf("Failed to create pool (server may not be running): %v", err)
	}
	defer pool.Close()

	// 获取最大数量的客户端
	clients := make([]*PooledClient, 3)
	for i := 0; i < 3; i++ {
		clients[i], err = pool.Get()
		if err != nil {
			t.Fatalf("Failed to get client %d: %v", i, err)
		}
	}

	// 尝试获取超过最大数量的客户端，应该失败
	_, err = pool.Get()
	if err != ErrPoolEmpty {
		t.Errorf("Expected ErrPoolEmpty, got %v", err)
	}

	// 归还一个客户端
	clients[0].Close()

	// 现在应该可以获取新客户端
	pc, err := pool.Get()
	if err != nil {
		t.Errorf("Failed to get client after return: %v", err)
	}
	pc.Close()

	// 清理
	for i := 1; i < 3; i++ {
		clients[i].Close()
	}
}

func TestClientPoolConcurrent(t *testing.T) {
	config := NewDefaultConfig()
	config.Connection.Endpoint = "localhost:8520"

	pool, err := NewClientPool(
		config,
		10, // maxSize
		2,  // minSize
		1*time.Minute,
		5*time.Minute,
	)
	if err != nil {
		t.Skipf("Failed to create pool (server may not be running): %v", err)
	}
	defer pool.Close()

	// 并发获取和归还客户端
	const numGoroutines = 20
	const numIterations = 10

	done := make(chan bool, numGoroutines)
	errors := make(chan error, numGoroutines*numIterations)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			for j := 0; j < numIterations; j++ {
				pc, err := pool.Get()
				if err != nil && err != ErrPoolEmpty {
					errors <- err
					continue
				}
				if pc != nil {
					// 模拟使用客户端
					time.Sleep(10 * time.Millisecond)
					pc.Close()
				}
			}
			done <- true
		}(i)
	}

	// 等待所有 goroutine 完成
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	close(errors)
	errorCount := 0
	for range errors {
		errorCount++
	}

	if errorCount > 0 {
		t.Logf("Got %d errors during concurrent access (some pool empty errors expected)", errorCount)
	}

	// 验证最终状态
	stats := pool.Stats()
	t.Logf("Final pool stats: %+v", stats)

	if stats["current_size"].(int) > 10 {
		t.Errorf("Pool size exceeded max: %d", stats["current_size"])
	}
}
