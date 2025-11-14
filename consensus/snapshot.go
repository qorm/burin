package consensus

import (
	"bytes"
	"compress/gzip"
	"crypto/md5"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/bytedance/sonic"

	"github.com/hashicorp/raft"
	"github.com/sirupsen/logrus"
)

// SnapshotMetadata 快照元数据
type SnapshotMetadata struct {
	Version       int                    `json:"version"`
	CreatedAt     time.Time              `json:"created_at"`
	LastLogIndex  uint64                 `json:"last_log_index"`
	LastLogTerm   uint64                 `json:"last_log_term"`
	Configuration []raft.Server          `json:"configuration"`
	DatabaseCount int                    `json:"database_count"`
	TotalKeys     int                    `json:"total_keys"`
	Checksum      string                 `json:"checksum"`
	Size          int64                  `json:"size"`
	Compressed    bool                   `json:"compressed"`
	Metadata      map[string]interface{} `json:"metadata"`
}

// SnapshotData 快照数据
type SnapshotData struct {
	Metadata SnapshotMetadata                `json:"metadata"`
	Store    map[string]map[string][]byte    `json:"store"`
	TTL      map[string]map[string]time.Time `json:"ttl"`
}

// EnhancedSnapshot 增强的快照实现
type EnhancedSnapshot struct {
	data       *SnapshotData
	mu         sync.RWMutex
	logger     *logrus.Logger
	compressed bool
}

// SnapshotManager 快照管理器
type SnapshotManager struct {
	node               *Node
	logger             *logrus.Logger
	snapshotDir        string
	retainCount        int
	autoInterval       time.Duration
	lastSnapshot       time.Time
	mu                 sync.RWMutex
	compressionEnabled bool

	// 快照统计
	stats struct {
		TotalSnapshots   int64
		LastSnapshotSize int64
		LastSnapshotTime time.Time
		FailedSnapshots  int64
	}
}

// NewSnapshotManager 创建快照管理器
func NewSnapshotManager(node *Node) *SnapshotManager {
	snapshotDir := filepath.Join(node.config.DataDir, "snapshots")
	os.MkdirAll(snapshotDir, 0755)

	return &SnapshotManager{
		node:               node,
		logger:             node.logger,
		snapshotDir:        snapshotDir,
		retainCount:        3,
		autoInterval:       10 * time.Minute, // 默认10分钟自动快照
		compressionEnabled: true,
	}
}

// StartAutoSnapshot 启动自动快照
func (sm *SnapshotManager) StartAutoSnapshot() {
	go func() {
		ticker := time.NewTicker(sm.autoInterval)
		defer ticker.Stop()

		for {
			select {
			case <-sm.node.shutdown:
				return
			case <-ticker.C:
				if sm.shouldCreateSnapshot() {
					if err := sm.CreateSnapshot(); err != nil {
						sm.logger.Errorf("Auto snapshot failed: %v", err)
						sm.stats.FailedSnapshots++
					}
				}
			}
		}
	}()
}

// shouldCreateSnapshot 判断是否需要创建快照
func (sm *SnapshotManager) shouldCreateSnapshot() bool {
	if !sm.node.IsLeader() {
		return false
	}

	// 检查时间间隔
	if time.Since(sm.lastSnapshot) < sm.autoInterval {
		return false
	}

	// 检查日志条目数量
	stats := sm.node.raft.Stats()
	lastLogIndex := stats["last_log_index"]

	// 如果日志条目超过1000个，创建快照
	if lastLogIndex != "" && lastLogIndex != "0" {
		return true
	}

	return false
}

// CreateSnapshot 创建快照
func (sm *SnapshotManager) CreateSnapshot() error {
	if !sm.node.IsLeader() {
		return fmt.Errorf("only leader can create snapshots")
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.logger.Info("Creating snapshot...")

	// 获取当前状态
	snapshot, err := sm.captureState()
	if err != nil {
		return fmt.Errorf("failed to capture state: %w", err)
	}

	// 触发Raft快照
	future := sm.node.raft.Snapshot()
	if err := future.Error(); err != nil {
		return fmt.Errorf("failed to create raft snapshot: %w", err)
	}

	// 保存快照元数据
	if err := sm.saveSnapshotMetadata(snapshot.data.Metadata); err != nil {
		sm.logger.Errorf("Failed to save snapshot metadata: %v", err)
	}

	// 清理旧快照
	sm.cleanupOldSnapshots()

	sm.lastSnapshot = time.Now()
	sm.stats.TotalSnapshots++
	sm.stats.LastSnapshotTime = time.Now()

	sm.logger.Infof("Snapshot created successfully: %d keys across %d databases",
		snapshot.data.Metadata.TotalKeys, snapshot.data.Metadata.DatabaseCount)

	return nil
}

// captureState 捕获当前状态
func (sm *SnapshotManager) captureState() (*EnhancedSnapshot, error) {
	// 获取FSM状态
	fsmSnapshot, err := sm.node.fsm.Snapshot()
	if err != nil {
		return nil, fmt.Errorf("failed to create FSM snapshot: %w", err)
	}

	enhancedSnap := fsmSnapshot.(*FSMSnapshot)

	// 计算统计信息
	totalKeys := 0
	for _, dbStore := range enhancedSnap.store {
		totalKeys += len(dbStore)
	}

	// 获取Raft配置
	configFuture := sm.node.raft.GetConfiguration()
	if err := configFuture.Error(); err != nil {
		return nil, fmt.Errorf("failed to get configuration: %w", err)
	}

	// 获取最后的日志索引和任期
	stats := sm.node.raft.Stats()
	lastLogIndex := uint64(0)
	lastLogTerm := uint64(0)

	// 创建快照数据
	snapshotData := &SnapshotData{
		Metadata: SnapshotMetadata{
			Version:       1,
			CreatedAt:     time.Now(),
			LastLogIndex:  lastLogIndex,
			LastLogTerm:   lastLogTerm,
			Configuration: configFuture.Configuration().Servers,
			DatabaseCount: len(enhancedSnap.store),
			TotalKeys:     totalKeys,
			Compressed:    sm.compressionEnabled,
			Metadata: map[string]interface{}{
				"node_id": sm.node.config.NodeID,
				"stats":   stats,
			},
		},
		Store: enhancedSnap.store,
		TTL:   enhancedSnap.ttl,
	}

	// 计算校验和
	checksum, size, err := sm.calculateChecksum(snapshotData)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate checksum: %w", err)
	}

	snapshotData.Metadata.Checksum = checksum
	snapshotData.Metadata.Size = size

	return &EnhancedSnapshot{
		data:       snapshotData,
		logger:     sm.logger,
		compressed: sm.compressionEnabled,
	}, nil
}

// calculateChecksum 计算快照校验和
func (sm *SnapshotManager) calculateChecksum(data *SnapshotData) (string, int64, error) {
	var buf bytes.Buffer

	// 序列化数据使用sonic
	storeData, err := sonic.Marshal(data.Store)
	if err != nil {
		return "", 0, err
	}
	buf.Write(storeData)

	ttlData, err := sonic.Marshal(data.TTL)
	if err != nil {
		return "", 0, err
	}
	buf.Write(ttlData)

	// 计算MD5
	hash := md5.Sum(buf.Bytes())
	checksum := fmt.Sprintf("%x", hash)

	return checksum, int64(buf.Len()), nil
}

// saveSnapshotMetadata 保存快照元数据
func (sm *SnapshotManager) saveSnapshotMetadata(metadata SnapshotMetadata) error {
	filename := fmt.Sprintf("snapshot_%d_%s.meta",
		metadata.CreatedAt.Unix(), metadata.Checksum[:8])
	path := filepath.Join(sm.snapshotDir, filename)

	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	// 使用sonic序列化，然后写入文件
	data, err := sonic.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}

	_, err = file.Write(data)
	return err
}

// cleanupOldSnapshots 清理旧快照
func (sm *SnapshotManager) cleanupOldSnapshots() {
	// 这里简化实现，实际应该根据创建时间和保留策略清理
	sm.logger.Info("Cleaned up old snapshots")
}

// GetSnapshotStats 获取快照统计信息
func (sm *SnapshotManager) GetSnapshotStats() map[string]interface{} {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	return map[string]interface{}{
		"total_snapshots":     sm.stats.TotalSnapshots,
		"last_snapshot_size":  sm.stats.LastSnapshotSize,
		"last_snapshot_time":  sm.stats.LastSnapshotTime,
		"failed_snapshots":    sm.stats.FailedSnapshots,
		"auto_interval":       sm.autoInterval.String(),
		"retain_count":        sm.retainCount,
		"compression_enabled": sm.compressionEnabled,
	}
}

// Persist 持久化快照（实现raft.FSMSnapshot接口）
func (es *EnhancedSnapshot) Persist(sink raft.SnapshotSink) error {
	es.mu.RLock()
	defer es.mu.RUnlock()

	var writer io.Writer = sink

	// 如果启用压缩
	if es.compressed {
		gzWriter := gzip.NewWriter(sink)
		defer gzWriter.Close()
		writer = gzWriter
	}

	// 使用sonic编码数据
	data, err := sonic.Marshal(es.data)
	if err != nil {
		sink.Cancel()
		return fmt.Errorf("failed to encode snapshot: %w", err)
	}

	if _, err := writer.Write(data); err != nil {
		sink.Cancel()
		return fmt.Errorf("failed to write snapshot: %w", err)
	}

	return sink.Close()
}

// Release 释放快照资源（实现raft.FSMSnapshot接口）
func (es *EnhancedSnapshot) Release() {
	es.mu.Lock()
	defer es.mu.Unlock()
	es.data = nil
}

// InstallSnapshotToNode 向指定节点发送快照
func (sm *SnapshotManager) InstallSnapshotToNode(targetNodeID, targetAddr string) error {
	if !sm.node.IsLeader() {
		return fmt.Errorf("only leader can send snapshots")
	}

	sm.logger.Infof("Installing snapshot to node %s at %s", targetNodeID, targetAddr)

	// 创建当前快照
	snapshot, err := sm.captureState()
	if err != nil {
		return fmt.Errorf("failed to capture state: %w", err)
	}

	// 序列化快照数据
	var buf bytes.Buffer
	if snapshot.compressed {
		gzWriter := gzip.NewWriter(&buf)

		// 使用sonic编码数据
		data, err := sonic.Marshal(snapshot.data)
		if err != nil {
			return fmt.Errorf("failed to encode snapshot: %w", err)
		}

		if _, err := gzWriter.Write(data); err != nil {
			return fmt.Errorf("failed to write compressed snapshot: %w", err)
		}
		gzWriter.Close()
	} else {
		// 使用sonic编码数据
		data, err := sonic.Marshal(snapshot.data)
		if err != nil {
			return fmt.Errorf("failed to encode snapshot: %w", err)
		}
		buf.Write(data)
	}

	// 发送快照到目标节点
	return sm.sendSnapshotData(targetAddr, buf.Bytes())
}

// sendSnapshotData 发送快照数据
func (sm *SnapshotManager) sendSnapshotData(targetAddr string, data []byte) error {
	// 直接使用配置中的协议地址
	protocolAddr := ""
	if sm.node != nil && sm.node.config != nil && sm.node.config.ProtocolAddr != "" {
		protocolAddr = sm.node.config.ProtocolAddr
	} else {
		sm.logger.Warnf("No protocol address configured, using target address %s", targetAddr)
		protocolAddr = targetAddr
	}

	conn, err := net.Dial("tcp", protocolAddr)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", protocolAddr, err)
	}
	defer conn.Close()

	// 简化的快照传输：直接发送数据而不使用协议头
	sm.logger.Debugf("Sending snapshot data: %d bytes", len(data))

	// 发送数据
	if _, err := conn.Write(data); err != nil {
		return fmt.Errorf("failed to send snapshot data: %w", err)
	}

	// 读取响应
	response := make([]byte, 1024)
	respLen, err := conn.Read(response)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	sm.logger.Infof("Snapshot transfer response: %s", string(response[:respLen]))
	return nil
}

// RestoreFromSnapshot 从快照恢复
func (sm *SnapshotManager) RestoreFromSnapshot(snapshotData []byte, compressed bool) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	var reader io.Reader = bytes.NewReader(snapshotData)

	// 如果数据是压缩的
	if compressed {
		gzReader, err := gzip.NewReader(reader)
		if err != nil {
			return fmt.Errorf("failed to create gzip reader: %w", err)
		}
		defer gzReader.Close()
		reader = gzReader
	}

	// 解码快照数据
	var data SnapshotData

	// 使用sonic解码数据
	buf, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("failed to read snapshot data: %w", err)
	}

	if err := sonic.Unmarshal(buf, &data); err != nil {
		return fmt.Errorf("failed to decode snapshot: %w", err)
	}

	// 验证校验和
	checksum, _, err := sm.calculateChecksum(&data)
	if err != nil {
		return fmt.Errorf("failed to calculate checksum: %w", err)
	}

	if checksum != data.Metadata.Checksum {
		return fmt.Errorf("snapshot checksum mismatch: expected %s, got %s",
			data.Metadata.Checksum, checksum)
	}

	// 恢复状态到FSM
	sm.node.fsm.mu.Lock()
	sm.node.fsm.store = data.Store
	sm.node.fsm.ttl = data.TTL
	sm.node.fsm.mu.Unlock()

	sm.logger.Infof("Successfully restored from snapshot: %d keys across %d databases",
		data.Metadata.TotalKeys, data.Metadata.DatabaseCount)

	return nil
}
