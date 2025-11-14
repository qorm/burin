package auth

import (
	"context"
	"fmt"
	"time"

	"burin/consensus"
	"burin/store"

	"github.com/bytedance/sonic"
	"github.com/sirupsen/logrus"
)

// StoreAdapter 将 store.ShardedCacheEngine 适配为 StorageBackend 接口
type StoreAdapter struct {
	engine        *store.ShardedCacheEngine
	consensusNode *consensus.Node
	logger        *logrus.Logger
}

// NewStoreAdapter 创建存储适配器
func NewStoreAdapter(engine *store.ShardedCacheEngine, consensusNode *consensus.Node, logger *logrus.Logger) *StoreAdapter {
	return &StoreAdapter{
		engine:        engine,
		consensusNode: consensusNode,
		logger:        logger,
	}
}

// Get 获取数据
func (sa *StoreAdapter) Get(ctx context.Context, database, key string) ([]byte, error) {
	value, found, err := sa.engine.GetWithDatabase(database, key)
	if err != nil {
		return nil, fmt.Errorf("failed to get key: %w", err)
	}
	if !found {
		return nil, fmt.Errorf("key not found: %s", key)
	}
	return value, nil
}

// Set 设置数据
func (sa *StoreAdapter) Set(ctx context.Context, database, key string, value []byte, ttl time.Duration) error {
	// 对于系统数据库，必须通过 Raft 同步
	if database == SystemDatabase && sa.consensusNode != nil && sa.consensusNode.IsLeader() {
		sa.logger.Infof("System database write detected, using Raft synchronization for key=%s", key)

		// 构造 Raft 命令
		raftCommand := map[string]interface{}{
			"operation": "set",
			"database":  database,
			"key":       key,
			"value":     string(value), // 转换为string以便JSON序列化
			"ttl":       int64(ttl.Seconds()),
			"timestamp": time.Now().UnixNano(),
		}

		commandData, err := sonic.Marshal(raftCommand)
		if err != nil {
			return fmt.Errorf("failed to marshal raft command: %w", err)
		}

		// 通过 Raft 提交
		if err := sa.consensusNode.ApplyCommand(commandData); err != nil {
			return fmt.Errorf("failed to apply raft command: %w", err)
		}

		sa.logger.Infof("System database key=%s replicated via Raft successfully", key)
		return nil
	}

	// 非系统数据库或非 Leader 节点，直接写入本地
	return sa.engine.SetWithDatabase(database, key, value, ttl)
}

// Delete 删除数据
func (sa *StoreAdapter) Delete(ctx context.Context, database, key string) error {
	// 对于系统数据库，必须通过 Raft 同步
	if database == SystemDatabase && sa.consensusNode != nil && sa.consensusNode.IsLeader() {
		sa.logger.Infof("System database delete detected, using Raft synchronization for key=%s", key)

		// 构造 Raft 命令
		raftCommand := map[string]interface{}{
			"operation": "delete",
			"database":  database,
			"key":       key,
			"timestamp": time.Now().UnixNano(),
		}

		commandData, err := sonic.Marshal(raftCommand)
		if err != nil {
			return fmt.Errorf("failed to marshal raft command: %w", err)
		}

		// 通过 Raft 提交
		if err := sa.consensusNode.ApplyCommand(commandData); err != nil {
			return fmt.Errorf("failed to apply raft command: %w", err)
		}

		sa.logger.Infof("System database key=%s deletion replicated via Raft successfully", key)
		return nil
	}

	// 非系统数据库或非 Leader 节点，直接从本地删除
	return sa.engine.DeleteWithDatabase(database, key)
}

// List 列出键
func (sa *StoreAdapter) List(ctx context.Context, database, prefix string) ([]string, error) {
	// 使用 ListKeysWithDatabase 方法，限制最多返回1000个
	keys, _, err := sa.engine.ListKeysWithDatabase(database, prefix, 0, 1000)
	if err != nil {
		return nil, fmt.Errorf("failed to list keys: %w", err)
	}
	return keys, nil
}
