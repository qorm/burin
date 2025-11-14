package store

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/bytedance/sonic"
	"github.com/dgraph-io/badger/v4"
)

// BatchOperation 批量操作类型
type BatchOperation int

const (
	BatchGet BatchOperation = iota
	BatchSet
	BatchDelete
)

// BatchItem 批量操作项
type BatchItem struct {
	Key      string
	Value    []byte
	TTL      time.Duration
	Database string
}

// BatchResult 批量操作结果
type BatchResult struct {
	Key     string
	Value   []byte
	Found   bool
	Error   error
	Success bool
}

// BatchProcessor 批量操作处理器
type BatchProcessor struct {
	engine     *ShardedCacheEngine
	maxBatch   int           // 最大批次大小
	timeout    time.Duration // 操作超时
	workerPool int           // 工作协程数
}

// NewBatchProcessor 创建批量处理器
func NewBatchProcessor(engine *ShardedCacheEngine) *BatchProcessor {
	return &BatchProcessor{
		engine:     engine,
		maxBatch:   1000,
		timeout:    30 * time.Second,
		workerPool: 10,
	}
}

// MGet 批量获取
func (bp *BatchProcessor) MGet(ctx context.Context, keys []string, database string) map[string]*BatchResult {
	if database == "" {
		database = "default"
	}

	results := make(map[string]*BatchResult)
	resultsMu := sync.Mutex{}

	// 按分片分组键
	shardGroups := bp.groupKeysByShard(keys, database)

	// 并发处理每个分片的键
	var wg sync.WaitGroup
	for shardID, shardKeys := range shardGroups {
		wg.Add(1)
		go func(sid int, keys []string) {
			defer wg.Done()

			shard := bp.getShard(sid, database)
			if shard == nil {
				resultsMu.Lock()
				for _, key := range keys {
					results[key] = &BatchResult{
						Key:   key,
						Error: fmt.Errorf("shard not found"),
					}
				}
				resultsMu.Unlock()
				return
			}

			// 批量读取
			batchResults := bp.batchGetFromShard(shard, keys)

			resultsMu.Lock()
			for key, result := range batchResults {
				results[key] = result
			}
			resultsMu.Unlock()
		}(shardID, shardKeys)
	}

	wg.Wait()
	return results
}

// MSet 批量设置
func (bp *BatchProcessor) MSet(ctx context.Context, items []BatchItem) map[string]*BatchResult {
	results := make(map[string]*BatchResult)
	resultsMu := sync.Mutex{}

	// 按数据库和分片分组
	type shardKey struct {
		database string
		shardID  int
	}
	shardGroups := make(map[shardKey][]BatchItem)

	for _, item := range items {
		database := item.Database
		if database == "" {
			database = "default"
		}

		shardID := bp.getShardID(item.Key, database)
		key := shardKey{database: database, shardID: shardID}
		shardGroups[key] = append(shardGroups[key], item)
	}

	// 并发处理每个分片的数据
	var wg sync.WaitGroup
	for key, shardItems := range shardGroups {
		wg.Add(1)
		go func(db string, sid int, items []BatchItem) {
			defer wg.Done()

			shard := bp.getShard(sid, db)
			if shard == nil {
				resultsMu.Lock()
				for _, item := range items {
					results[item.Key] = &BatchResult{
						Key:     item.Key,
						Success: false,
						Error:   fmt.Errorf("shard not found"),
					}
				}
				resultsMu.Unlock()
				return
			}

			// 批量写入
			batchResults := bp.batchSetToShard(shard, items)

			resultsMu.Lock()
			for key, result := range batchResults {
				results[key] = result
			}
			resultsMu.Unlock()
		}(key.database, key.shardID, shardItems)
	}

	wg.Wait()
	return results
}

// MDelete 批量删除
func (bp *BatchProcessor) MDelete(ctx context.Context, keys []string, database string) map[string]*BatchResult {
	if database == "" {
		database = "default"
	}

	results := make(map[string]*BatchResult)
	resultsMu := sync.Mutex{}

	// 按分片分组
	shardGroups := bp.groupKeysByShard(keys, database)

	// 并发处理
	var wg sync.WaitGroup
	for shardID, shardKeys := range shardGroups {
		wg.Add(1)
		go func(sid int, keys []string) {
			defer wg.Done()

			shard := bp.getShard(sid, database)
			if shard == nil {
				resultsMu.Lock()
				for _, key := range keys {
					results[key] = &BatchResult{
						Key:     key,
						Success: false,
						Error:   fmt.Errorf("shard not found"),
					}
				}
				resultsMu.Unlock()
				return
			}

			// 批量删除
			batchResults := bp.batchDeleteFromShard(shard, keys)

			resultsMu.Lock()
			for key, result := range batchResults {
				results[key] = result
			}
			resultsMu.Unlock()
		}(shardID, shardKeys)
	}

	wg.Wait()
	return results
}

// batchGetFromShard 从分片批量获取
func (bp *BatchProcessor) batchGetFromShard(shard *CacheShard, keys []string) map[string]*BatchResult {
	results := make(map[string]*BatchResult)

	err := shard.db.View(func(txn *badger.Txn) error {
		for _, key := range keys {
			item, err := txn.Get([]byte(key))
			if err != nil {
				if err == badger.ErrKeyNotFound {
					results[key] = &BatchResult{
						Key:   key,
						Found: false,
					}
					continue
				}
				results[key] = &BatchResult{
					Key:   key,
					Found: false,
					Error: err,
				}
				continue
			}

			err = item.Value(func(val []byte) error {
				var cacheValue CacheValue
				if err := sonic.Unmarshal(val, &cacheValue); err != nil {
					results[key] = &BatchResult{
						Key:   key,
						Found: false,
						Error: err,
					}
					return nil
				}

				// 检查TTL
				if cacheValue.TTL > 0 {
					expireTime := cacheValue.CreatedAt.Add(time.Duration(cacheValue.TTL) * time.Second)
					if time.Now().After(expireTime) {
						results[key] = &BatchResult{
							Key:   key,
							Found: false,
						}
						// 异步删除过期键
						go shard.Delete(key)
						return nil
					}
				}

				results[key] = &BatchResult{
					Key:   key,
					Value: cacheValue.Data,
					Found: true,
				}
				return nil
			})
		}
		return nil
	})

	if err != nil {
		// 如果整个事务失败，标记所有键为错误
		for _, key := range keys {
			if _, exists := results[key]; !exists {
				results[key] = &BatchResult{
					Key:   key,
					Found: false,
					Error: err,
				}
			}
		}
	}

	return results
}

// batchSetToShard 批量写入分片
func (bp *BatchProcessor) batchSetToShard(shard *CacheShard, items []BatchItem) map[string]*BatchResult {
	results := make(map[string]*BatchResult)

	// 使用WriteBatch提高写入性能
	wb := shard.db.NewWriteBatch()
	defer wb.Cancel()

	for _, item := range items {
		cacheValue := &CacheValue{
			Data:      item.Value,
			TTL:       int64(item.TTL.Seconds()),
			CreatedAt: time.Now(),
			Version:   1,
		}

		data, err := sonic.Marshal(cacheValue)
		if err != nil {
			results[item.Key] = &BatchResult{
				Key:     item.Key,
				Success: false,
				Error:   err,
			}
			continue
		}

		if err := wb.Set([]byte(item.Key), data); err != nil {
			results[item.Key] = &BatchResult{
				Key:     item.Key,
				Success: false,
				Error:   err,
			}
			continue
		}

		results[item.Key] = &BatchResult{
			Key:     item.Key,
			Success: true,
		}

		// 更新TTL索引
		if item.TTL > 0 {
			shard.ttlMu.Lock()
			shard.ttlIndex[item.Key] = time.Now().Add(item.TTL)
			shard.ttlMu.Unlock()
		}
	}

	// 提交批次
	if err := wb.Flush(); err != nil {
		// 标记所有成功项为失败
		for key, result := range results {
			if result.Success {
				results[key].Success = false
				results[key].Error = err
			}
		}
	}

	return results
}

// batchDeleteFromShard 批量删除分片中的键
func (bp *BatchProcessor) batchDeleteFromShard(shard *CacheShard, keys []string) map[string]*BatchResult {
	results := make(map[string]*BatchResult)

	wb := shard.db.NewWriteBatch()
	defer wb.Cancel()

	for _, key := range keys {
		if err := wb.Delete([]byte(key)); err != nil {
			results[key] = &BatchResult{
				Key:     key,
				Success: false,
				Error:   err,
			}
			continue
		}

		results[key] = &BatchResult{
			Key:     key,
			Success: true,
		}

		// 清理TTL索引
		shard.ttlMu.Lock()
		delete(shard.ttlIndex, key)
		shard.ttlMu.Unlock()
	}

	// 提交批次
	if err := wb.Flush(); err != nil {
		for key, result := range results {
			if result.Success {
				results[key].Success = false
				results[key].Error = err
			}
		}
	}

	return results
}

// groupKeysByShard 按分片分组键
func (bp *BatchProcessor) groupKeysByShard(keys []string, database string) map[int][]string {
	groups := make(map[int][]string)

	for _, key := range keys {
		shardID := bp.getShardID(key, database)
		groups[shardID] = append(groups[shardID], key)
	}

	return groups
}

// getShardID 获取键对应的分片ID
func (bp *BatchProcessor) getShardID(key string, database string) int {
	return int(bp.engine.hashKey(key) % uint32(bp.engine.config.ShardCount))
}

// getShard 获取分片
func (bp *BatchProcessor) getShard(shardID int, database string) *CacheShard {
	bp.engine.mu.RLock()
	defer bp.engine.mu.RUnlock()

	if shards, exists := bp.engine.databases[database]; exists {
		if shardID >= 0 && shardID < len(shards) {
			return shards[shardID]
		}
	}

	return nil
}
