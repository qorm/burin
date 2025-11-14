package consensus

import (
	"fmt"
	"sync"
	"time"

	"github.com/hashicorp/raft"
	"github.com/sirupsen/logrus"
)

// HealthChecker 健康检查器
type HealthChecker struct {
	node   *Node
	logger *logrus.Logger

	mu            sync.RWMutex
	lastCheck     time.Time
	isHealthy     bool
	healthStatus  *HealthStatus
	checkInterval time.Duration
	stopCh        chan struct{}
}

// HealthStatus 健康状态
type HealthStatus struct {
	IsHealthy     bool              `json:"is_healthy"`
	LeaderID      string            `json:"leader_id,omitempty"`
	State         string            `json:"state"`
	LastContact   time.Duration     `json:"last_contact"`
	NumPeers      int               `json:"num_peers"`
	AppliedIndex  uint64            `json:"applied_index"`
	CommitIndex   uint64            `json:"commit_index"`
	LastLogIndex  uint64            `json:"last_log_index"`
	LastLogTerm   uint64            `json:"last_log_term"`
	Checks        map[string]string `json:"checks"`
	LastCheckTime time.Time         `json:"last_check_time"`
	CheckDuration time.Duration     `json:"check_duration"`
}

// NewHealthChecker 创建健康检查器
func NewHealthChecker(node *Node, checkInterval time.Duration) *HealthChecker {
	return &HealthChecker{
		node:          node,
		logger:        node.logger,
		checkInterval: checkInterval,
		stopCh:        make(chan struct{}),
		healthStatus:  &HealthStatus{},
	}
}

// Start 启动健康检查
func (hc *HealthChecker) Start() {
	go hc.run()
}

// Stop 停止健康检查
func (hc *HealthChecker) Stop() {
	close(hc.stopCh)
}

// run 运行健康检查循环
func (hc *HealthChecker) run() {
	ticker := time.NewTicker(hc.checkInterval)
	defer ticker.Stop()

	// 立即执行一次检查
	hc.performCheck()

	for {
		select {
		case <-hc.stopCh:
			return
		case <-ticker.C:
			hc.performCheck()
		}
	}
}

// performCheck 执行健康检查
func (hc *HealthChecker) performCheck() {
	start := time.Now()
	status := &HealthStatus{
		Checks:        make(map[string]string),
		LastCheckTime: start,
	}

	// 1. 检查Raft状态
	if hc.node.raft == nil {
		status.IsHealthy = false
		status.Checks["raft"] = "raft not initialized"
	} else {
		state := hc.node.raft.State()
		status.State = state.String()
		status.Checks["raft"] = "ok"

		// 获取leader信息
		_, leaderID := hc.node.raft.LeaderWithID()
		if leaderID != "" {
			status.LeaderID = string(leaderID)
			status.Checks["leader"] = "ok"
		} else {
			status.Checks["leader"] = "no leader"
		}

		// 检查最后联系时间
		if state != raft.Leader {
			lastContact := hc.node.raft.LastContact()
			status.LastContact = time.Since(lastContact)

			if status.LastContact > 5*time.Second {
				status.Checks["contact"] = fmt.Sprintf("stale: %v", status.LastContact)
			} else {
				status.Checks["contact"] = "ok"
			}
		}

		// 获取日志信息
		status.AppliedIndex = hc.node.raft.AppliedIndex()
		// CommitIndex不是公开API，从Stats获取
		stats := hc.node.raft.Stats()
		if commitIdx, ok := stats["commit_index"]; ok {
			fmt.Sscanf(commitIdx, "%d", &status.CommitIndex)
		}
		status.LastLogIndex = hc.node.raft.LastIndex()

		lastLog := hc.node.raft.LastIndex()
		if lastLog > 0 {
			// 无法直接获取term，使用配置信息
			stats := hc.node.raft.Stats()
			if term, ok := stats["last_log_term"]; ok {
				fmt.Sscanf(term, "%d", &status.LastLogTerm)
			}
		}

		// 2. 检查对等节点数量
		future := hc.node.raft.GetConfiguration()
		if err := future.Error(); err != nil {
			status.Checks["peers"] = fmt.Sprintf("error: %v", err)
		} else {
			config := future.Configuration()
			status.NumPeers = len(config.Servers) - 1 // 排除自己
			if status.NumPeers == 0 && !hc.node.config.Bootstrap {
				status.Checks["peers"] = "no peers"
			} else {
				status.Checks["peers"] = "ok"
			}
		}

		// 3. 检查日志应用延迟
		if status.CommitIndex > 0 && status.AppliedIndex > 0 {
			lag := status.CommitIndex - status.AppliedIndex
			if lag > 1000 {
				status.Checks["apply_lag"] = fmt.Sprintf("high lag: %d", lag)
			} else {
				status.Checks["apply_lag"] = "ok"
			}
		}

		// 4. 检查FSM状态
		if hc.node.fsm != nil {
			status.Checks["fsm"] = "ok"
		} else {
			status.Checks["fsm"] = "not initialized"
		}
	}

	// 综合判断健康状态
	status.IsHealthy = hc.evaluateHealth(status)
	status.CheckDuration = time.Since(start)

	hc.mu.Lock()
	hc.lastCheck = time.Now()
	hc.isHealthy = status.IsHealthy
	hc.healthStatus = status
	hc.mu.Unlock()

	if !status.IsHealthy {
		hc.logger.Warnf("Health check failed: %+v", status.Checks)
	}
}

// evaluateHealth 评估健康状态
func (hc *HealthChecker) evaluateHealth(status *HealthStatus) bool {
	// 必须有Raft实例
	if status.Checks["raft"] != "ok" {
		return false
	}

	// 必须有FSM
	if status.Checks["fsm"] != "ok" {
		return false
	}

	// 非leader节点必须有leader联系
	if status.State != "Leader" {
		if status.Checks["leader"] != "ok" {
			return false
		}
		if status.Checks["contact"] != "ok" {
			return false
		}
	}

	// 应用延迟不能太高
	if status.Checks["apply_lag"] != "" && status.Checks["apply_lag"] != "ok" {
		return false
	}

	return true
}

// GetStatus 获取健康状态
func (hc *HealthChecker) GetStatus() *HealthStatus {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	// 返回副本
	statusCopy := *hc.healthStatus
	statusCopy.Checks = make(map[string]string)
	for k, v := range hc.healthStatus.Checks {
		statusCopy.Checks[k] = v
	}

	return &statusCopy
}

// IsHealthy 是否健康
func (hc *HealthChecker) IsHealthy() bool {
	hc.mu.RLock()
	defer hc.mu.RUnlock()
	return hc.isHealthy
}

// WaitForHealthy 等待健康状态
func (hc *HealthChecker) WaitForHealthy(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		if hc.IsHealthy() {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("timeout waiting for healthy status")
}

// ErrorRecovery 错误恢复管理器
type ErrorRecovery struct {
	node   *Node
	logger *logrus.Logger

	mu               sync.RWMutex
	recoveryAttempts map[string]*RecoveryAttempt
	maxAttempts      int
	backoffDuration  time.Duration
}

// RecoveryAttempt 恢复尝试
type RecoveryAttempt struct {
	ErrorType     string
	AttemptCount  int
	LastAttempt   time.Time
	LastError     error
	RecoveryFunc  func() error
	NextAttemptAt time.Time
}

// NewErrorRecovery 创建错误恢复管理器
func NewErrorRecovery(node *Node, maxAttempts int, backoffDuration time.Duration) *ErrorRecovery {
	return &ErrorRecovery{
		node:             node,
		logger:           node.logger,
		recoveryAttempts: make(map[string]*RecoveryAttempt),
		maxAttempts:      maxAttempts,
		backoffDuration:  backoffDuration,
	}
}

// RegisterRecovery 注册恢复函数
func (er *ErrorRecovery) RegisterRecovery(errorType string, recoveryFunc func() error) {
	er.mu.Lock()
	defer er.mu.Unlock()

	er.recoveryAttempts[errorType] = &RecoveryAttempt{
		ErrorType:    errorType,
		RecoveryFunc: recoveryFunc,
	}
}

// AttemptRecovery 尝试恢复
func (er *ErrorRecovery) AttemptRecovery(errorType string, err error) error {
	er.mu.Lock()
	attempt, exists := er.recoveryAttempts[errorType]
	if !exists {
		er.mu.Unlock()
		return fmt.Errorf("no recovery function registered for error type: %s", errorType)
	}

	// 检查是否需要等待
	if time.Now().Before(attempt.NextAttemptAt) {
		er.mu.Unlock()
		return fmt.Errorf("recovery backoff in progress, next attempt at: %v", attempt.NextAttemptAt)
	}

	// 检查最大尝试次数
	if attempt.AttemptCount >= er.maxAttempts {
		er.mu.Unlock()
		return fmt.Errorf("max recovery attempts reached for %s", errorType)
	}

	attempt.AttemptCount++
	attempt.LastAttempt = time.Now()
	attempt.LastError = err
	recoveryFunc := attempt.RecoveryFunc
	er.mu.Unlock()

	// 执行恢复
	er.logger.Infof("Attempting recovery for %s (attempt %d/%d)", errorType, attempt.AttemptCount, er.maxAttempts)

	recoveryErr := recoveryFunc()

	er.mu.Lock()
	if recoveryErr == nil {
		// 恢复成功，重置计数
		attempt.AttemptCount = 0
		er.logger.Infof("Recovery successful for %s", errorType)
	} else {
		// 恢复失败，计算下次尝试时间（指数退避）
		backoff := time.Duration(attempt.AttemptCount) * er.backoffDuration
		attempt.NextAttemptAt = time.Now().Add(backoff)
		er.logger.Warnf("Recovery failed for %s: %v (next attempt in %v)", errorType, recoveryErr, backoff)
	}
	er.mu.Unlock()

	return recoveryErr
}

// ResetAttempts 重置尝试次数
func (er *ErrorRecovery) ResetAttempts(errorType string) {
	er.mu.Lock()
	defer er.mu.Unlock()

	if attempt, exists := er.recoveryAttempts[errorType]; exists {
		attempt.AttemptCount = 0
		attempt.NextAttemptAt = time.Time{}
	}
}

// GetAttemptInfo 获取尝试信息
func (er *ErrorRecovery) GetAttemptInfo(errorType string) *RecoveryAttempt {
	er.mu.RLock()
	defer er.mu.RUnlock()

	if attempt, exists := er.recoveryAttempts[errorType]; exists {
		// 返回副本
		attemptCopy := *attempt
		return &attemptCopy
	}

	return nil
}
