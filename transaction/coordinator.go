package transaction

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// CoordinatorImpl 事务协调器实现
type CoordinatorImpl struct {
	tm *TransactionManager
	mu sync.RWMutex
}

// ParticipantImpl 事务参与者实现
type ParticipantImpl struct {
	tm *TransactionManager
	mu sync.RWMutex
}

// NewCoordinator 创建新的协调器
func NewCoordinator(tm *TransactionManager) Coordinator {
	return &CoordinatorImpl{
		tm: tm,
	}
}

// NewParticipant 创建新的参与者
func NewParticipant(tm *TransactionManager) Participant {
	return &ParticipantImpl{
		tm: tm,
	}
}

// CoordinatorImpl 方法实现

// StartTransaction 启动分布式事务
func (c *CoordinatorImpl) StartTransaction(ctx context.Context, txnType TransactionType, participants []string) (*Transaction, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 创建事务
	txn, err := c.tm.BeginTransaction(ctx, txnType)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}

	txn.mu.Lock()
	txn.Participants = participants
	txn.mu.Unlock()

	c.tm.logger.Infof("Started distributed transaction %s with %d participants",
		txn.ID, len(participants))

	return txn, nil
}

// PrepareTransaction 执行 2PC 预提交阶段
func (c *CoordinatorImpl) PrepareTransaction(ctx context.Context, txnID string) error {
	start := time.Now()
	defer func() {
		c.tm.metrics.PrepareLatency.Observe(time.Since(start).Seconds())
	}()

	c.tm.mu.RLock()
	txn, exists := c.tm.transactions[txnID]
	c.tm.mu.RUnlock()

	if !exists {
		return fmt.Errorf("transaction %s not found", txnID)
	}

	txn.mu.Lock()
	defer txn.mu.Unlock()

	if txn.Status != TxnPending {
		return fmt.Errorf("transaction %s is not in pending state", txnID)
	}

	txn.Status = TxnPreparing

	// 并行向所有参与者发送预提交请求
	prepareChan := make(chan PrepareResult, len(txn.Participants))

	for _, participant := range txn.Participants {
		go func(nodeID string) {
			// 这里应该通过网络调用参与者的 Prepare 方法
			// 简化实现，假设本地调用
			result := PrepareResult{
				Success:   true,
				NodeID:    nodeID,
				Version:   time.Now().UnixNano(),
				Timestamp: time.Now(),
				Locks:     make(map[string]LockType),
			}

			// 模拟网络延迟
			time.Sleep(10 * time.Millisecond)

			prepareChan <- result
		}(participant)
	}

	// 收集所有参与者的响应
	allSuccess := true
	for i := 0; i < len(txn.Participants); i++ {
		select {
		case result := <-prepareChan:
			txn.PrepareResults[result.NodeID] = result
			if !result.Success {
				allSuccess = false
			}
		case <-ctx.Done():
			return fmt.Errorf("prepare phase timeout")
		}
	}

	if allSuccess {
		txn.Status = TxnPrepared
		c.tm.logger.Infof("All participants prepared for transaction %s", txnID)
		return nil
	} else {
		txn.Status = TxnAborting
		c.tm.logger.Warnf("Some participants failed to prepare transaction %s", txnID)
		return fmt.Errorf("prepare phase failed")
	}
}

// CommitTransaction 执行 2PC 提交阶段
func (c *CoordinatorImpl) CommitTransaction(ctx context.Context, txnID string) error {
	start := time.Now()
	defer func() {
		c.tm.metrics.CommitLatency.Observe(time.Since(start).Seconds())
	}()

	// 首先执行预提交阶段
	if err := c.PrepareTransaction(ctx, txnID); err != nil {
		c.AbortTransaction(ctx, txnID)
		return fmt.Errorf("prepare failed: %w", err)
	}

	c.tm.mu.RLock()
	txn, exists := c.tm.transactions[txnID]
	c.tm.mu.RUnlock()

	if !exists {
		return fmt.Errorf("transaction %s not found", txnID)
	}

	txn.mu.Lock()
	defer txn.mu.Unlock()

	if txn.Status != TxnPrepared {
		return fmt.Errorf("transaction %s is not prepared", txnID)
	}

	txn.Status = TxnCommitting

	// 并行向所有参与者发送提交请求
	commitChan := make(chan CommitResult, len(txn.Participants))

	for _, participant := range txn.Participants {
		go func(nodeID string) {
			// 这里应该通过网络调用参与者的 Commit 方法
			result := CommitResult{
				Success:   true,
				NodeID:    nodeID,
				Version:   time.Now().UnixNano(),
				Timestamp: time.Now(),
			}

			// 模拟网络延迟
			time.Sleep(5 * time.Millisecond)

			commitChan <- result
		}(participant)
	}

	// 收集所有参与者的响应
	successCount := 0
	for i := 0; i < len(txn.Participants); i++ {
		select {
		case result := <-commitChan:
			txn.CommitResults[result.NodeID] = result
			if result.Success {
				successCount++
			}
		case <-ctx.Done():
			c.tm.logger.Errorf("Commit phase timeout for transaction %s", txnID)
		}
	}

	if successCount == len(txn.Participants) {
		txn.Status = TxnCommitted
		txn.cancel()
		c.tm.releaseLocks(txn)
		c.tm.metrics.TransactionsCommitted.Inc()
		c.tm.logger.Infof("Successfully committed distributed transaction %s", txnID)
		return nil
	} else {
		txn.Status = TxnAborted
		c.tm.logger.Errorf("Failed to commit distributed transaction %s", txnID)
		return fmt.Errorf("commit phase partially failed")
	}
}

// AbortTransaction 回滚分布式事务
func (c *CoordinatorImpl) AbortTransaction(ctx context.Context, txnID string) error {
	c.tm.mu.RLock()
	txn, exists := c.tm.transactions[txnID]
	c.tm.mu.RUnlock()

	if !exists {
		return fmt.Errorf("transaction %s not found", txnID)
	}

	txn.mu.Lock()
	defer txn.mu.Unlock()

	if txn.Status == TxnAborted || txn.Status == TxnCommitted {
		return nil // 已经终止或提交
	}

	txn.Status = TxnAborting

	// 并行通知所有参与者回滚
	abortChan := make(chan bool, len(txn.Participants))

	for _, participant := range txn.Participants {
		go func(nodeID string) {
			// 这里应该通过网络调用参与者的 Abort 方法
			// 简化实现
			time.Sleep(5 * time.Millisecond)
			abortChan <- true
		}(participant)
	}

	// 等待所有参与者确认回滚
	for i := 0; i < len(txn.Participants); i++ {
		select {
		case <-abortChan:
			// 参与者确认回滚
		case <-time.After(5 * time.Second):
			c.tm.logger.Warnf("Timeout waiting for participant to abort transaction %s", txnID)
		}
	}

	txn.Status = TxnAborted
	txn.cancel()
	c.tm.releaseLocks(txn)
	c.tm.metrics.TransactionsAborted.Inc()

	c.tm.logger.Infof("Aborted distributed transaction %s", txnID)
	return nil
}

// ParticipantImpl 方法实现

// Prepare 参与者预提交
func (p *ParticipantImpl) Prepare(ctx context.Context, txnID string, operations []*TransactionOp) (PrepareResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	result := PrepareResult{
		NodeID:    p.tm.config.NodeID,
		Timestamp: time.Now(),
		Locks:     make(map[string]LockType),
	}

	// 检查是否可以获取所需的锁
	for _, op := range operations {
		lockType := SharedLock
		if op.Type == OpSet || op.Type == OpDelete {
			lockType = ExclusiveLock
		}

		// 简化的锁检查逻辑
		if p.canAcquireLock(op.Key, lockType) {
			result.Locks[op.Key] = lockType
			p.tm.metrics.LocksAcquired.Inc()
		} else {
			result.Success = false
			result.Error = fmt.Sprintf("cannot acquire %s lock on key %s", lockType, op.Key)
			p.tm.logger.Warnf("Failed to acquire lock for transaction %s: %s", txnID, result.Error)
			return result, nil
		}
	}

	// 执行冲突检测
	if err := p.checkConflicts(operations); err != nil {
		result.Success = false
		result.Error = fmt.Sprintf("conflict detected: %v", err)
		p.tm.logger.Warnf("Conflict detected for transaction %s: %s", txnID, result.Error)
		return result, nil
	}

	result.Success = true
	result.Version = time.Now().UnixNano()

	p.tm.logger.Infof("Prepared transaction %s on participant %s", txnID, p.tm.config.NodeID)
	return result, nil
}

// Commit 参与者提交
func (p *ParticipantImpl) Commit(ctx context.Context, txnID string) (CommitResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	result := CommitResult{
		NodeID:    p.tm.config.NodeID,
		Timestamp: time.Now(),
	}

	// 查找本地事务
	p.tm.mu.RLock()
	txn, exists := p.tm.transactions[txnID]
	p.tm.mu.RUnlock()

	if !exists {
		result.Success = false
		result.Error = "transaction not found"
		return result, fmt.Errorf("transaction %s not found", txnID)
	}

	// 应用所有写操作
	txn.mu.Lock()
	for _, op := range txn.Operations {
		if op.Type == OpSet || op.Type == OpDelete {
			// 这里应该调用存储引擎的实际写入方法
			p.tm.logger.Debugf("Applying operation %s for key %s in transaction %s",
				op.Type, op.Key, txnID)
		}
	}

	txn.Status = TxnCommitted
	txn.mu.Unlock()

	// 释放锁
	p.tm.releaseLocks(txn)

	result.Success = true
	result.Version = time.Now().UnixNano()

	p.tm.logger.Infof("Committed transaction %s on participant %s", txnID, p.tm.config.NodeID)
	return result, nil
}

// Abort 参与者回滚
func (p *ParticipantImpl) Abort(ctx context.Context, txnID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// 查找本地事务
	p.tm.mu.RLock()
	txn, exists := p.tm.transactions[txnID]
	p.tm.mu.RUnlock()

	if !exists {
		return fmt.Errorf("transaction %s not found", txnID)
	}

	txn.mu.Lock()
	txn.Status = TxnAborted
	txn.cancel()
	txn.mu.Unlock()

	// 释放锁
	p.tm.releaseLocks(txn)

	p.tm.logger.Infof("Aborted transaction %s on participant %s", txnID, p.tm.config.NodeID)
	return nil
}

// 辅助方法

// canAcquireLock 检查是否可以获取锁
func (p *ParticipantImpl) canAcquireLock(key string, lockType LockType) bool {
	// 简化的锁检查逻辑
	// 实际实现需要维护一个全局锁表
	return true
}

// checkConflicts 检查操作冲突
func (p *ParticipantImpl) checkConflicts(operations []*TransactionOp) error {
	// 简化的冲突检测
	// 实际实现需要检查 MVCC 版本冲突
	return nil
}
