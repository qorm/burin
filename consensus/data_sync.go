package consensus

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"net"
	"sync"
	"time"

	"burin/cid"

	"github.com/bytedance/sonic"

	"github.com/hashicorp/raft"
	"github.com/sirupsen/logrus"
)

// DataSyncStrategy 数据同步策略
type DataSyncStrategy int

const (
	// SyncStrategySnapshot 使用快照同步
	SyncStrategySnapshot DataSyncStrategy = iota
	// SyncStrategyIncremental 增量同步
	SyncStrategyIncremental
	// SyncStrategyHybrid 混合同步（小数据增量，大数据快照）
	SyncStrategyHybrid
)

// SyncStatus 同步状态
type SyncStatus int

const (
	SyncStatusIdle SyncStatus = iota
	SyncStatusInProgress
	SyncStatusCompleted
	SyncStatusFailed
)

// DataSyncManager 数据同步管理器
type DataSyncManager struct {
	node     *Node
	logger   *logrus.Logger
	mu       sync.RWMutex
	strategy DataSyncStrategy

	// 同步状态跟踪
	activeSyncs map[string]*SyncSession
	syncHistory []*SyncRecord

	// 配置参数
	maxConcurrentSyncs int
	syncTimeout        time.Duration
	chunkSize          int

	// 统计信息
	stats struct {
		TotalSyncs      int64
		SuccessfulSyncs int64
		FailedSyncs     int64
		TotalDataSynced int64
		LastSyncTime    time.Time
	}
}

// SyncSession 同步会话
type SyncSession struct {
	SessionID   string           `json:"session_id"`
	TargetNode  string           `json:"target_node"`
	TargetAddr  string           `json:"target_addr"`
	Strategy    DataSyncStrategy `json:"strategy"`
	Status      SyncStatus       `json:"status"`
	StartTime   time.Time        `json:"start_time"`
	EndTime     time.Time        `json:"end_time"`
	Progress    float64          `json:"progress"`
	TotalBytes  int64            `json:"total_bytes"`
	SyncedBytes int64            `json:"synced_bytes"`
	Error       string           `json:"error,omitempty"`
}

// SyncRecord 同步记录
type SyncRecord struct {
	SessionID  string           `json:"session_id"`
	TargetNode string           `json:"target_node"`
	Strategy   DataSyncStrategy `json:"strategy"`
	Status     SyncStatus       `json:"status"`
	StartTime  time.Time        `json:"start_time"`
	EndTime    time.Time        `json:"end_time"`
	Duration   time.Duration    `json:"duration"`
	TotalBytes int64            `json:"total_bytes"`
	Error      string           `json:"error,omitempty"`
}

// SyncRequest 同步请求
type SyncRequest struct {
	RequestID    string                 `json:"request_id"`
	SourceNode   string                 `json:"source_node"`
	Strategy     DataSyncStrategy       `json:"strategy"`
	LastLogIndex uint64                 `json:"last_log_index"`
	Compressed   bool                   `json:"compressed"`
	ChunkSize    int                    `json:"chunk_size"`
	Metadata     map[string]interface{} `json:"metadata"`
}

// SyncResponse 同步响应
type SyncResponse struct {
	RequestID string     `json:"request_id"`
	Status    SyncStatus `json:"status"`
	Message   string     `json:"message"`
	Error     string     `json:"error,omitempty"`
}

// NewDataSyncManager 创建数据同步管理器
func NewDataSyncManager(node *Node) *DataSyncManager {
	return &DataSyncManager{
		node:               node,
		logger:             node.logger,
		strategy:           SyncStrategyHybrid,
		activeSyncs:        make(map[string]*SyncSession),
		syncHistory:        make([]*SyncRecord, 0),
		maxConcurrentSyncs: 3,
		syncTimeout:        30 * time.Minute,
		chunkSize:          1024 * 1024, // 1MB chunks
	}
}

// SyncNewNode 为新节点同步数据
func (dsm *DataSyncManager) SyncNewNode(nodeID, nodeAddr string) error {
	if !dsm.node.IsLeader() {
		return fmt.Errorf("only leader can initiate data sync")
	}

	dsm.mu.Lock()
	defer dsm.mu.Unlock()

	// 检查并发同步限制
	if len(dsm.activeSyncs) >= dsm.maxConcurrentSyncs {
		return fmt.Errorf("maximum concurrent syncs reached: %d", dsm.maxConcurrentSyncs)
	}

	// 创建同步会话 - 使用cid生成唯一会话ID
	sessionID := "sync_" + nodeID + "_" + cid.Generate()
	session := &SyncSession{
		SessionID:  sessionID,
		TargetNode: nodeID,
		TargetAddr: nodeAddr,
		Strategy:   dsm.determineStrategy(),
		Status:     SyncStatusInProgress,
		StartTime:  time.Now(),
	}

	dsm.activeSyncs[sessionID] = session
	dsm.logger.Infof("Starting data sync for new node %s (session: %s, strategy: %v)",
		nodeID, sessionID, session.Strategy)

	// 异步执行同步
	go dsm.executeSyncSession(session)

	return nil
}

// determineStrategy 确定同步策略
func (dsm *DataSyncManager) determineStrategy() DataSyncStrategy {
	// 获取当前数据量
	totalKeys := dsm.getTotalKeyCount()

	switch dsm.strategy {
	case SyncStrategyHybrid:
		// 如果数据量较小，使用增量同步；否则使用快照
		if totalKeys < 10000 {
			return SyncStrategyIncremental
		}
		return SyncStrategySnapshot
	default:
		return dsm.strategy
	}
}

// getTotalKeyCount 获取总键数量
func (dsm *DataSyncManager) getTotalKeyCount() int {
	dsm.node.fsm.mu.RLock()
	defer dsm.node.fsm.mu.RUnlock()

	total := 0
	for _, dbStore := range dsm.node.fsm.store {
		total += len(dbStore)
	}
	return total
}

// executeSyncSession 执行同步会话
func (dsm *DataSyncManager) executeSyncSession(session *SyncSession) {
	defer func() {
		dsm.finalizeSyncSession(session)
	}()

	var err error

	switch session.Strategy {
	case SyncStrategySnapshot:
		err = dsm.syncViaSnapshot(session)
	case SyncStrategyIncremental:
		err = dsm.syncViaIncremental(session)
	case SyncStrategyHybrid:
		// 混合策略已在determineStrategy中处理
		err = dsm.syncViaSnapshot(session)
	default:
		err = fmt.Errorf("unknown sync strategy: %v", session.Strategy)
	}

	if err != nil {
		session.Status = SyncStatusFailed
		session.Error = err.Error()
		dsm.logger.Errorf("Sync session %s failed: %v", session.SessionID, err)
		dsm.stats.FailedSyncs++
	} else {
		session.Status = SyncStatusCompleted
		dsm.logger.Infof("Sync session %s completed successfully", session.SessionID)
		dsm.stats.SuccessfulSyncs++
		dsm.stats.TotalDataSynced += session.SyncedBytes
	}

	session.EndTime = time.Now()
	dsm.stats.TotalSyncs++
	dsm.stats.LastSyncTime = time.Now()
}

// syncViaSnapshot 通过快照同步
func (dsm *DataSyncManager) syncViaSnapshot(session *SyncSession) error {
	dsm.logger.Infof("Syncing node %s via snapshot", session.TargetNode)

	// 创建快照
	snapshot, err := dsm.node.snapshotManager.captureState()
	if err != nil {
		return fmt.Errorf("failed to capture snapshot: %w", err)
	}

	// 序列化快照数据
	var buf bytes.Buffer
	if snapshot.compressed {
		gzWriter := gzip.NewWriter(&buf)

		// 使用sonic序列化数据
		data, err := sonic.Marshal(snapshot.data)
		if err != nil {
			return fmt.Errorf("failed to encode snapshot: %w", err)
		}

		if _, err := gzWriter.Write(data); err != nil {
			return fmt.Errorf("failed to write compressed snapshot: %w", err)
		}
		gzWriter.Close()
	} else {
		// 使用sonic序列化数据
		data, err := sonic.Marshal(snapshot.data)
		if err != nil {
			return fmt.Errorf("failed to encode snapshot: %w", err)
		}
		buf.Write(data)
	}

	snapshotData := buf.Bytes()
	session.TotalBytes = int64(len(snapshotData))

	// 发送快照到目标节点
	return dsm.sendSnapshotToNode(session, snapshotData)
}

// syncViaIncremental 通过增量同步
func (dsm *DataSyncManager) syncViaIncremental(session *SyncSession) error {
	dsm.logger.Infof("Syncing node %s via incremental updates", session.TargetNode)

	// 获取目标节点的最后日志索引
	lastLogIndex, err := dsm.getNodeLastLogIndex(session.TargetAddr)
	if err != nil {
		return fmt.Errorf("failed to get node last log index: %w", err)
	}

	// 获取需要同步的日志条目
	logs, err := dsm.getLogsSince(lastLogIndex)
	if err != nil {
		return fmt.Errorf("failed to get logs since %d: %w", lastLogIndex, err)
	}

	// 分批发送日志条目
	return dsm.sendIncrementalUpdates(session, logs)
}

// sendSnapshotToNode 向节点发送快照
func (dsm *DataSyncManager) sendSnapshotToNode(session *SyncSession, data []byte) error {
	// 直接使用配置中的协议地址
	protocolAddr := ""
	if dsm.node != nil && dsm.node.config != nil && dsm.node.config.ProtocolAddr != "" {
		protocolAddr = dsm.node.config.ProtocolAddr
	} else {
		dsm.logger.Warnf("No protocol address configured, using target address %s", session.TargetAddr)
		protocolAddr = session.TargetAddr
	}

	conn, err := net.Dial("tcp", protocolAddr)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", protocolAddr, err)
	}
	defer conn.Close()

	// 发送同步请求头
	syncRequest := SyncRequest{
		RequestID:  session.SessionID,
		SourceNode: dsm.node.config.NodeID,
		Strategy:   SyncStrategySnapshot,
		Compressed: true,
		ChunkSize:  dsm.chunkSize,
	}

	requestData, err := sonic.Marshal(syncRequest)
	if err != nil {
		return fmt.Errorf("failed to marshal sync request: %w", err)
	}

	// 简化的数据同步：直接发送JSON数据而不使用协议头
	dsm.logger.Debugf("Sending sync data: %d bytes", len(data))

	// 发送同步请求（简化版本）
	if _, err := conn.Write(requestData); err != nil {
		return fmt.Errorf("failed to send sync request: %w", err)
	}

	// 分块发送快照数据
	return dsm.sendDataInChunks(conn, data, session)
}

// sendDataInChunks 分块发送数据
func (dsm *DataSyncManager) sendDataInChunks(conn net.Conn, data []byte, session *SyncSession) error {
	totalSize := len(data)
	sent := 0

	for sent < totalSize {
		chunkSize := dsm.chunkSize
		if sent+chunkSize > totalSize {
			chunkSize = totalSize - sent
		}

		chunk := data[sent : sent+chunkSize]
		n, err := conn.Write(chunk)
		if err != nil {
			return fmt.Errorf("failed to send chunk: %w", err)
		}

		sent += n
		session.SyncedBytes = int64(sent)
		session.Progress = float64(sent) / float64(totalSize)

		dsm.logger.Debugf("Sent chunk %d/%d bytes (%.2f%%)",
			sent, totalSize, session.Progress*100)
	}

	// 读取确认响应
	response := make([]byte, 1024)
	respLen, err := conn.Read(response)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	var syncResp SyncResponse
	if err := sonic.Unmarshal(response[:respLen], &syncResp); err != nil {
		dsm.logger.Warnf("Failed to parse sync response, assuming success: %v", err)
		return nil
	}

	if syncResp.Status != SyncStatusCompleted {
		return fmt.Errorf("sync failed on target node: %s", syncResp.Error)
	}

	return nil
}

// getNodeLastLogIndex 获取节点的最后日志索引
func (dsm *DataSyncManager) getNodeLastLogIndex(nodeAddr string) (uint64, error) {
	// 这里简化实现，实际应该通过协议查询目标节点
	return 0, nil
}

// getLogsSince 获取指定索引之后的日志条目
func (dsm *DataSyncManager) getLogsSince(lastLogIndex uint64) ([]raft.Log, error) {
	// 这里简化实现，实际应该从Raft日志存储中获取
	return nil, nil
}

// sendIncrementalUpdates 发送增量更新
func (dsm *DataSyncManager) sendIncrementalUpdates(session *SyncSession, logs []raft.Log) error {
	// 简化实现
	dsm.logger.Infof("Sending %d incremental updates to %s", len(logs), session.TargetNode)
	return nil
}

// finalizeSyncSession 完成同步会话
func (dsm *DataSyncManager) finalizeSyncSession(session *SyncSession) {
	dsm.mu.Lock()
	defer dsm.mu.Unlock()

	// 从活跃同步中移除
	delete(dsm.activeSyncs, session.SessionID)

	// 添加到历史记录
	record := &SyncRecord{
		SessionID:  session.SessionID,
		TargetNode: session.TargetNode,
		Strategy:   session.Strategy,
		Status:     session.Status,
		StartTime:  session.StartTime,
		EndTime:    session.EndTime,
		Duration:   session.EndTime.Sub(session.StartTime),
		TotalBytes: session.TotalBytes,
		Error:      session.Error,
	}

	dsm.syncHistory = append(dsm.syncHistory, record)

	// 保持历史记录数量限制
	if len(dsm.syncHistory) > 100 {
		dsm.syncHistory = dsm.syncHistory[1:]
	}
}

// GetSyncStatus 获取同步状态
func (dsm *DataSyncManager) GetSyncStatus() map[string]interface{} {
	dsm.mu.RLock()
	defer dsm.mu.RUnlock()

	activeSessions := make([]SyncSession, 0, len(dsm.activeSyncs))
	for _, session := range dsm.activeSyncs {
		activeSessions = append(activeSessions, *session)
	}

	recentHistory := dsm.syncHistory
	if len(recentHistory) > 10 {
		recentHistory = recentHistory[len(recentHistory)-10:]
	}

	return map[string]interface{}{
		"strategy":       dsm.strategy,
		"active_syncs":   activeSessions,
		"recent_history": recentHistory,
		"max_concurrent": dsm.maxConcurrentSyncs,
		"sync_timeout":   dsm.syncTimeout.String(),
		"chunk_size":     dsm.chunkSize,
		"stats": map[string]interface{}{
			"total_syncs":       dsm.stats.TotalSyncs,
			"successful_syncs":  dsm.stats.SuccessfulSyncs,
			"failed_syncs":      dsm.stats.FailedSyncs,
			"total_data_synced": dsm.stats.TotalDataSynced,
			"last_sync_time":    dsm.stats.LastSyncTime,
		},
	}
}

// HandleSyncRequest 处理同步请求
func (dsm *DataSyncManager) HandleSyncRequest(requestData []byte) ([]byte, error) {
	var request SyncRequest
	if err := sonic.Unmarshal(requestData, &request); err != nil {
		return nil, fmt.Errorf("failed to unmarshal sync request: %w", err)
	}

	dsm.logger.Infof("Handling sync request %s from node %s (strategy: %v)",
		request.RequestID, request.SourceNode, request.Strategy)

	response := SyncResponse{
		RequestID: request.RequestID,
		Status:    SyncStatusCompleted,
		Message:   "Sync completed successfully",
	}

	// 这里应该实际处理同步数据
	// 简化实现，直接返回成功

	return sonic.Marshal(response)
}

// SetStrategy 设置同步策略
func (dsm *DataSyncManager) SetStrategy(strategy DataSyncStrategy) {
	dsm.mu.Lock()
	defer dsm.mu.Unlock()
	dsm.strategy = strategy
	dsm.logger.Infof("Data sync strategy changed to: %v", strategy)
}

// GetActiveSync 获取活跃同步会话
func (dsm *DataSyncManager) GetActiveSync(sessionID string) (*SyncSession, bool) {
	dsm.mu.RLock()
	defer dsm.mu.RUnlock()
	session, exists := dsm.activeSyncs[sessionID]
	return session, exists
}
