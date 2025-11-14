package store

import (
	"fmt"
	"hash/crc32"
	"sync"
	"time"

	"github.com/bytedance/sonic"

	"burin/cid"

	"github.com/dgraph-io/badger/v4"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/sirupsen/logrus"
)

// ShardedCacheEngine 分片缓存引擎
type ShardedCacheEngine struct {
	databases map[string][]*CacheShard // 数据库名 -> 分片数组
	config    *Config
	logger    *logrus.Logger
	metrics   *Metrics
	stopCh    chan struct{}
	doneCh    chan struct{}
	mu        sync.RWMutex

	// 分布式相关
	nodeID   string
	cluster  ClusterManager
	migrator *DataMigrator
}

// CacheShard 单个分片
type CacheShard struct {
	db       *badger.DB
	mu       sync.RWMutex
	config   *Config
	logger   *logrus.Logger
	metrics  *Metrics
	shardID  int
	database string // 所属数据库名

	// 分片范围
	startHash uint32
	endHash   uint32

	// TTL 管理
	ttlIndex map[string]time.Time
	ttlMu    sync.RWMutex
}

// Config 缓存配置
type Config struct {
	// 存储配置
	DataDir      string `yaml:"data_dir" mapstructure:"data_dir"`
	MaxCacheSize int64  `yaml:"max_cache_size" mapstructure:"max_cache_size"`
	SyncWrites   bool   `yaml:"sync_writes" mapstructure:"sync_writes"`

	// 分片配置
	ShardCount int   `yaml:"shard_count" mapstructure:"shard_count"`
	ShardSize  int64 `yaml:"shard_size" mapstructure:"shard_size"`

	// 性能配置
	ReadOnly     bool          `yaml:"read_only" mapstructure:"read_only"`
	InMemory     bool          `yaml:"in_memory" mapstructure:"in_memory"`
	GCFrequency  time.Duration `yaml:"gc_frequency" mapstructure:"gc_frequency"`
	TTLCheckFreq time.Duration `yaml:"ttl_check_frequency" mapstructure:"ttl_check_frequency"`

	// 网络配置
	Listen       string        `yaml:"listen" mapstructure:"listen"`
	MaxConns     int           `yaml:"max_conns" mapstructure:"max_conns"`
	ReadTimeout  time.Duration `yaml:"read_timeout"`
	WriteTimeout time.Duration `yaml:"write_timeout"`

	// 集群配置
	NodeID       string   `yaml:"node_id"`
	ClusterNodes []string `yaml:"cluster_nodes"`
	ReplicaCount int      `yaml:"replica_count"`
}

// Metrics 性能指标
type Metrics struct {
	// QPS 指标
	ReadOps   prometheus.Counter
	WriteOps  prometheus.Counter
	DeleteOps prometheus.Counter

	// 延迟指标
	ReadLatency   prometheus.Histogram
	WriteLatency  prometheus.Histogram
	DeleteLatency prometheus.Histogram

	// 缓存指标
	CacheHits   prometheus.Counter
	CacheMisses prometheus.Counter
	CacheSize   prometheus.Gauge

	// 分片指标
	ShardReads    *prometheus.CounterVec
	ShardWrites   *prometheus.CounterVec
	ShardHotspots prometheus.Histogram

	// 集群指标
	ClusterNodes   prometheus.Gauge
	DataMigration  prometheus.Counter
	ReplicationLag prometheus.Histogram

	// 连接指标
	ActiveConnections prometheus.Gauge
	TotalConnections  prometheus.Counter
}

// ClusterManager 集群管理接口
type ClusterManager interface {
	GetNodeForKey(key string) (string, error)
	GetReplicaNodes(key string) ([]string, error)
	AddNode(nodeID, address string) error
	RemoveNode(nodeID string) error
	GetClusterState() ClusterState
	NotifyDataMigration(from, to string, keyRange KeyRange) error
}

// KeyRange 键范围
type KeyRange struct {
	StartHash uint32 `json:"start_hash"`
	EndHash   uint32 `json:"end_hash"`
	ShardID   int    `json:"shard_id"`
}

// ClusterState 集群状态
type ClusterState struct {
	Nodes       map[string]NodeInfo `json:"nodes"`
	ShardRanges []KeyRange          `json:"shard_ranges"`
	Version     int64               `json:"version"`
}

// NodeInfo 节点信息
type NodeInfo struct {
	ID       string    `json:"id"`
	Address  string    `json:"address"`
	Status   string    `json:"status"`
	LastSeen time.Time `json:"last_seen"`
	Shards   []int     `json:"shards"`
}

// DataMigrator 数据迁移器
type DataMigrator struct {
	engine *ShardedCacheEngine
	logger *logrus.Logger

	// 迁移状态
	migrations map[string]*MigrationTask
	mu         sync.RWMutex
}

// MigrationTask 迁移任务
type MigrationTask struct {
	ID        string     `json:"id"`
	FromNode  string     `json:"from_node"`
	ToNode    string     `json:"to_node"`
	KeyRange  KeyRange   `json:"key_range"`
	Status    string     `json:"status"`
	Progress  float64    `json:"progress"`
	StartTime time.Time  `json:"start_time"`
	EndTime   *time.Time `json:"end_time,omitempty"`
	Error     string     `json:"error,omitempty"`
}

// CacheValue 缓存值结构
type CacheValue struct {
	Data      []byte    `json:"data"`
	TTL       int64     `json:"ttl"`
	CreatedAt time.Time `json:"created_at"`
	Version   int64     `json:"version"`
	Checksum  uint32    `json:"checksum"`
}

// NewShardedCacheEngine 创建分片缓存引擎
func NewShardedCacheEngine(config *Config) (*ShardedCacheEngine, error) {
	if config == nil {
		config = DefaultConfig()
	}

	if err := config.validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)

	metrics := initMetrics()

	engine := &ShardedCacheEngine{
		databases: make(map[string][]*CacheShard),
		config:    config,
		logger:    logger,
		metrics:   metrics,
		stopCh:    make(chan struct{}),
		doneCh:    make(chan struct{}),
		nodeID:    config.NodeID,
	}

	// 创建默认数据库
	if err := engine.CreateDatabase("default"); err != nil {
		return nil, fmt.Errorf("failed to create default database: %w", err)
	}

	// 自动创建 GEO 专用数据库
	if err := engine.CreateDatabase("__burin_geo__"); err != nil {
		return nil, fmt.Errorf("failed to create geo database: %w", err)
	}

	// 初始化数据迁移器
	engine.migrator = &DataMigrator{
		engine:     engine,
		logger:     logger,
		migrations: make(map[string]*MigrationTask),
	}

	// 启动后台任务
	go engine.runBackgroundTasks()

	logger.Infof("ShardedCacheEngine started with %d shards", config.ShardCount)
	return engine, nil
}

// initDatabaseShards 为指定数据库初始化分片
func (e *ShardedCacheEngine) initDatabaseShards(database string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// 如果数据库已存在，直接返回
	if _, exists := e.databases[database]; exists {
		return nil
	}

	shards := make([]*CacheShard, e.config.ShardCount)
	hashRangePerShard := uint32(0xFFFFFFFF) / uint32(e.config.ShardCount)

	for i := 0; i < e.config.ShardCount; i++ {
		startHash := uint32(i) * hashRangePerShard
		endHash := startHash + hashRangePerShard
		if i == e.config.ShardCount-1 {
			endHash = 0xFFFFFFFF // 最后一个分片包含到最大值
		}

		shard, err := e.createShard(i, startHash, endHash, database)
		if err != nil {
			return fmt.Errorf("failed to create shard %d for database %s: %w", i, database, err)
		}

		shards[i] = shard
		e.logger.Infof("Created shard %d for database %s: hash range [%x, %x]", i, database, startHash, endHash)
	}

	e.databases[database] = shards
	return nil
}

// CreateDatabase 创建新数据库
func (e *ShardedCacheEngine) CreateDatabase(database string) error {
	e.mu.Lock()

	// 检查数据库是否已存在
	if _, exists := e.databases[database]; exists {
		e.mu.Unlock()
		return fmt.Errorf("database %s already exists", database)
	}

	e.mu.Unlock()

	// 初始化分片
	if err := e.initDatabaseShards(database); err != nil {
		return fmt.Errorf("failed to init shards for database %s: %w", database, err)
	}

	e.logger.Infof("Created database %s", database)
	return nil
}

// GetDatabaseType 获取数据库的类型（已废弃，保留兼容性）
func (e *ShardedCacheEngine) GetDatabaseType(database string) (string, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	_, exists := e.databases[database]
	if !exists {
		return "", fmt.Errorf("database %s does not exist", database)
	}

	// 固定返回 cache 类型以兼容旧代码
	return "cache", nil
}

// DatabaseExists 检查数据库是否存在
func (e *ShardedCacheEngine) DatabaseExists(database string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()

	_, exists := e.databases[database]
	return exists
}

// ListDatabases 列出所有数据库
func (e *ShardedCacheEngine) ListDatabases() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	databases := make([]string, 0, len(e.databases))
	for db := range e.databases {
		databases = append(databases, db)
	}
	return databases
}

// GetDatabaseKeyCount 获取数据库中的键数量
func (e *ShardedCacheEngine) GetDatabaseKeyCount(database string) (int64, error) {
	if !e.DatabaseExists(database) {
		return 0, fmt.Errorf("database '%s' does not exist", database)
	}

	e.mu.RLock()
	shards, ok := e.databases[database]
	e.mu.RUnlock()

	if !ok {
		return 0, fmt.Errorf("database '%s' not found", database)
	}

	var totalCount int64 = 0
	for _, shard := range shards {
		count, err := shard.countKeysWithPrefix("")
		if err != nil {
			return 0, err
		}
		totalCount += count
	}

	return totalCount, nil
}

// DeleteDatabase 删除数据库
func (e *ShardedCacheEngine) DeleteDatabase(database string) error {
	// 检查是否是系统保留数据库
	if database == "__burin_system__" || database == "__burin_geo__" || database == "default" {
		return fmt.Errorf("cannot delete system database: %s", database)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// 检查数据库是否存在
	shards, exists := e.databases[database]
	if !exists {
		return fmt.Errorf("database '%s' does not exist", database)
	}

	// 关闭并删除所有分片
	for _, shard := range shards {
		if err := shard.db.Close(); err != nil {
			e.logger.Warnf("Failed to close shard for database %s: %v", database, err)
		}
	}

	// 从内存中移除
	delete(e.databases, database)

	e.logger.Infof("Deleted database: %s", database)
	return nil
}

// createShard 创建单个分片
func (e *ShardedCacheEngine) createShard(shardID int, startHash, endHash uint32, database string) (*CacheShard, error) {
	// 使用数据库隔离的目录结构: dataDir/{database}/shard_{id}
	dataDir := fmt.Sprintf("%s/%s/shard_%d", e.config.DataDir, database, shardID)
	opts := badger.DefaultOptions(dataDir)
	opts.SyncWrites = e.config.SyncWrites
	opts.InMemory = e.config.InMemory

	// 性能优化
	opts.NumMemtables = 4
	opts.NumLevelZeroTables = 16
	opts.NumLevelZeroTablesStall = 32
	opts.NumCompactors = 4
	opts.BlockCacheSize = int64((256 << 20) / e.config.ShardCount) // 平均分配缓存
	opts.IndexCacheSize = int64((100 << 20) / e.config.ShardCount)

	db, err := badger.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to open badger db for shard %d in database %s: %w", shardID, database, err)
	}

	shard := &CacheShard{
		db:        db,
		config:    e.config,
		logger:    e.logger,
		metrics:   e.metrics,
		shardID:   shardID,
		database:  database,
		startHash: startHash,
		endHash:   endHash,
		ttlIndex:  make(map[string]time.Time),
	}

	return shard, nil
}

// hashKey 计算key的hash值
func (e *ShardedCacheEngine) hashKey(key string) uint32 {
	return crc32.ChecksumIEEE([]byte(key))
}

// getShardForKey 根据 key 和数据库获取对应的分片
func (e *ShardedCacheEngine) getShardForKey(key string, database string) *CacheShard {
	// 确保数据库存在
	if err := e.initDatabaseShards(database); err != nil {
		e.logger.Errorf("Failed to init database shards for %s: %v", database, err)
		return nil
	}

	e.mu.RLock()
	shards, exists := e.databases[database]
	e.mu.RUnlock()

	if !exists {
		e.logger.Errorf("Database %s not found", database)
		return nil
	}

	hash := e.hashKey(key)
	e.logger.Debugf("getShardForKey: key=%s, database=%s, hash=%x (decimal=%d)", key, database, hash, hash)

	for i, shard := range shards {
		e.logger.Debugf("getShardForKey: Checking shard %d: hash range [%x, %x]", i, shard.startHash, shard.endHash)
		if hash >= shard.startHash && hash <= shard.endHash {
			e.logger.Infof("getShardForKey: Selected shard %d for key=%s (hash=%x in range [%x, %x])",
				i, key, hash, shard.startHash, shard.endHash)
			return shard
		}
	}

	// 兜底返回第一个分片
	e.logger.Warnf("getShardForKey: No matching shard found for key=%s (hash=%x), using shard 0", key, hash)
	return shards[0]
}

// SetWithDatabase 在指定数据库中设置缓存值
func (e *ShardedCacheEngine) SetWithDatabase(database, key string, value []byte, ttl time.Duration) error {
	start := time.Now()
	defer func() {
		e.metrics.WriteOps.Inc()
		e.metrics.WriteLatency.Observe(time.Since(start).Seconds())
	}()

	e.logger.Infof("SetWithDatabase: database=%s, key=%s, valueLength=%d, ttl=%v", database, key, len(value), ttl)

	shard := e.getShardForKey(key, database)
	if shard == nil {
		e.logger.Errorf("SetWithDatabase: Failed to get shard for key %s in database %s", key, database)
		return fmt.Errorf("failed to get shard for key %s in database %s", key, database)
	}
	e.logger.Infof("SetWithDatabase: Using shard %d for key %s", shard.shardID, key)
	e.metrics.ShardWrites.WithLabelValues(fmt.Sprintf("shard_%d", shard.shardID)).Inc()

	err := shard.Set(key, value, ttl)
	if err != nil {
		e.logger.Errorf("SetWithDatabase: Shard.Set failed for key %s: %v", key, err)
	} else {
		e.logger.Infof("SetWithDatabase: Successfully set key %s in shard %d", key, shard.shardID)
	}
	return err
}

// Set 设置缓存值（使用默认数据库）
func (e *ShardedCacheEngine) Set(key string, value []byte, ttl time.Duration) error {
	return e.SetWithDatabase("default", key, value, ttl)
}

// GetWithDatabase 从指定数据库获取缓存值
func (e *ShardedCacheEngine) GetWithDatabase(database, key string) ([]byte, bool, error) {
	start := time.Now()
	defer func() {
		e.metrics.ReadOps.Inc()
		e.metrics.ReadLatency.Observe(time.Since(start).Seconds())
	}()

	e.logger.Infof("GetWithDatabase: database=%s, key=%s", database, key)

	shard := e.getShardForKey(key, database)
	if shard == nil {
		e.logger.Errorf("GetWithDatabase: Failed to get shard for key %s in database %s", key, database)
		return nil, false, fmt.Errorf("failed to get shard for key %s in database %s", key, database)
	}
	e.logger.Infof("GetWithDatabase: Using shard %d for key %s", shard.shardID, key)
	e.metrics.ShardReads.WithLabelValues(fmt.Sprintf("shard_%d", shard.shardID)).Inc()

	value, found, err := shard.Get(key)
	if found {
		e.metrics.CacheHits.Inc()
		e.logger.Infof("GetWithDatabase: Cache HIT for key %s in shard %d (valueLength=%d)", key, shard.shardID, len(value))
	} else {
		e.metrics.CacheMisses.Inc()
		e.logger.Infof("GetWithDatabase: Cache MISS for key %s in shard %d (err=%v)", key, shard.shardID, err)
	}

	return value, found, err
}

// Get 获取缓存值（使用默认数据库）
func (e *ShardedCacheEngine) Get(key string) ([]byte, bool, error) {
	return e.GetWithDatabase("default", key)
}

// DeleteWithDatabase 从指定数据库删除缓存值
func (e *ShardedCacheEngine) DeleteWithDatabase(database, key string) error {
	start := time.Now()
	defer func() {
		e.metrics.DeleteOps.Inc()
		e.metrics.DeleteLatency.Observe(time.Since(start).Seconds())
	}()

	shard := e.getShardForKey(key, database)
	if shard == nil {
		return fmt.Errorf("failed to get shard for key %s in database %s", key, database)
	}
	return shard.Delete(key)
}

// Delete 删除缓存值（使用默认数据库）
func (e *ShardedCacheEngine) Delete(key string) error {
	return e.DeleteWithDatabase("default", key)
}

// 分片级别的操作
// Set 设置值到分片
func (s *CacheShard) Set(key string, value []byte, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.Infof("CacheShard.Set: shard=%d, database=%s, key=%s, valueLength=%d, ttl=%v",
		s.shardID, s.database, key, len(value), ttl)

	// 创建缓存值
	cacheValue := &CacheValue{
		Data:      value,
		TTL:       int64(ttl.Seconds()),
		CreatedAt: time.Now(),
		Version:   1,
		Checksum:  crc32.ChecksumIEEE(value),
	}

	s.logger.Debugf("CacheShard.Set: Created cache value - TTL=%d, CreatedAt=%v, Checksum=%x",
		cacheValue.TTL, cacheValue.CreatedAt, cacheValue.Checksum)

	// 序列化
	data, err := sonic.Marshal(cacheValue)
	if err != nil {
		s.logger.Errorf("CacheShard.Set: Failed to marshal cache value for key=%s: %v", key, err)
		return fmt.Errorf("failed to marshal cache value: %w", err)
	}

	s.logger.Debugf("CacheShard.Set: Marshaled cache value size=%d", len(data))

	// 写入 BadgerDB
	err = s.db.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte(key), data)
	})

	if err != nil {
		s.logger.Errorf("CacheShard.Set: Failed to set value in badger for key=%s: %v", key, err)
		return fmt.Errorf("failed to set value in badger: %w", err)
	}

	// 更新 TTL 索引
	if ttl > 0 {
		s.ttlMu.Lock()
		s.ttlIndex[key] = time.Now().Add(ttl)
		s.ttlMu.Unlock()
		s.logger.Debugf("CacheShard.Set: Added TTL index for key=%s, expires at=%v", key, time.Now().Add(ttl))
	}

	s.logger.Infof("CacheShard.Set: Successfully set key=%s in shard=%d", key, s.shardID)
	return nil
}

// Get 从分片获取值
func (s *CacheShard) Get(key string) ([]byte, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	s.logger.Infof("CacheShard.Get: shard=%d, database=%s, key=%s", s.shardID, s.database, key)

	var result []byte
	var found bool

	err := s.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(key))
		if err != nil {
			if err == badger.ErrKeyNotFound {
				s.logger.Infof("CacheShard.Get: Key not found in badger: %s", key)
				return nil
			}
			s.logger.Errorf("CacheShard.Get: BadgerDB error for key=%s: %v", key, err)
			return err
		}

		s.logger.Debugf("CacheShard.Get: Found item in badger for key=%s", key)

		return item.Value(func(val []byte) error {
			s.logger.Debugf("CacheShard.Get: Retrieved value size=%d for key=%s", len(val), key)

			var cacheValue CacheValue
			if err := sonic.Unmarshal(val, &cacheValue); err != nil {
				s.logger.Errorf("CacheShard.Get: Failed to unmarshal cache value for key=%s: %v", key, err)
				return fmt.Errorf("failed to unmarshal cache value: %w", err)
			}

			s.logger.Debugf("CacheShard.Get: Unmarshaled cache value - TTL=%d, CreatedAt=%v, DataLen=%d",
				cacheValue.TTL, cacheValue.CreatedAt, len(cacheValue.Data))

			// 检查 TTL
			if cacheValue.TTL > 0 {
				expireTime := cacheValue.CreatedAt.Add(time.Duration(cacheValue.TTL) * time.Second)
				if time.Now().After(expireTime) {
					// 过期，立即删除并返回未找到
					s.logger.Warnf("CacheShard.Get: Key %s expired at %v, deleting immediately", key, expireTime)
					go s.Delete(key) // 异步删除
					found = false
					return nil
				} else {
					s.logger.Debugf("CacheShard.Get: Key %s not expired, expires at %v", key, expireTime)
				}
			}

			// 验证校验和
			if crc32.ChecksumIEEE(cacheValue.Data) != cacheValue.Checksum {
				s.logger.Errorf("CacheShard.Get: Checksum mismatch for key=%s in shard=%d (expected=%x, got=%x)",
					key, s.shardID, cacheValue.Checksum, crc32.ChecksumIEEE(cacheValue.Data))
				return fmt.Errorf("checksum mismatch")
			}

			result = cacheValue.Data
			found = true
			s.logger.Infof("CacheShard.Get: Successfully retrieved key=%s, dataLen=%d", key, len(result))
			return nil
		})
	})

	if err != nil {
		s.logger.Errorf("CacheShard.Get: Failed to get value from badger for key=%s: %v", key, err)
		return nil, false, fmt.Errorf("failed to get value from badger: %w", err)
	}

	s.logger.Infof("CacheShard.Get: Final result for key=%s in shard=%d: found=%v, dataLen=%d",
		key, s.shardID, found, len(result))
	return result, found, nil
}

// Delete 从分片删除值
func (s *CacheShard) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	err := s.db.Update(func(txn *badger.Txn) error {
		return txn.Delete([]byte(key))
	})

	if err != nil && err != badger.ErrKeyNotFound {
		return fmt.Errorf("failed to delete value from badger: %w", err)
	}

	// 清理 TTL 索引
	s.ttlMu.Lock()
	delete(s.ttlIndex, key)
	s.ttlMu.Unlock()

	s.logger.Debugf("Delete key=%s in shard=%d", key, s.shardID)
	return nil
}

// 后台任务
// runBackgroundTasks 运行后台任务
func (e *ShardedCacheEngine) runBackgroundTasks() {
	gcTicker := time.NewTicker(e.config.GCFrequency)
	ttlTicker := time.NewTicker(e.config.TTLCheckFreq)
	metricsTicker := time.NewTicker(30 * time.Second)

	defer func() {
		gcTicker.Stop()
		ttlTicker.Stop()
		metricsTicker.Stop()
		close(e.doneCh)
	}()

	for {
		select {
		case <-e.stopCh:
			return
		case <-gcTicker.C:
			e.runGarbageCollection()
		case <-ttlTicker.C:
			e.cleanupExpiredKeys()
		case <-metricsTicker.C:
			e.updateMetrics()
		}
	}
}

// getAllShards 获取所有数据库的所有分片
func (e *ShardedCacheEngine) getAllShards() []*CacheShard {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var allShards []*CacheShard
	for _, shards := range e.databases {
		allShards = append(allShards, shards...)
	}
	return allShards
}

// runGarbageCollection 运行垃圾回收
func (e *ShardedCacheEngine) runGarbageCollection() {
	allShards := e.getAllShards()
	for i, shard := range allShards {
		go func(shardID int, s *CacheShard) {
			err := s.db.RunValueLogGC(0.5)
			if err != nil && err != badger.ErrNoRewrite {
				e.logger.Errorf("GC failed for shard %d in database %s: %v", shardID, s.database, err)
			} else {
				e.logger.Debugf("GC completed for shard %d in database %s", shardID, s.database)
			}
		}(i, shard)
	}
}

// cleanupExpiredKeys 清理过期键
func (e *ShardedCacheEngine) cleanupExpiredKeys() {
	allShards := e.getAllShards()
	var wg sync.WaitGroup

	for i, shard := range allShards {
		wg.Add(1)
		go func(shardID int, s *CacheShard) {
			defer wg.Done()

			s.ttlMu.RLock()
			var expiredKeys []string
			now := time.Now()

			for key, expireTime := range s.ttlIndex {
				if now.After(expireTime) {
					expiredKeys = append(expiredKeys, key)
				}
			}
			s.ttlMu.RUnlock()

			// 删除过期键
			if len(expiredKeys) > 0 {
				e.logger.Infof("Cleaning up %d expired keys from shard %d", len(expiredKeys), shardID)
				for _, key := range expiredKeys {
					if err := s.Delete(key); err != nil {
						e.logger.Errorf("Failed to delete expired key %s from shard %d: %v", key, shardID, err)
					}
				}
				e.logger.Infof("Successfully cleaned up %d expired keys from shard %d", len(expiredKeys), shardID)
			}
		}(i, shard)
	}

	// 等待所有分片清理完成
	wg.Wait()
}

// updateMetrics 更新指标
func (e *ShardedCacheEngine) updateMetrics() {
	var totalSize int64

	allShards := e.getAllShards()
	for _, shard := range allShards {
		// 这里可以添加更详细的指标收集
		// 比如分片大小、热点检测等
		totalSize += shard.getSize()
	}

	e.metrics.CacheSize.Set(float64(totalSize))
}

// getSize 获取分片大小
func (s *CacheShard) getSize() int64 {
	lsm, vlog := s.db.Size()
	return lsm + vlog
}

// Shutdown 优雅关闭
func (e *ShardedCacheEngine) Shutdown() error {
	e.logger.Info("Shutting down ShardedCacheEngine...")

	close(e.stopCh)
	<-e.doneCh

	// 关闭所有分片
	allShards := e.getAllShards()
	for i, shard := range allShards {
		if err := shard.db.Close(); err != nil {
			e.logger.Errorf("Failed to close shard %d in database %s: %v", i, shard.database, err)
		}
	}

	e.logger.Info("ShardedCacheEngine shutdown complete")
	return nil
}

// DefaultConfig 默认配置
func DefaultConfig() *Config {
	return &Config{
		DataDir:      "./data",
		MaxCacheSize: 10 << 30, // 10GB
		SyncWrites:   false,
		ShardCount:   16,
		ShardSize:    1 << 30, // 1GB per shard
		ReadOnly:     false,
		InMemory:     false,
		GCFrequency:  5 * time.Minute,
		TTLCheckFreq: 10 * time.Second, // 更频繁的TTL检查，从1分钟改为10秒
		Listen:       ":8080",
		MaxConns:     10000,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		ReplicaCount: 3,
	}
}

// validate 验证配置
func (c *Config) validate() error {
	if c.DataDir == "" {
		return fmt.Errorf("data_dir is required")
	}
	if c.ShardCount <= 0 {
		return fmt.Errorf("shard_count must be positive")
	}
	if c.MaxCacheSize <= 0 {
		return fmt.Errorf("max_cache_size must be positive")
	}
	if c.GCFrequency <= 0 {
		c.GCFrequency = 5 * time.Minute
	}
	if c.TTLCheckFreq <= 0 {
		c.TTLCheckFreq = 1 * time.Minute
	}
	if c.NodeID == "" {
		c.NodeID = "node_" + cid.Generate()
	}
	return nil
}

// initMetrics 初始化监控指标
func initMetrics() *Metrics {
	return &Metrics{
		ReadOps: promauto.NewCounter(prometheus.CounterOpts{
			Name: "cache_read_ops_total",
			Help: "Total number of read operations",
		}),
		WriteOps: promauto.NewCounter(prometheus.CounterOpts{
			Name: "cache_write_ops_total",
			Help: "Total number of write operations",
		}),
		DeleteOps: promauto.NewCounter(prometheus.CounterOpts{
			Name: "cache_delete_ops_total",
			Help: "Total number of delete operations",
		}),
		ReadLatency: promauto.NewHistogram(prometheus.HistogramOpts{
			Name:    "cache_read_latency_seconds",
			Help:    "Read operation latency in seconds",
			Buckets: prometheus.ExponentialBuckets(0.0001, 2, 15), // 0.1ms to ~3s
		}),
		WriteLatency: promauto.NewHistogram(prometheus.HistogramOpts{
			Name:    "cache_write_latency_seconds",
			Help:    "Write operation latency in seconds",
			Buckets: prometheus.ExponentialBuckets(0.0001, 2, 15),
		}),
		DeleteLatency: promauto.NewHistogram(prometheus.HistogramOpts{
			Name:    "cache_delete_latency_seconds",
			Help:    "Delete operation latency in seconds",
			Buckets: prometheus.ExponentialBuckets(0.0001, 2, 15),
		}),
		CacheHits: promauto.NewCounter(prometheus.CounterOpts{
			Name: "cache_hits_total",
			Help: "Total number of cache hits",
		}),
		CacheMisses: promauto.NewCounter(prometheus.CounterOpts{
			Name: "cache_misses_total",
			Help: "Total number of cache misses",
		}),
		CacheSize: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "cache_size_bytes",
			Help: "Current cache size in bytes",
		}),
		ShardReads: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "cache_shard_reads_total",
			Help: "Total read operations per shard",
		}, []string{"shard"}),
		ShardWrites: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "cache_shard_writes_total",
			Help: "Total write operations per shard",
		}, []string{"shard"}),
		ShardHotspots: promauto.NewHistogram(prometheus.HistogramOpts{
			Name: "cache_shard_hotspots",
			Help: "Distribution of operations across shards",
		}),
		ClusterNodes: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "cache_cluster_nodes",
			Help: "Number of nodes in cluster",
		}),
		DataMigration: promauto.NewCounter(prometheus.CounterOpts{
			Name: "cache_data_migration_total",
			Help: "Total number of data migration operations",
		}),
		ReplicationLag: promauto.NewHistogram(prometheus.HistogramOpts{
			Name: "cache_replication_lag_seconds",
			Help: "Replication lag in seconds",
		}),
		ActiveConnections: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "cache_active_connections",
			Help: "Number of active connections",
		}),
		TotalConnections: promauto.NewCounter(prometheus.CounterOpts{
			Name: "cache_total_connections",
			Help: "Total number of connections",
		}),
	}
}

// CountKeysWithDatabase 统计指定数据库中匹配前缀的键数量
func (e *ShardedCacheEngine) CountKeysWithDatabase(database string, prefix string) (int64, error) {
	e.mu.RLock()
	shards, exists := e.databases[database]
	e.mu.RUnlock()

	if !exists {
		return 0, fmt.Errorf("database %s not found", database)
	}

	e.logger.Infof("Counting keys in database '%s' with prefix '%s' across %d shards", database, prefix, len(shards))

	var totalCount int64
	for _, shard := range shards {
		count, err := shard.countKeysWithPrefix(prefix)
		if err != nil {
			return 0, err
		}
		e.logger.Infof("Shard %d contributed %d keys", shard.shardID, count)
		totalCount += count
	}

	e.logger.Infof("Total count for database '%s' with prefix '%s': %d", database, prefix, totalCount)
	return totalCount, nil
}

// CountKeys 统计默认数据库中匹配前缀的键数量
func (e *ShardedCacheEngine) CountKeys(prefix string) (int64, error) {
	return e.CountKeysWithDatabase("default", prefix)
}

// ListKeysWithDatabase 列出指定数据库中匹配前缀的键
func (e *ShardedCacheEngine) ListKeysWithDatabase(database string, prefix string, offset int, limit int) ([]string, int64, error) {
	e.mu.RLock()
	shards, exists := e.databases[database]
	e.mu.RUnlock()

	if !exists {
		return nil, 0, fmt.Errorf("database %s not found", database)
	}

	var allKeys []string
	var totalCount int64

	// 收集所有分片的键
	for _, shard := range shards {
		keys, count, err := shard.listKeysWithPrefix(prefix, 0, -1) // 先获取所有键
		if err != nil {
			return nil, 0, err
		}
		allKeys = append(allKeys, keys...)
		totalCount += count
	}

	// 应用分页
	if offset < 0 {
		offset = 0
	}

	if offset >= len(allKeys) {
		return []string{}, totalCount, nil
	}

	end := offset + limit
	if limit < 0 || end > len(allKeys) {
		end = len(allKeys)
	}

	return allKeys[offset:end], totalCount, nil
}

// ListKeys 列出默认数据库中匹配前缀的键
func (e *ShardedCacheEngine) ListKeys(prefix string, offset int, limit int) ([]string, int64, error) {
	return e.ListKeysWithDatabase("default", prefix, offset, limit)
}

// countKeysWithPrefix 统计分片中匹配前缀的键数量
func (s *CacheShard) countKeysWithPrefix(prefix string) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var count int64

	err := s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false // 只需要键，不需要值
		it := txn.NewIterator(opts)
		defer it.Close()

		prefix_bytes := []byte(prefix)
		s.logger.Infof("Shard %d: Counting keys with prefix '%s'", s.shardID, prefix)

		for it.Seek(prefix_bytes); it.ValidForPrefix(prefix_bytes); it.Next() {
			key := string(it.Item().Key())
			s.logger.Infof("Shard %d: Found key '%s'", s.shardID, key)
			count++
		}

		s.logger.Infof("Shard %d: Total count with prefix '%s': %d", s.shardID, prefix, count)
		return nil
	})

	return count, err
}

// listKeysWithPrefix 列出分片中匹配前缀的键
func (s *CacheShard) listKeysWithPrefix(prefix string, offset int, limit int) ([]string, int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var keys []string
	var totalCount int64

	err := s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false // 只需要键，不需要值
		it := txn.NewIterator(opts)
		defer it.Close()

		prefix_bytes := []byte(prefix)
		currentIndex := 0

		for it.Seek(prefix_bytes); it.ValidForPrefix(prefix_bytes); it.Next() {
			totalCount++

			// 应用分页逻辑
			if currentIndex >= offset && (limit < 0 || len(keys) < limit) {
				key := string(it.Item().Key())
				keys = append(keys, key)
			}
			currentIndex++
		}
		return nil
	})

	return keys, totalCount, err
}
