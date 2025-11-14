package main

import (
	"fmt"
	"time"

	"github.com/qorm/burin/client"
	"github.com/qorm/burin/client/interfaces"
)

// getClientForOp 获取用于操作的客户端（如果连接池可用则从池中获取，否则使用全局客户端）
func getClientForOp() (*client.BurinClient, func(), error) {
	// 如果连接池可用，从池中获取客户端
	if clientPool != nil {
		pc, err := clientPool.Get()
		if err != nil {
			return nil, nil, fmt.Errorf("从连接池获取客户端失败: %w", err)
		}
		return pc.Get(), func() { pc.Close() }, nil
	}

	// 否则使用全局客户端（不并发安全，但用于非并发测试）
	return burinClient, func() {}, nil
}

// WithDatabase 设置数据库选项
func WithDatabase(db string) interfaces.CacheOption {
	return func(opts *interfaces.CacheOptions) {
		opts.Database = db
	}
}

// cacheSet 设置缓存（使用默认数据库）
func cacheSet(key string, value []byte, ttl time.Duration) error {
	return cacheSetDB(key, value, 0, ttl)
}

// cacheSetDB 设置指定数据库的缓存
func cacheSetDB(key string, value []byte, dbID int, ttl time.Duration) error {
	var opts []interfaces.CacheOption

	// 如果 dbID 不为 0，则指定数据库，否则使用默认数据库
	if dbID > 0 {
		opts = append(opts, WithDatabase(fmt.Sprintf("db%d", dbID)))
	}

	if ttl > 0 {
		opts = append(opts, interfaces.CacheOption(func(o *interfaces.CacheOptions) {
			o.TTL = &ttl
		}))
	}

	err := burinClient.Set(key, value, opts...)
	if err != nil {
		return fmt.Errorf("SET失败: %w", err)
	}

	return nil
} // cacheGetFromLeader 从 leader 节点读取数据（强一致性读，使用默认数据库）
func cacheGetFromLeader(key string) ([]byte, error) {
	return cacheGetFromLeaderDB(key, 0)
}

// cacheGetFromLeaderDB 从 leader 节点读取指定数据库的数据
func cacheGetFromLeaderDB(key string, dbID int) ([]byte, error) {
	var opts []interfaces.CacheOption

	// 如果 dbID 不为 0，则指定数据库，否则使用默认数据库
	if dbID > 0 {
		opts = append(opts, WithDatabase(fmt.Sprintf("db%d", dbID)))
	}

	resp, err := burinClient.Get(key, opts...)
	if err != nil {
		return nil, fmt.Errorf("GET失败: %w", err)
	}

	if !resp.Found {
		return nil, fmt.Errorf("键不存在")
	}

	return resp.Value, nil
}

// cacheGetFromLeaderWithRetry 从 leader 节点读取数据，失败时自动重试
func cacheGetFromLeaderWithRetry(key string, dbID int, maxRetry int) ([]byte, error) {
	var lastErr error
	for i := 0; i < maxRetry; i++ {
		value, err := cacheGetFromLeaderDB(key, dbID)
		if err == nil {
			return value, nil
		}
		lastErr = err
		time.Sleep(time.Duration(100*(i+1)) * time.Millisecond)
	}
	return nil, lastErr
}

// cacheGet 从任意节点读取数据（使用默认数据库）
func cacheGet(key string) ([]byte, error) {
	return cacheGetDB(key, 0)
}

// cacheGetDB 从任意节点读取指定数据库的数据
func cacheGetDB(key string, dbID int) ([]byte, error) {
	var opts []interfaces.CacheOption

	// 如果 dbID 不为 0，则指定数据库，否则使用默认数据库
	if dbID > 0 {
		opts = append(opts, WithDatabase(fmt.Sprintf("db%d", dbID)))
	}

	resp, err := burinClient.Get(key, opts...)
	if err != nil {
		return nil, fmt.Errorf("GET失败: %w", err)
	}

	if !resp.Found {
		return nil, fmt.Errorf("键不存在")
	}

	return resp.Value, nil
}

// cacheGetFromNode 从指定节点读取数据（创建临时客户端连接到该节点）
func cacheGetFromNode(key string, endpoint string) ([]byte, error) {
	return cacheGetFromNodeDB(key, endpoint, 0)
}

// cacheGetFromNodeDB 从指定节点的指定数据库读取数据
func cacheGetFromNodeDB(key string, endpoint string, dbID int) ([]byte, error) {
	// 创建临时客户端连接到指定节点
	config := client.NewDefaultConfig()
	config.Connection.Endpoint = endpoint
	config.Auth.Username = "burin"
	config.Auth.Password = "burin@secret"
	config.Logging.Level = "error" // 减少日志输出

	tempClient, err := client.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("创建临时客户端失败: %w", err)
	}

	if err := tempClient.Connect(); err != nil {
		return nil, fmt.Errorf("连接到节点 %s 失败: %w", endpoint, err)
	}
	defer tempClient.Disconnect()

	var opts []interfaces.CacheOption
	if dbID > 0 {
		opts = append(opts, WithDatabase(fmt.Sprintf("db%d", dbID)))
	}

	resp, err := tempClient.Get(key, opts...)
	if err != nil {
		return nil, fmt.Errorf("从节点 %s GET失败: %w", endpoint, err)
	}

	if !resp.Found {
		return nil, fmt.Errorf("键不存在")
	}

	return resp.Value, nil
}

// cacheGetFromNodeWithRetry 从指定节点读取数据，带重试
func cacheGetFromNodeWithRetry(key string, endpoint string, dbID int, maxRetry int) ([]byte, error) {
	var lastErr error
	for i := 0; i < maxRetry; i++ {
		value, err := cacheGetFromNodeDB(key, endpoint, dbID)
		if err == nil {
			return value, nil
		}
		lastErr = err
		if i < maxRetry-1 {
			time.Sleep(time.Duration(i+1) * time.Second)
		}
	}
	return nil, fmt.Errorf("重试%d次后失败: %w", maxRetry, lastErr)
}

// cacheDelete 删除缓存（使用默认数据库）
func cacheDelete(key string) error {
	return cacheDeleteDB(key, 0)
}

// cacheDeleteDB 删除指定数据库的缓存
func cacheDeleteDB(key string, dbID int) error {
	var opts []interfaces.CacheOption

	// 如果 dbID 不为 0，则指定数据库，否则使用默认数据库
	if dbID > 0 {
		opts = append(opts, WithDatabase(fmt.Sprintf("db%d", dbID)))
	}

	err := burinClient.Delete(key, opts...)
	if err != nil {
		return fmt.Errorf("DELETE失败: %w", err)
	}

	return nil
}

// cacheExists 检查键是否存在（使用默认数据库）
func cacheExists(key string) (bool, error) {
	return cacheExistsDB(key, 0)
}

// cacheExistsDB 检查指定数据库中的键是否存在
func cacheExistsDB(key string, dbID int) (bool, error) {
	var opts []interfaces.CacheOption

	// 如果 dbID 不为 0，则指定数据库，否则使用默认数据库
	if dbID > 0 {
		opts = append(opts, WithDatabase(fmt.Sprintf("db%d", dbID)))
	}

	exists, err := burinClient.Exists(key, opts...)
	if err != nil {
		return false, fmt.Errorf("EXISTS失败: %w", err)
	}

	return exists, nil
}

// cacheMSet 批量设置缓存（默认DB0）
func cacheMSet(keyValues map[string][]byte) error {
	return cacheMSetDB(keyValues, 0)
}

// cacheMSetDB 在指定数据库中批量设置缓存
func cacheMSetDB(keyValues map[string][]byte, dbID int) error {
	for key, value := range keyValues {
		if err := cacheSetDB(key, value, dbID, 0); err != nil {
			return fmt.Errorf("MSet失败，键 %s: %w", key, err)
		}
	}
	return nil
}

// cacheMGet 批量获取缓存（默认DB0）
func cacheMGet(keys []string) (map[string][]byte, error) {
	return cacheMGetDB(keys, 0)
}

// cacheMGetDB 从指定数据库批量获取缓存
func cacheMGetDB(keys []string, dbID int) (map[string][]byte, error) {
	result := make(map[string][]byte)
	for _, key := range keys {
		value, err := cacheGetDB(key, dbID)
		if err != nil {
			continue
		}
		result[key] = value
	}
	return result, nil
}
