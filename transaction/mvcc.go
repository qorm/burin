package transaction

import (
	"fmt"
	"sync"
	"time"
)

// MVCCStore MVCC存储
type MVCCStore struct {
	// 键 -> 版本链表
	versions map[string]*VersionChain
	mu       sync.RWMutex

	// 全局时钟
	clock *LogicalClock

	// GC相关
	minActiveVersion int64
	gcThreshold      int64
}

// VersionChain 版本链
type VersionChain struct {
	key      string
	versions []*Version
	mu       sync.RWMutex
}

// Version 数据版本
type Version struct {
	Value     []byte
	Version   int64
	TxnID     string
	Timestamp time.Time
	Committed bool
	Deleted   bool
	PrevVer   *Version // 指向前一个版本
}

// LogicalClock 逻辑时钟
type LogicalClock struct {
	counter int64
	mu      sync.Mutex
}

// NewMVCCStore 创建MVCC存储
func NewMVCCStore() *MVCCStore {
	return &MVCCStore{
		versions:    make(map[string]*VersionChain),
		clock:       &LogicalClock{counter: 0},
		gcThreshold: 100, // 保留最近100个版本
	}
}

// NewLogicalClock 创建逻辑时钟
func NewLogicalClock() *LogicalClock {
	return &LogicalClock{counter: 0}
}

// Tick 时钟滴答，返回新的时间戳
func (lc *LogicalClock) Tick() int64 {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	lc.counter++
	return lc.counter
}

// Now 获取当前时间戳
func (lc *LogicalClock) Now() int64 {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	return lc.counter
}

// Update 更新时钟（用于接收到的时间戳）
func (lc *LogicalClock) Update(timestamp int64) int64 {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	if timestamp > lc.counter {
		lc.counter = timestamp
	}
	lc.counter++
	return lc.counter
}

// Read 读取指定版本的数据
func (ms *MVCCStore) Read(key string, version int64) ([]byte, bool, error) {
	ms.mu.RLock()
	chain, exists := ms.versions[key]
	ms.mu.RUnlock()

	if !exists {
		return nil, false, nil
	}

	chain.mu.RLock()
	defer chain.mu.RUnlock()

	// 查找小于等于指定版本的最新已提交版本
	for i := len(chain.versions) - 1; i >= 0; i-- {
		ver := chain.versions[i]
		if ver.Version <= version && ver.Committed {
			if ver.Deleted {
				return nil, false, nil
			}
			return ver.Value, true, nil
		}
	}

	return nil, false, nil
}

// Write 写入新版本
func (ms *MVCCStore) Write(key string, value []byte, txnID string) (*Version, error) {
	version := ms.clock.Tick()

	newVersion := &Version{
		Value:     value,
		Version:   version,
		TxnID:     txnID,
		Timestamp: time.Now(),
		Committed: false,
		Deleted:   false,
	}

	ms.mu.Lock()
	chain, exists := ms.versions[key]
	if !exists {
		chain = &VersionChain{
			key:      key,
			versions: make([]*Version, 0),
		}
		ms.versions[key] = chain
	}
	ms.mu.Unlock()

	chain.mu.Lock()
	defer chain.mu.Unlock()

	// 添加到版本链
	if len(chain.versions) > 0 {
		newVersion.PrevVer = chain.versions[len(chain.versions)-1]
	}
	chain.versions = append(chain.versions, newVersion)

	return newVersion, nil
}

// Delete 标记删除
func (ms *MVCCStore) Delete(key string, txnID string) (*Version, error) {
	version := ms.clock.Tick()

	deleteVersion := &Version{
		Value:     nil,
		Version:   version,
		TxnID:     txnID,
		Timestamp: time.Now(),
		Committed: false,
		Deleted:   true,
	}

	ms.mu.Lock()
	chain, exists := ms.versions[key]
	if !exists {
		chain = &VersionChain{
			key:      key,
			versions: make([]*Version, 0),
		}
		ms.versions[key] = chain
	}
	ms.mu.Unlock()

	chain.mu.Lock()
	defer chain.mu.Unlock()

	if len(chain.versions) > 0 {
		deleteVersion.PrevVer = chain.versions[len(chain.versions)-1]
	}
	chain.versions = append(chain.versions, deleteVersion)

	return deleteVersion, nil
}

// Commit 提交版本
func (ms *MVCCStore) Commit(key string, version int64) error {
	ms.mu.RLock()
	chain, exists := ms.versions[key]
	ms.mu.RUnlock()

	if !exists {
		return fmt.Errorf("key %s not found", key)
	}

	chain.mu.Lock()
	defer chain.mu.Unlock()

	// 查找指定版本并提交
	for _, ver := range chain.versions {
		if ver.Version == version {
			ver.Committed = true
			return nil
		}
	}

	return fmt.Errorf("version %d not found for key %s", version, key)
}

// Abort 回滚版本
func (ms *MVCCStore) Abort(key string, version int64) error {
	ms.mu.RLock()
	chain, exists := ms.versions[key]
	ms.mu.RUnlock()

	if !exists {
		return fmt.Errorf("key %s not found", key)
	}

	chain.mu.Lock()
	defer chain.mu.Unlock()

	// 移除未提交的版本
	newVersions := make([]*Version, 0)
	for _, ver := range chain.versions {
		if ver.Version != version {
			newVersions = append(newVersions, ver)
		}
	}
	chain.versions = newVersions

	return nil
}

// CreateSnapshot 创建快照
func (ms *MVCCStore) CreateSnapshot() *Snapshot {
	version := ms.clock.Now()

	return &Snapshot{
		Version:   version,
		Timestamp: time.Now(),
		store:     ms,
	}
}

// GC 垃圾回收旧版本
func (ms *MVCCStore) GC() int {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	removed := 0
	minVersion := ms.clock.Now() - ms.gcThreshold

	for key, chain := range ms.versions {
		chain.mu.Lock()

		// 保留最近的已提交版本和所有未提交的版本
		newVersions := make([]*Version, 0)
		var lastCommitted *Version

		for i := len(chain.versions) - 1; i >= 0; i-- {
			ver := chain.versions[i]

			if !ver.Committed {
				// 保留未提交的版本
				newVersions = append([]*Version{ver}, newVersions...)
			} else if ver.Version >= minVersion {
				// 保留在阈值内的版本
				newVersions = append([]*Version{ver}, newVersions...)
				if lastCommitted == nil {
					lastCommitted = ver
				}
			} else if lastCommitted == nil {
				// 保留至少一个已提交的版本
				newVersions = append([]*Version{ver}, newVersions...)
				lastCommitted = ver
			} else {
				removed++
			}
		}

		if len(newVersions) == 0 {
			delete(ms.versions, key)
		} else {
			chain.versions = newVersions
		}

		chain.mu.Unlock()
	}

	return removed
}

// GetVersionCount 获取键的版本数量
func (ms *MVCCStore) GetVersionCount(key string) int {
	ms.mu.RLock()
	chain, exists := ms.versions[key]
	ms.mu.RUnlock()

	if !exists {
		return 0
	}

	chain.mu.RLock()
	defer chain.mu.RUnlock()

	return len(chain.versions)
}

// GetAllVersions 获取键的所有版本（用于调试）
func (ms *MVCCStore) GetAllVersions(key string) []*Version {
	ms.mu.RLock()
	chain, exists := ms.versions[key]
	ms.mu.RUnlock()

	if !exists {
		return nil
	}

	chain.mu.RLock()
	defer chain.mu.RUnlock()

	// 返回副本
	versions := make([]*Version, len(chain.versions))
	copy(versions, chain.versions)
	return versions
}

// Snapshot MVCC快照辅助结构
type Snapshot struct {
	Version   int64
	Timestamp time.Time
	store     *MVCCStore
}

// Get 从快照读取数据
func (s *Snapshot) Get(key string) ([]byte, bool, error) {
	return s.store.Read(key, s.Version)
}

// GetVersion 获取快照版本号
func (s *Snapshot) GetVersion() int64 {
	return s.Version
}
