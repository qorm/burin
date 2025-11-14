package business

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/bytedance/sonic"

	"burin/cProtocol"

	"github.com/sirupsen/logrus"
)

// DataReplicator 数据复制器
type DataReplicator struct {
	nodeID   string
	nodes    []NodeInfo
	logger   *logrus.Logger
	mu       sync.RWMutex
	clients  map[string]*cProtocol.Client
	isLeader bool
}

// NodeInfo 节点信息
type NodeInfo struct {
	ID       string `json:"id"`
	Address  string `json:"address"`
	IsLeader bool   `json:"is_leader"`
}

// ReplicationRequest 复制请求
type ReplicationRequest struct {
	Operation string `json:"operation"` // set, delete
	Key       string `json:"key"`
	Value     string `json:"value,omitempty"`
	Database  string `json:"database,omitempty"`
	TTL       int64  `json:"ttl,omitempty"`
	NodeID    string `json:"node_id"`   // 发起节点
	Timestamp int64  `json:"timestamp"` // 时间戳
}

// NewDataReplicator 创建数据复制器
func NewDataReplicator(nodeID string, logger *logrus.Logger) *DataReplicator {
	return &DataReplicator{
		nodeID:  nodeID,
		nodes:   make([]NodeInfo, 0),
		logger:  logger,
		clients: make(map[string]*cProtocol.Client),
	}
}

// AddNode 添加集群节点
func (dr *DataReplicator) AddNode(node NodeInfo) {
	dr.mu.Lock()
	defer dr.mu.Unlock()

	// 检查是否已存在
	for i, existing := range dr.nodes {
		if existing.ID == node.ID {
			dr.nodes[i] = node // 更新
			dr.logger.Infof("Updated node info: %s (%s)", node.ID, node.Address)
			return
		}
	}

	dr.nodes = append(dr.nodes, node)
	dr.logger.Infof("Added node to replication: %s (%s)", node.ID, node.Address)
}

// SetLeaderStatus 设置领导者状态
func (dr *DataReplicator) SetLeaderStatus(isLeader bool) {
	dr.mu.Lock()
	defer dr.mu.Unlock()

	if dr.isLeader != isLeader {
		dr.isLeader = isLeader
		dr.logger.Infof("Node leadership status changed: %v", isLeader)
	}
}

// ReplicateSet 复制SET操作到其他节点
func (dr *DataReplicator) ReplicateSet(ctx context.Context, key, value, database string, ttl time.Duration) error {
	dr.logger.Infof("ReplicateSet called: key=%s", key)

	// 检查状态和获取节点列表（在锁保护下）
	dr.mu.RLock()
	isLeader := dr.isLeader
	nodeCount := len(dr.nodes)
	// 复制节点列表以避免在锁外使用时被修改
	targetNodes := make([]NodeInfo, 0)
	for _, node := range dr.nodes {
		if node.ID != dr.nodeID {
			targetNodes = append(targetNodes, node)
		}
	}
	dr.mu.RUnlock()

	dr.logger.Infof("ReplicateSet status: isLeader=%v, totalNodes=%d, targetNodes=%d", isLeader, nodeCount, len(targetNodes))

	// 只有leader才能发起复制
	if !isLeader {
		dr.logger.Info("Not leader, skipping replication")
		return nil
	}

	if nodeCount <= 1 {
		dr.logger.Infof("Single node cluster (%d nodes), no replication needed", nodeCount)
		return nil
	}

	if len(targetNodes) == 0 {
		dr.logger.Info("No target nodes for replication")
		return nil
	}

	dr.logger.Infof("Starting replication to %d nodes for key=%s", len(targetNodes), key)

	req := ReplicationRequest{
		Operation: "set",
		Key:       key,
		Value:     value,
		Database:  database,
		TTL:       int64(ttl.Seconds()),
		NodeID:    dr.nodeID,
		Timestamp: time.Now().UnixNano(),
	}

	var wg sync.WaitGroup
	errors := make(chan error, len(targetNodes))

	// 并行复制到所有从节点（现在不持有锁）
	for _, node := range targetNodes {
		wg.Add(1)
		go func(target NodeInfo) {
			defer wg.Done()

			err := dr.sendReplication(ctx, target, req)
			if err != nil {
				dr.logger.Errorf("Failed to replicate to node %s: %v", target.ID, err)
				errors <- fmt.Errorf("node %s: %w", target.ID, err)
			} else {
				dr.logger.Infof("Successfully replicated to node %s", target.ID)
			}
		}(node)
	}

	wg.Wait()
	close(errors)

	// 收集错误
	var replicationErrors []error
	for err := range errors {
		replicationErrors = append(replicationErrors, err)
	}

	if len(replicationErrors) > 0 {
		dr.logger.Warnf("Replication completed with %d errors out of %d target nodes",
			len(replicationErrors), len(targetNodes))
		// 返回第一个错误，但不阻塞主操作
		return replicationErrors[0]
	}

	dr.logger.Infof("Replication completed successfully to %d nodes", len(targetNodes))
	return nil
}

// ReplicateDelete 复制DELETE操作到其他节点
func (dr *DataReplicator) ReplicateDelete(ctx context.Context, key, database string) error {
	dr.mu.RLock()
	defer dr.mu.RUnlock()

	if !dr.isLeader || len(dr.nodes) <= 1 {
		return nil
	}

	req := ReplicationRequest{
		Operation: "delete",
		Key:       key,
		Database:  database,
		NodeID:    dr.nodeID,
		Timestamp: time.Now().UnixNano(),
	}

	var wg sync.WaitGroup
	for _, node := range dr.nodes {
		if node.ID == dr.nodeID {
			continue
		}

		wg.Add(1)
		go func(target NodeInfo) {
			defer wg.Done()
			err := dr.sendReplication(ctx, target, req)
			if err != nil {
				dr.logger.Errorf("Failed to replicate delete to node %s: %v", target.ID, err)
			}
		}(node)
	}

	wg.Wait()
	return nil
}

// sendReplication 发送复制请求到目标节点
func (dr *DataReplicator) sendReplication(ctx context.Context, target NodeInfo, req ReplicationRequest) error {
	dr.logger.Infof("sendReplication: target=%s, key=%s", target.Address, req.Key)

	client, err := dr.getOrCreateClient(target.Address)
	if err != nil {
		dr.logger.Errorf("Failed to get client for %s: %v", target.Address, err)
		return fmt.Errorf("failed to get client for %s: %w", target.Address, err)
	}

	// 构造标准的缓存请求格式
	cacheRequest := map[string]interface{}{
		"database":     req.Database,
		"key":          req.Key,
		"value":        req.Value,
		"ttl":          req.TTL,
		"IsReplicated": true, // 标记为复制请求，避免循环复制
	}

	reqData, err := sonic.Marshal(cacheRequest)
	if err != nil {
		dr.logger.Errorf("Failed to marshal replication request: %v", err)
		return fmt.Errorf("failed to marshal replication request: %w", err)
	}

	dr.logger.Infof("Sending replication to %s: db=%s, key=%s, data=%s",
		target.Address, req.Database, req.Key, string(reqData))

	// 根据操作类型选择命令
	var command cProtocol.Command
	switch req.Operation {
	case "set":
		command = cProtocol.CmdSet
	case "delete":
		command = cProtocol.CmdDelete
	default:
		dr.logger.Errorf("Unsupported replication operation: %s", req.Operation)
		return fmt.Errorf("unsupported replication operation: %s", req.Operation)
	}

	// 发送复制请求
	dr.logger.Infof("About to send request to %s with command %d", target.Address, command)

	// 创建正确的协议头
	head := cProtocol.CreateClientCommandHead(command)
	head.SetContentLength(uint32(len(reqData)))

	resp, err := client.SendRequest(ctx, &cProtocol.ProtocolRequest{
		Head: head,
		Data: reqData,
	})

	if err != nil {
		dr.logger.Errorf("Failed to send replication request to %s: %v", target.Address, err)
		return err
	}

	// 检查响应状态码
	if resp.Status >= 400 {
		dr.logger.Errorf("Replication request to %s failed with status %d: %s", target.Address, resp.Status, resp.Error)
		return fmt.Errorf("replication request failed with status %d: %s", resp.Status, resp.Error)
	}

	dr.logger.Infof("Successfully sent replication to %s, response status: %d", target.Address, resp.Status)
	return nil
}

// getOrCreateClient 获取或创建到目标节点的客户端
func (dr *DataReplicator) getOrCreateClient(address string) (*cProtocol.Client, error) {
	dr.mu.Lock()
	defer dr.mu.Unlock()

	dr.logger.Infof("getOrCreateClient: address=%s", address)

	if client, exists := dr.clients[address]; exists {
		dr.logger.Infof("Using existing client for %s", address)
		return client, nil
	}

	dr.logger.Infof("Creating new client for %s", address)

	config := &cProtocol.ClientConfig{
		Endpoint:            address,
		ReadTimeout:         2 * time.Second,
		WriteTimeout:        2 * time.Second,
		DialTimeout:         3 * time.Second,
		HealthCheckInterval: 30 * time.Second,
	}

	client := cProtocol.NewClient(config, dr.logger)
	dr.logger.Infof("Starting client for %s", address)
	err := client.Start()
	if err != nil {
		dr.logger.Errorf("Failed to start client for %s: %v", address, err)
		return nil, err
	}

	dr.clients[address] = client
	dr.logger.Infof("Client created and started successfully for %s", address)
	return client, nil
}

// Close 关闭所有客户端连接
func (dr *DataReplicator) Close() {
	dr.mu.Lock()
	defer dr.mu.Unlock()

	for addr, client := range dr.clients {
		client.Stop()
		dr.logger.Debugf("Closed replication client to %s", addr)
	}
	dr.clients = make(map[string]*cProtocol.Client)
}

// GetNodes 获取节点列表
func (dr *DataReplicator) GetNodes() []NodeInfo {
	dr.mu.RLock()
	defer dr.mu.RUnlock()

	nodes := make([]NodeInfo, len(dr.nodes))
	copy(nodes, dr.nodes)
	return nodes
}

// ClearNodes 清除所有节点
func (dr *DataReplicator) ClearNodes() {
	dr.mu.Lock()
	defer dr.mu.Unlock()

	dr.nodes = make([]NodeInfo, 0)
	dr.logger.Info("Cleared all nodes from replicator")
}
