package transaction

import (
	"context"
	"fmt"
	"sync"
	"time"

	"burin/cid"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/sirupsen/logrus"
)

// CacheEngine 缓存引擎接口
type CacheEngine interface {
	Set(key string, value []byte, ttl time.Duration) error
	Get(key string) ([]byte, bool, error)
	Delete(key string) error
	// 新增数据库感知的方法
	SetWithDatabase(database, key string, value []byte, ttl time.Duration) error
	GetWithDatabase(database, key string) ([]byte, bool, error)
	DeleteWithDatabase(database, key string) error
}

// ConsensusEngine consensus引擎接口
type ConsensusEngine interface {
	SetValueWithDatabase(database, key string, value []byte, ttl time.Duration) error
	GetValueWithDatabase(database, key string) ([]byte, bool, error)
	DeleteValueWithDatabase(database, key string) error
}

// TransactionManager 分布式事务管理器
type TransactionManager struct {
	// 本地事务状态
	transactions map[string]*Transaction
	mu           sync.RWMutex

	// 协调器相关
	coordinator Coordinator
	participant Participant

	// 配置和依赖
	config          *TransactionConfig
	logger          *logrus.Logger
	metrics         *TransactionMetrics
	cacheEngine     CacheEngine     // 缓存引擎接口
	consensusEngine ConsensusEngine // consensus引擎接口

	// 生命周期管理
	stopCh chan struct{}
	doneCh chan struct{}
}

// TransactionConfig 事务配置
type TransactionConfig struct {
	NodeID          string         `yaml:"node_id" mapstructure:"node_id"`
	Timeout         time.Duration  `yaml:"timeout" mapstructure:"timeout"`
	RetryAttempts   int            `yaml:"retry_attempts" mapstructure:"retry_attempts"`
	RetryInterval   time.Duration  `yaml:"retry_interval" mapstructure:"retry_interval"`
	CleanupInterval time.Duration  `yaml:"cleanup_interval" mapstructure:"cleanup_interval"`
	MaxConcurrent   int            `yaml:"max_concurrent" mapstructure:"max_concurrent"`
	IsolationLevel  IsolationLevel `yaml:"isolation_level" mapstructure:"isolation_level"`
}

// IsolationLevel 隔离级别
type IsolationLevel int

const (
	ReadCommitted IsolationLevel = iota
	RepeatableRead
	Serializable
)

// Transaction 事务结构
type Transaction struct {
	ID           string                   `json:"id"`
	Type         TransactionType          `json:"type"`
	Status       TransactionStatus        `json:"status"`
	StartTime    time.Time                `json:"start_time"`
	Timeout      time.Duration            `json:"timeout"`
	Coordinator  string                   `json:"coordinator"`
	Participants []string                 `json:"participants"`
	Operations   []*TransactionOp         `json:"operations"`
	Snapshots    map[string]*MVCCSnapshot `json:"snapshots"`
	LockSet      map[string]LockType      `json:"lock_set"`

	// 2PC 状态
	PrepareResults map[string]PrepareResult `json:"prepare_results"`
	CommitResults  map[string]CommitResult  `json:"commit_results"`

	// 上下文和取消
	ctx    context.Context    `json:"-"`
	cancel context.CancelFunc `json:"-"`

	mu sync.RWMutex `json:"-"`
}

// TransactionType 事务类型
type TransactionType int

const (
	ReadOnlyTransaction TransactionType = iota
	ReadWriteTransaction
	DistributedTransaction
)

// TransactionStatus 事务状态
type TransactionStatus int

const (
	TxnPending TransactionStatus = iota
	TxnPreparing
	TxnPrepared
	TxnCommitting
	TxnCommitted
	TxnAborting
	TxnAborted
	TxnTimeout
)

// TransactionOp 事务操作
type TransactionOp struct {
	Type      OpType                 `json:"type"`
	Key       string                 `json:"key"`
	Value     []byte                 `json:"value,omitempty"`
	Database  string                 `json:"database,omitempty"` // 数据库名
	Version   int64                  `json:"version"`
	Timestamp time.Time              `json:"timestamp"`
	NodeID    string                 `json:"node_id"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// OpType 操作类型
type OpType int

const (
	OpGet OpType = iota
	OpSet
	OpDelete
	OpExists
)

// String 返回操作类型的字符串表示
func (op OpType) String() string {
	switch op {
	case OpGet:
		return "GET"
	case OpSet:
		return "SET"
	case OpDelete:
		return "DELETE"
	case OpExists:
		return "EXISTS"
	default:
		return "UNKNOWN"
	}
}

// LockType 锁类型
type LockType int

const (
	SharedLock LockType = iota
	ExclusiveLock
)

// String 返回锁类型的字符串表示
func (lt LockType) String() string {
	switch lt {
	case SharedLock:
		return "SHARED"
	case ExclusiveLock:
		return "EXCLUSIVE"
	default:
		return "UNKNOWN"
	}
}

// MVCCSnapshot MVCC快照
type MVCCSnapshot struct {
	Timestamp time.Time        `json:"timestamp"`
	Version   int64            `json:"version"`
	ReadSet   map[string]int64 `json:"read_set"`
	WriteSet  map[string]int64 `json:"write_set"`
	DeleteSet map[string]int64 `json:"delete_set"`
}

// Coordinator 事务协调器接口
type Coordinator interface {
	StartTransaction(ctx context.Context, txnType TransactionType, participants []string) (*Transaction, error)
	PrepareTransaction(ctx context.Context, txnID string) error
	CommitTransaction(ctx context.Context, txnID string) error
	AbortTransaction(ctx context.Context, txnID string) error
}

// Participant 事务参与者接口
type Participant interface {
	Prepare(ctx context.Context, txnID string, operations []*TransactionOp) (PrepareResult, error)
	Commit(ctx context.Context, txnID string) (CommitResult, error)
	Abort(ctx context.Context, txnID string) error
}

// PrepareResult 预提交结果
type PrepareResult struct {
	Success   bool                `json:"success"`
	NodeID    string              `json:"node_id"`
	Version   int64               `json:"version"`
	Error     string              `json:"error,omitempty"`
	Locks     map[string]LockType `json:"locks"`
	Timestamp time.Time           `json:"timestamp"`
}

// CommitResult 提交结果
type CommitResult struct {
	Success   bool      `json:"success"`
	NodeID    string    `json:"node_id"`
	Version   int64     `json:"version"`
	Error     string    `json:"error,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// TransactionMetrics 事务指标
type TransactionMetrics struct {
	// 事务指标
	TransactionsStarted   prometheus.Counter
	TransactionsCommitted prometheus.Counter
	TransactionsAborted   prometheus.Counter
	TransactionDuration   prometheus.Histogram

	// 2PC 指标
	PrepareLatency      prometheus.Histogram
	CommitLatency       prometheus.Histogram
	TwoPhaseSuccessRate prometheus.Gauge

	// 锁指标
	LocksAcquired     prometheus.Counter
	LocksReleased     prometheus.Counter
	LockWaitTime      prometheus.Histogram
	DeadlocksDetected prometheus.Counter

	// MVCC 指标
	SnapshotsCreated  prometheus.Counter
	ConflictsDetected prometheus.Counter
	ReadSetSize       prometheus.Histogram
	WriteSetSize      prometheus.Histogram
}

// NewTransactionManager 创建事务管理器
func NewTransactionManager(config *TransactionConfig, cacheEngine CacheEngine, consensusEngine ConsensusEngine) (*TransactionManager, error) {
	if config == nil {
		config = DefaultTransactionConfig()
	}

	if err := config.validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	if cacheEngine == nil {
		return nil, fmt.Errorf("cache engine cannot be nil")
	}

	if consensusEngine == nil {
		return nil, fmt.Errorf("consensus engine cannot be nil")
	}

	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)

	tm := &TransactionManager{
		transactions:    make(map[string]*Transaction),
		config:          config,
		logger:          logger,
		metrics:         initTransactionMetrics(),
		cacheEngine:     cacheEngine,
		consensusEngine: consensusEngine,
		stopCh:          make(chan struct{}),
		doneCh:          make(chan struct{}),
	}

	// 创建协调器和参与者
	tm.coordinator = NewCoordinator(tm)
	tm.participant = NewParticipant(tm)

	// 启动后台任务
	go tm.runBackgroundTasks()

	logger.Infof("TransactionManager started with node_id=%s", config.NodeID)
	return tm, nil
}

// BeginTransaction 开始事务
func (tm *TransactionManager) BeginTransaction(ctx context.Context, txnType TransactionType) (*Transaction, error) {
	tm.metrics.TransactionsStarted.Inc()

	txnID := "txn_" + tm.config.NodeID + "_" + cid.Generate()

	txnCtx, cancel := context.WithTimeout(ctx, tm.config.Timeout)

	txn := &Transaction{
		ID:             txnID,
		Type:           txnType,
		Status:         TxnPending,
		StartTime:      time.Now(),
		Timeout:        tm.config.Timeout,
		Coordinator:    tm.config.NodeID,
		Operations:     make([]*TransactionOp, 0),
		Snapshots:      make(map[string]*MVCCSnapshot),
		LockSet:        make(map[string]LockType),
		PrepareResults: make(map[string]PrepareResult),
		CommitResults:  make(map[string]CommitResult),
		ctx:            txnCtx,
		cancel:         cancel,
	}

	// 创建 MVCC 快照
	snapshot := &MVCCSnapshot{
		Timestamp: time.Now(),
		Version:   time.Now().UnixNano(),
		ReadSet:   make(map[string]int64),
		WriteSet:  make(map[string]int64),
		DeleteSet: make(map[string]int64),
	}
	txn.Snapshots[tm.config.NodeID] = snapshot

	tm.mu.Lock()
	tm.transactions[txnID] = txn
	tm.mu.Unlock()

	tm.logger.Infof("Started transaction %s of type %d", txnID, txnType)
	return txn, nil
}

// AddOperation 添加事务操作
func (tm *TransactionManager) AddOperation(txnID string, op *TransactionOp) error {
	tm.mu.RLock()
	txn, exists := tm.transactions[txnID]
	tm.mu.RUnlock()

	if !exists {
		return fmt.Errorf("transaction %s not found", txnID)
	}

	txn.mu.Lock()
	defer txn.mu.Unlock()

	if txn.Status != TxnPending {
		return fmt.Errorf("transaction %s is not pending", txnID)
	}

	op.Timestamp = time.Now()
	op.NodeID = tm.config.NodeID

	txn.Operations = append(txn.Operations, op)

	// 更新快照中的读写集合
	snapshot := txn.Snapshots[tm.config.NodeID]
	switch op.Type {
	case OpGet, OpExists:
		snapshot.ReadSet[op.Key] = op.Version
	case OpSet:
		snapshot.WriteSet[op.Key] = op.Version
	case OpDelete:
		snapshot.DeleteSet[op.Key] = op.Version
	}

	tm.logger.Debugf("Added operation %s to transaction %s", op.Type, txnID)
	return nil
}

// GetTransactionValue 在事务内获取值，优先从事务的写集合中获取
func (tm *TransactionManager) GetTransactionValue(txnID string, key string) ([]byte, bool, error) {
	return tm.GetTransactionValueWithDatabase(txnID, "default", key)
}

// GetTransactionValueWithDatabase 获取事务中的值（带数据库参数）
func (tm *TransactionManager) GetTransactionValueWithDatabase(txnID string, database string, key string) ([]byte, bool, error) {
	tm.mu.RLock()
	txn, exists := tm.transactions[txnID]
	tm.mu.RUnlock()

	if !exists {
		return nil, false, fmt.Errorf("transaction %s not found", txnID)
	}

	txn.mu.RLock()
	defer txn.mu.RUnlock()

	// 首先检查事务的写集合，看是否有未提交的写操作
	for i := len(txn.Operations) - 1; i >= 0; i-- {
		op := txn.Operations[i]
		// 检查数据库和key是否匹配
		opDatabase := op.Database
		if opDatabase == "" {
			opDatabase = "default"
		}
		if opDatabase == database && op.Key == key {
			switch op.Type {
			case OpSet:
				// 在事务内找到了SET操作，返回这个值
				return op.Value, true, nil
			case OpDelete:
				// 在事务内找到了DELETE操作，返回不存在
				return nil, false, nil
			}
		}
	}

	// 事务内没有找到相关操作，从consensus引擎获取
	if tm.consensusEngine != nil {
		return tm.consensusEngine.GetValueWithDatabase(database, key)
	}

	return nil, false, nil
}

// CommitTransaction 提交事务
func (tm *TransactionManager) CommitTransaction(ctx context.Context, txnID string) error {
	start := time.Now()
	defer func() {
		tm.metrics.TransactionDuration.Observe(time.Since(start).Seconds())
	}()

	tm.mu.RLock()
	txn, exists := tm.transactions[txnID]
	tm.mu.RUnlock()

	if !exists {
		return fmt.Errorf("transaction %s not found", txnID)
	}

	txn.mu.Lock()
	defer txn.mu.Unlock()

	if txn.Status != TxnPending {
		return fmt.Errorf("transaction %s is not pending", txnID)
	}

	// 检查事务是否超时
	elapsed := time.Since(txn.StartTime)
	tm.logger.Infof("Checking timeout for transaction %s: elapsed=%v, timeout=%v",
		txnID, elapsed, txn.Timeout)
	if elapsed > txn.Timeout {
		txn.Status = TxnTimeout
		tm.metrics.TransactionsAborted.Inc()
		tm.logger.Warnf("Transaction %s timed out (elapsed: %v, timeout: %v)",
			txnID, elapsed, txn.Timeout)
		return fmt.Errorf("transaction %s has timed out", txnID)
	}

	// 检查冲突
	if err := tm.detectConflicts(txn); err != nil {
		tm.metrics.ConflictsDetected.Inc()
		tm.AbortTransaction(ctx, txnID)
		return fmt.Errorf("conflict detected: %w", err)
	}

	// 根据事务类型选择提交策略
	switch txn.Type {
	case ReadOnlyTransaction:
		return tm.commitReadOnlyTransaction(ctx, txn)
	case ReadWriteTransaction:
		return tm.commitLocalTransaction(ctx, txn)
	case DistributedTransaction:
		return tm.coordinator.CommitTransaction(ctx, txnID)
	default:
		return fmt.Errorf("unknown transaction type: %d", txn.Type)
	}
}

// AbortTransaction 回滚事务
func (tm *TransactionManager) AbortTransaction(ctx context.Context, txnID string) error {
	tm.mu.RLock()
	txn, exists := tm.transactions[txnID]
	tm.mu.RUnlock()

	if !exists {
		return fmt.Errorf("transaction %s not found", txnID)
	}

	txn.mu.Lock()
	defer txn.mu.Unlock()

	if txn.Status == TxnAborted || txn.Status == TxnCommitted {
		return fmt.Errorf("transaction %s already finalized", txnID)
	}

	txn.Status = TxnAborting

	// 释放锁
	tm.releaseLocks(txn)

	// 清理资源
	txn.cancel()
	txn.Status = TxnAborted

	tm.metrics.TransactionsAborted.Inc()
	tm.logger.Infof("Aborted transaction %s", txnID)

	return nil
}

// commitReadOnlyTransaction 提交只读事务
func (tm *TransactionManager) commitReadOnlyTransaction(ctx context.Context, txn *Transaction) error {
	txn.Status = TxnCommitted
	txn.cancel()

	tm.metrics.TransactionsCommitted.Inc()
	tm.logger.Infof("Committed read-only transaction %s", txn.ID)

	return nil
}

// commitLocalTransaction 提交本地事务
func (tm *TransactionManager) commitLocalTransaction(ctx context.Context, txn *Transaction) error {
	// 再次检查超时（防御性编程）
	if time.Since(txn.StartTime) > txn.Timeout {
		txn.Status = TxnTimeout
		tm.metrics.TransactionsAborted.Inc()
		tm.logger.Warnf("Transaction %s timed out during commit (elapsed: %v, timeout: %v)",
			txn.ID, time.Since(txn.StartTime), txn.Timeout)
		return fmt.Errorf("transaction %s has timed out", txn.ID)
	}

	txn.Status = TxnCommitting

	// 应用所有写操作，使用consensus引擎确保集群一致性
	for _, op := range txn.Operations {
		switch op.Type {
		case OpSet:
			var err error
			if op.Database != "" {
				err = tm.consensusEngine.SetValueWithDatabase(op.Database, op.Key, op.Value, 0)
			} else {
				err = tm.consensusEngine.SetValueWithDatabase("default", op.Key, op.Value, 0)
			}
			if err != nil {
				tm.logger.Errorf("Failed to apply SET operation via consensus for database %s, key %s: %v", op.Database, op.Key, err)
				// 回滚事务
				txn.Status = TxnAborting
				return fmt.Errorf("failed to apply SET operation via consensus: %w", err)
			}
			tm.logger.Infof("Applied SET operation via consensus for database %s, key %s", op.Database, op.Key)
		case OpDelete:
			var err error
			if op.Database != "" {
				err = tm.consensusEngine.DeleteValueWithDatabase(op.Database, op.Key)
			} else {
				err = tm.consensusEngine.DeleteValueWithDatabase("default", op.Key)
			}
			if err != nil {
				tm.logger.Errorf("Failed to apply DELETE operation via consensus for database %s, key %s: %v", op.Database, op.Key, err)
				// 回滚事务
				txn.Status = TxnAborting
				return fmt.Errorf("failed to apply DELETE operation via consensus: %w", err)
			}
			tm.logger.Infof("Applied DELETE operation via consensus for database %s, key %s", op.Database, op.Key)
		default:
			tm.logger.Infof("Skipping non-write operation %s for key %s", op.Type, op.Key)
		}
	}

	txn.Status = TxnCommitted
	txn.cancel()

	// 释放锁
	tm.releaseLocks(txn)

	tm.metrics.TransactionsCommitted.Inc()
	tm.logger.Infof("Committed local transaction %s with %d operations via consensus", txn.ID, len(txn.Operations))

	return nil
}

// detectConflicts 检测事务冲突
func (tm *TransactionManager) detectConflicts(txn *Transaction) error {
	snapshot := txn.Snapshots[tm.config.NodeID]

	// 检查读写冲突
	for key, readVersion := range snapshot.ReadSet {
		// 这里需要检查是否有其他事务在此之后修改了这个key
		// 简化实现，实际需要与存储引擎集成
		_ = key
		_ = readVersion
	}

	// 检查写写冲突
	for key, writeVersion := range snapshot.WriteSet {
		// 检查是否有其他事务同时写入相同的key
		_ = key
		_ = writeVersion
	}

	return nil
}

// releaseLocks 释放事务持有的锁
func (tm *TransactionManager) releaseLocks(txn *Transaction) {
	for key, lockType := range txn.LockSet {
		tm.metrics.LocksReleased.Inc()
		tm.logger.Debugf("Released %s lock on key %s for transaction %s",
			lockType, key, txn.ID)
	}

	txn.LockSet = make(map[string]LockType)
}

// runBackgroundTasks 运行后台任务
func (tm *TransactionManager) runBackgroundTasks() {
	cleanupTicker := time.NewTicker(tm.config.CleanupInterval)
	defer func() {
		cleanupTicker.Stop()
		close(tm.doneCh)
	}()

	for {
		select {
		case <-tm.stopCh:
			return
		case <-cleanupTicker.C:
			tm.cleanupExpiredTransactions()
		}
	}
}

// cleanupExpiredTransactions 清理过期事务
func (tm *TransactionManager) cleanupExpiredTransactions() {
	now := time.Now()
	var expiredTxns []string

	tm.mu.RLock()
	for txnID, txn := range tm.transactions {
		if now.Sub(txn.StartTime) > txn.Timeout {
			expiredTxns = append(expiredTxns, txnID)
		}
	}
	tm.mu.RUnlock()

	for _, txnID := range expiredTxns {
		tm.AbortTransaction(context.Background(), txnID)

		tm.mu.Lock()
		delete(tm.transactions, txnID)
		tm.mu.Unlock()

		tm.logger.Infof("Cleaned up expired transaction %s", txnID)
	}

	if len(expiredTxns) > 0 {
		tm.logger.Infof("Cleaned up %d expired transactions", len(expiredTxns))
	}
}

// GetTransaction 获取事务信息
func (tm *TransactionManager) GetTransaction(txnID string) (*Transaction, error) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	txn, exists := tm.transactions[txnID]
	if !exists {
		return nil, fmt.Errorf("transaction %s not found", txnID)
	}

	return txn, nil
}

// ListTransactions 列出所有事务
func (tm *TransactionManager) ListTransactions() []*Transaction {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	txns := make([]*Transaction, 0, len(tm.transactions))
	for _, txn := range tm.transactions {
		txns = append(txns, txn)
	}

	return txns
}

// Shutdown 优雅关闭
func (tm *TransactionManager) Shutdown() error {
	tm.logger.Info("Shutting down TransactionManager...")

	close(tm.stopCh)
	<-tm.doneCh

	// 回滚所有活跃事务
	tm.mu.RLock()
	activeTxns := make([]string, 0, len(tm.transactions))
	for txnID := range tm.transactions {
		activeTxns = append(activeTxns, txnID)
	}
	tm.mu.RUnlock()

	for _, txnID := range activeTxns {
		tm.AbortTransaction(context.Background(), txnID)
	}

	tm.logger.Info("TransactionManager shutdown complete")
	return nil
}

// DefaultTransactionConfig 默认事务配置
func DefaultTransactionConfig() *TransactionConfig {
	return &TransactionConfig{
		Timeout:         30 * time.Second,
		RetryAttempts:   3,
		RetryInterval:   1 * time.Second,
		CleanupInterval: 5 * time.Minute,
		MaxConcurrent:   1000,
		IsolationLevel:  RepeatableRead,
	}
}

// validate 验证配置
func (c *TransactionConfig) validate() error {
	if c.NodeID == "" {
		c.NodeID = "txn_node_" + cid.Generate()
	}
	if c.Timeout <= 0 {
		return fmt.Errorf("timeout must be positive")
	}
	if c.RetryAttempts < 0 {
		return fmt.Errorf("retry_attempts must be non-negative")
	}
	if c.MaxConcurrent <= 0 {
		return fmt.Errorf("max_concurrent must be positive")
	}
	if c.CleanupInterval <= 0 {
		c.CleanupInterval = 5 * time.Minute
	}
	return nil
}

// initTransactionMetrics 初始化事务指标
func initTransactionMetrics() *TransactionMetrics {
	return &TransactionMetrics{
		TransactionsStarted: promauto.NewCounter(prometheus.CounterOpts{
			Name: "transactions_started_total",
			Help: "Total number of transactions started",
		}),
		TransactionsCommitted: promauto.NewCounter(prometheus.CounterOpts{
			Name: "transactions_committed_total",
			Help: "Total number of transactions committed",
		}),
		TransactionsAborted: promauto.NewCounter(prometheus.CounterOpts{
			Name: "transactions_aborted_total",
			Help: "Total number of transactions aborted",
		}),
		TransactionDuration: promauto.NewHistogram(prometheus.HistogramOpts{
			Name:    "transaction_duration_seconds",
			Help:    "Transaction duration in seconds",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 15),
		}),
		PrepareLatency: promauto.NewHistogram(prometheus.HistogramOpts{
			Name:    "transaction_prepare_latency_seconds",
			Help:    "2PC prepare phase latency in seconds",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 15),
		}),
		CommitLatency: promauto.NewHistogram(prometheus.HistogramOpts{
			Name:    "transaction_commit_latency_seconds",
			Help:    "2PC commit phase latency in seconds",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 15),
		}),
		TwoPhaseSuccessRate: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "transaction_2pc_success_rate",
			Help: "Success rate of 2PC transactions",
		}),
		LocksAcquired: promauto.NewCounter(prometheus.CounterOpts{
			Name: "transaction_locks_acquired_total",
			Help: "Total number of locks acquired",
		}),
		LocksReleased: promauto.NewCounter(prometheus.CounterOpts{
			Name: "transaction_locks_released_total",
			Help: "Total number of locks released",
		}),
		LockWaitTime: promauto.NewHistogram(prometheus.HistogramOpts{
			Name:    "transaction_lock_wait_seconds",
			Help:    "Time spent waiting for locks in seconds",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 15),
		}),
		DeadlocksDetected: promauto.NewCounter(prometheus.CounterOpts{
			Name: "transaction_deadlocks_detected_total",
			Help: "Total number of deadlocks detected",
		}),
		SnapshotsCreated: promauto.NewCounter(prometheus.CounterOpts{
			Name: "transaction_snapshots_created_total",
			Help: "Total number of MVCC snapshots created",
		}),
		ConflictsDetected: promauto.NewCounter(prometheus.CounterOpts{
			Name: "transaction_conflicts_detected_total",
			Help: "Total number of transaction conflicts detected",
		}),
		ReadSetSize: promauto.NewHistogram(prometheus.HistogramOpts{
			Name:    "transaction_read_set_size",
			Help:    "Size of transaction read sets",
			Buckets: prometheus.ExponentialBuckets(1, 2, 15),
		}),
		WriteSetSize: promauto.NewHistogram(prometheus.HistogramOpts{
			Name:    "transaction_write_set_size",
			Help:    "Size of transaction write sets",
			Buckets: prometheus.ExponentialBuckets(1, 2, 15),
		}),
	}
}
