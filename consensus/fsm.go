package consensus

import (
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/bytedance/sonic"

	"github.com/hashicorp/raft"
	"github.com/sirupsen/logrus"
)

// FSM 实现Raft的状态机接口
type FSM struct {
	mu                       sync.RWMutex
	store                    map[string]map[string][]byte    // database -> key -> value
	ttl                      map[string]map[string]time.Time // database -> key -> expiry time
	logger                   *logrus.Logger
	applyCallback            func(database, key string, value []byte, ttl time.Duration) // 回调函数用于同步SET到本地存储
	deleteCallback           func(database, key string)                                  // 回调函数用于同步DELETE到本地存储
	createDatabaseCallback   func(database, dataType string) error                       // 回调函数用于同步CREATE DATABASE到本地存储
	deleteDatabaseCallback   func(database string) error                                 // 回调函数用于同步DELETE DATABASE到本地存储
	geoAddCallback           func(database, key string, members interface{}) error       // 回调函数用于同步GEOADD到本地存储
	syncNodeMetadataCallback func(nodeID, clientAddr string)                             // 回调函数用于同步节点元数据
}

// FSMSnapshot 快照实现
type FSMSnapshot struct {
	store map[string]map[string][]byte
	ttl   map[string]map[string]time.Time
}

// NewFSM 创建新的状态机
func NewFSM(logger *logrus.Logger) *FSM {
	return &FSM{
		store:  make(map[string]map[string][]byte),
		ttl:    make(map[string]map[string]time.Time),
		logger: logger,
	}
}

// SetApplyCallback 设置Apply回调函数
func (f *FSM) SetApplyCallback(callback func(database, key string, value []byte, ttl time.Duration)) {
	f.applyCallback = callback
}

// SetDeleteCallback 设置Delete回调函数
func (f *FSM) SetDeleteCallback(callback func(database, key string)) {
	f.deleteCallback = callback
}

// SetCreateDatabaseCallback 设置CreateDatabase回调函数
func (f *FSM) SetCreateDatabaseCallback(callback func(database, dataType string) error) {
	f.createDatabaseCallback = callback
}

// SetDeleteDatabaseCallback 设置DeleteDatabase回调函数
func (f *FSM) SetDeleteDatabaseCallback(callback func(database string) error) {
	f.deleteDatabaseCallback = callback
}

// SetGeoAddCallback 设置GeoAdd回调函数
func (f *FSM) SetGeoAddCallback(callback func(database, key string, members interface{}) error) {
	f.geoAddCallback = callback
}

// SetSyncNodeMetadataCallback 设置同步节点元数据回调函数
func (f *FSM) SetSyncNodeMetadataCallback(callback func(nodeID, clientAddr string)) {
	f.syncNodeMetadataCallback = callback
}

// Apply 应用日志到状态机
func (f *FSM) Apply(l *raft.Log) interface{} {
	var cmd map[string]interface{}
	if err := sonic.Unmarshal(l.Data, &cmd); err != nil {
		f.logger.Errorf("Failed to unmarshal command: %v", err)
		return err
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	switch cmd["operation"] {
	case "set":
		db := cmd["database"].(string)
		key := cmd["key"].(string)

		// 正确处理value，支持string和[]byte类型
		var value []byte
		switch v := cmd["value"].(type) {
		case string:
			value = []byte(v)
		case []byte:
			value = v
		case []interface{}:
			// JSON反序列化的[]byte会变成[]interface{}
			value = make([]byte, len(v))
			for i, b := range v {
				if bInt, ok := b.(float64); ok {
					value[i] = byte(bInt)
				}
			}
		default:
			f.logger.Errorf("Invalid value type for SET operation: %T", v)
			return fmt.Errorf("invalid value type: %T", v)
		}

		if f.store[db] == nil {
			f.store[db] = make(map[string][]byte)
		}
		f.store[db][key] = value

		// 处理TTL
		var ttlDuration time.Duration
		if ttlSeconds, ok := cmd["ttl"]; ok && ttlSeconds != nil {
			if ttlValue, ok := ttlSeconds.(float64); ok && ttlValue > 0 {
				ttlDuration = time.Duration(ttlValue) * time.Second
				if f.ttl[db] == nil {
					f.ttl[db] = make(map[string]time.Time)
				}
				f.ttl[db][key] = time.Now().Add(ttlDuration)
				f.logger.Debugf("Set TTL for %s:%s to %v", db, key, f.ttl[db][key])
			}
		}

		f.logger.Infof("FSM Applied SET: db=%s, key=%s, valueLen=%d", db, key, len(value))

		// 调用回调函数同步到本地缓存引擎
		if f.applyCallback != nil {
			f.applyCallback(db, key, value, ttlDuration)
		}

	case "delete":
		db := cmd["database"].(string)
		key := cmd["key"].(string)

		if f.store[db] != nil {
			delete(f.store[db], key)
		}
		if f.ttl[db] != nil {
			delete(f.ttl[db], key)
		}

		f.logger.Infof("FSM Applied DELETE: db=%s, key=%s", db, key)

		// 调用回调函数同步删除到本地缓存引擎
		if f.deleteCallback != nil {
			f.deleteCallback(db, key)
		}

	case "sync_node_metadata":
		// 同步节点元数据（如 ClientAddr）
		nodeID := cmd["node_id"].(string)
		clientAddr := cmd["client_addr"].(string)

		f.logger.Infof("FSM Applied SYNC_NODE_METADATA: nodeID=%s, clientAddr=%s", nodeID, clientAddr)

		// 调用回调函数更新 Node 的 clientAddrs 映射
		if f.syncNodeMetadataCallback != nil {
			f.syncNodeMetadataCallback(nodeID, clientAddr)
		}

		return nil

	case "create_database":
		db := cmd["database"].(string)
		dataType := cmd["data_type"].(string)

		f.logger.Infof("FSM Applied CREATE_DATABASE: db=%s, dataType=%s", db, dataType)

		// 调用回调函数同步创建数据库到本地存储引擎
		if f.createDatabaseCallback != nil {
			if err := f.createDatabaseCallback(db, dataType); err != nil {
				f.logger.Errorf("Failed to create database via callback: %v", err)
				return err
			}
		}

	case "delete_database":
		db := cmd["database"].(string)

		f.logger.Infof("FSM Applied DELETE_DATABASE: db=%s", db)

		// 调用回调函数同步删除数据库到本地存储引擎
		if f.deleteDatabaseCallback != nil {
			if err := f.deleteDatabaseCallback(db); err != nil {
				f.logger.Errorf("Failed to delete database via callback: %v", err)
				return err
			}
		}

	case "geoadd":
		db := cmd["database"].(string)
		key := cmd["key"].(string)

		// 解析members数组
		membersData, ok := cmd["members"]
		if !ok {
			f.logger.Errorf("geoadd command missing members field")
			return fmt.Errorf("missing members field")
		}

		f.logger.Infof("FSM Applied GEOADD: db=%s, key=%s", db, key)

		// 调用回调函数同步GEO数据到本地存储引擎
		if f.geoAddCallback != nil {
			if err := f.geoAddCallback(db, key, membersData); err != nil {
				f.logger.Errorf("Failed to add geo data via callback: %v", err)
				return err
			}
		}
	}

	return nil
}

// Snapshot 创建快照
func (f *FSM) Snapshot() (raft.FSMSnapshot, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	// 深拷贝数据
	storeCopy := make(map[string]map[string][]byte)
	for db, dbStore := range f.store {
		storeCopy[db] = make(map[string][]byte)
		for k, v := range dbStore {
			storeCopy[db][k] = make([]byte, len(v))
			copy(storeCopy[db][k], v)
		}
	}

	ttlCopy := make(map[string]map[string]time.Time)
	for db, dbTTL := range f.ttl {
		ttlCopy[db] = make(map[string]time.Time)
		for k, v := range dbTTL {
			ttlCopy[db][k] = v
		}
	}

	return &FSMSnapshot{
		store: storeCopy,
		ttl:   ttlCopy,
	}, nil
}

// Restore 从快照恢复状态
func (f *FSM) Restore(snapshot io.ReadCloser) error {
	defer snapshot.Close()

	var data SnapshotData
	// 使用sonic读取所有数据
	buf, err := io.ReadAll(snapshot)
	if err != nil {
		return err
	}

	if err := sonic.Unmarshal(buf, &data); err != nil {
		return err
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	f.store = data.Store
	f.ttl = data.TTL

	f.logger.Infof("Restored state from snapshot: %d databases, %d total keys",
		len(f.store), f.getTotalKeys())

	return nil
}

// getTotalKeys 获取总键数
func (f *FSM) getTotalKeys() int {
	total := 0
	for _, dbStore := range f.store {
		total += len(dbStore)
	}
	return total
}

// Get 获取值
func (f *FSM) Get(database, key string) ([]byte, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	// 检查过期时间
	if f.ttl[database] != nil {
		if expiry, exists := f.ttl[database][key]; exists {
			if time.Now().After(expiry) {
				// 键已过期，但由于一致性要求，不在这里删除
				return nil, false
			}
		}
	}

	if f.store[database] == nil {
		return nil, false
	}

	value, exists := f.store[database][key]
	if !exists {
		return nil, false
	}

	// 返回值的副本
	result := make([]byte, len(value))
	copy(result, value)
	return result, true
}

// Persist 持久化快照
func (snapshot *FSMSnapshot) Persist(sink raft.SnapshotSink) error {
	defer sink.Close()

	snapshotData := &SnapshotData{
		Store: snapshot.store,
		TTL:   snapshot.ttl,
		Metadata: SnapshotMetadata{
			Version:   1,
			CreatedAt: time.Now(),
		},
	}

	// 使用sonic序列化数据
	data, err := sonic.Marshal(snapshotData)
	if err != nil {
		return err
	}

	_, err = sink.Write(data)
	return err
}

// Release 释放快照资源
func (snapshot *FSMSnapshot) Release() {
	// 清理资源
	snapshot.store = nil
	snapshot.ttl = nil
}
