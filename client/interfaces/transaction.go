package interfaces

import (
	"time"
)

// TransactionInterface 事务接口
type TransactionInterface interface {
	// Begin 开始一个新事务
	Begin(opts ...TransactionOption) (Transaction, error)
}

// Transaction 表示一个活跃的事务
type Transaction interface {
	// ID 获取事务ID
	ID() string

	// Get 在事务中读取数据
	Get(key string) ([]byte, error)

	// Set 在事务中写入数据
	Set(key string, value []byte) error

	// Delete 在事务中删除数据
	Delete(key string) error

	// Commit 提交事务
	Commit() error

	// Rollback 回滚事务
	Rollback() error

	// Status 获取事务状态
	Status() TransactionStatus
}

// TransactionStatus 事务状态
type TransactionStatus string

const (
	TxStatusPending   TransactionStatus = "pending"
	TxStatusPreparing TransactionStatus = "preparing"
	TxStatusPrepared  TransactionStatus = "prepared"
	TxStatusCommitted TransactionStatus = "committed"
	TxStatusAborted   TransactionStatus = "aborted"
)

// TransactionOptions 事务选项
type TransactionOptions struct {
	Timeout        time.Duration  // 事务超时时间
	IsolationLevel IsolationLevel // 隔离级别
	ReadOnly       bool           // 是否只读事务
	Participants   []string       // 参与节点（分布式事务）
	Database       string         // 数据库名称
}

// IsolationLevel 隔离级别
type IsolationLevel string

const (
	ReadCommitted  IsolationLevel = "read_committed"  // 读已提交
	RepeatableRead IsolationLevel = "repeatable_read" // 可重复读
	Serializable   IsolationLevel = "serializable"    // 串行化
)

// TransactionOption 事务选项函数
type TransactionOption func(*TransactionOptions)

// WithTimeout 设置事务超时
func WithTxTimeout(timeout time.Duration) TransactionOption {
	return func(opts *TransactionOptions) {
		opts.Timeout = timeout
	}
}

// WithIsolationLevel 设置隔离级别
func WithIsolationLevel(level IsolationLevel) TransactionOption {
	return func(opts *TransactionOptions) {
		opts.IsolationLevel = level
	}
}

// WithReadOnly 设置为只读事务
func WithReadOnly() TransactionOption {
	return func(opts *TransactionOptions) {
		opts.ReadOnly = true
	}
}

// WithParticipants 设置参与节点（分布式事务）
func WithParticipants(participants []string) TransactionOption {
	return func(opts *TransactionOptions) {
		opts.Participants = participants
	}
}

// WithTxDatabase 设置事务使用的数据库
func WithTxDatabase(database string) TransactionOption {
	return func(opts *TransactionOptions) {
		opts.Database = database
	}
}
