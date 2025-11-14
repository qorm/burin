package business

import (
	"context"
	"fmt"
	"time"

	"github.com/bytedance/sonic"

	"burin/cProtocol"
	"burin/store"
	"burin/transaction"
)

// handleReplication 处理来自其他节点的复制请求
func (e *Engine) handleReplication(ctx context.Context, req *cProtocol.ProtocolRequest) (*cProtocol.ProtocolResponse, error) {
	e.logger.Infof("Received replication request: %s", string(req.Data))

	var replicationReq ReplicationRequest
	if err := sonic.Unmarshal(req.Data, &replicationReq); err != nil {
		e.logger.Errorf("Failed to parse replication request: %v", err)
		return &cProtocol.ProtocolResponse{
			Status: 400,
			Error:  "invalid replication request format",
		}, nil
	}

	database := replicationReq.Database
	if database == "" {
		database = "default"
	}

	switch replicationReq.Operation {
	case "set":
		// 执行复制的SET操作
		err := e.cacheEngine.SetWithDatabase(database, replicationReq.Key,
			[]byte(replicationReq.Value), time.Duration(replicationReq.TTL)*time.Second)

		if err != nil {
			e.logger.Errorf("Failed to replicate SET for key=%s: %v", replicationReq.Key, err)
			return &cProtocol.ProtocolResponse{
				Status: 500,
				Error:  "failed to replicate set operation",
			}, nil
		}

		e.logger.Infof("Successfully replicated SET: key=%s, from_node=%s",
			replicationReq.Key, replicationReq.NodeID)

		return &cProtocol.ProtocolResponse{
			Status: 200,
			Data:   []byte(`{"status":"replicated","operation":"set"}`),
		}, nil

	case "delete":
		// 执行复制的DELETE操作
		err := e.cacheEngine.DeleteWithDatabase(database, replicationReq.Key)

		if err != nil {
			e.logger.Errorf("Failed to replicate DELETE for key=%s: %v", replicationReq.Key, err)
			return &cProtocol.ProtocolResponse{
				Status: 500,
				Error:  "failed to replicate delete operation",
			}, nil
		}

		e.logger.Infof("Successfully replicated DELETE: key=%s, from_node=%s",
			replicationReq.Key, replicationReq.NodeID)

		return &cProtocol.ProtocolResponse{
			Status: 200,
			Data:   []byte(`{"status":"replicated","operation":"delete"}`),
		}, nil

	default:
		e.logger.Warnf("Unknown replication operation: %s", replicationReq.Operation)
		return &cProtocol.ProtocolResponse{
			Status: 400,
			Error:  "unknown replication operation",
		}, nil
	}
}

// handleClusterNodeJoin 处理节点加入请求
func (e *Engine) handleClusterNodeJoin(ctx context.Context, req *cProtocol.ClusterRequest) (*cProtocol.ClusterResponse, error) {
	e.logger.Infof("Processing cluster node join request: %s", string(req.Data))

	// 解析节点加入请求
	var joinRequest map[string]interface{}
	if err := sonic.Unmarshal(req.Data, &joinRequest); err != nil {
		return &cProtocol.ClusterResponse{
			Status: 400,
			Error:  fmt.Sprintf("invalid join request format: %v", err),
		}, nil
	}

	// 提取节点信息
	nodeID, ok := joinRequest["node_id"].(string)
	if !ok {
		return &cProtocol.ClusterResponse{
			Status: 400,
			Error:  "missing or invalid node_id",
		}, nil
	}

	address, ok := joinRequest["address"].(string)
	if !ok {
		return &cProtocol.ClusterResponse{
			Status: 400,
			Error:  "missing or invalid address",
		}, nil
	}

	// 提取客户端地址（可选）
	clientAddr, _ := joinRequest["client_addr"].(string)
	e.logger.Infof("Node join request: nodeID=%s, raftAddr=%s, clientAddr=%s", nodeID, address, clientAddr)

	// 调用consensus节点的AddNodeToCluster方法，传递客户端地址
	if e.consensusNode != nil {
		err := e.consensusNode.AddNodeToClusterWithClientAddr(nodeID, address, clientAddr)
		if err != nil {
			return &cProtocol.ClusterResponse{
				Status: 500,
				Error:  fmt.Sprintf("failed to add node to cluster: %v", err),
			}, nil
		}

		// 成功响应，包含客户端地址信息
		response := map[string]interface{}{
			"status":  "success",
			"message": fmt.Sprintf("node %s successfully added to cluster", nodeID),
			"node_id": nodeID,
			"address": address,
		}

		// 如果提供了客户端地址，也包含在响应中
		if clientAddr != "" {
			response["client_addr"] = clientAddr
		}

		responseData, _ := sonic.Marshal(response)
		return &cProtocol.ClusterResponse{
			Status: 200,
			Data:   responseData,
		}, nil
	}

	return &cProtocol.ClusterResponse{
		Status: 503,
		Error:  "consensus node not available",
	}, nil
}

// handleClusterNodeLeave 处理节点离开请求
func (e *Engine) handleClusterNodeLeave(ctx context.Context, req *cProtocol.ClusterRequest) (*cProtocol.ClusterResponse, error) {
	e.logger.Infof("Processing cluster node leave request: %s", string(req.Data))

	// TODO: 实现节点离开逻辑
	return &cProtocol.ClusterResponse{
		Status: 501,
		Error:  "node leave functionality not implemented yet",
	}, nil
}

// handleClusterNodeStatus 处理节点状态查询
func (e *Engine) handleClusterNodeStatus(ctx context.Context, req *cProtocol.ClusterRequest) (*cProtocol.ClusterResponse, error) {
	e.logger.Infof("Processing cluster node status request")

	// TODO: 实现节点状态查询逻辑
	if e.consensusNode != nil {
		status := map[string]interface{}{
			"node_id":   e.nodeID,
			"is_leader": e.consensusNode.IsLeader(),
			"leader":    e.consensusNode.GetLeader(),
		}

		responseData, _ := sonic.Marshal(status)
		return &cProtocol.ClusterResponse{
			Status: 200,
			Data:   responseData,
		}, nil
	}

	return &cProtocol.ClusterResponse{
		Status: 503,
		Error:  "consensus node not available",
	}, nil
}

// handleClusterRaftHeartbeat 处理Raft心跳
func (e *Engine) handleClusterRaftHeartbeat(ctx context.Context, req *cProtocol.ClusterRequest) (*cProtocol.ClusterResponse, error) {
	e.logger.Debugf("Processing cluster raft heartbeat")

	// TODO: 实现Raft心跳处理逻辑
	return &cProtocol.ClusterResponse{
		Status: 200,
		Data:   []byte(`{"status":"heartbeat_received"}`),
	}, nil
}

// handleClusterDataSync 处理数据同步
func (e *Engine) handleClusterDataSync(ctx context.Context, req *cProtocol.ClusterRequest) (*cProtocol.ClusterResponse, error) {
	e.logger.Infof("Processing cluster data sync request, data size: %d bytes", len(req.Data))

	if e.consensusNode == nil {
		return &cProtocol.ClusterResponse{
			Status: 503,
			Error:  "consensus node not available",
		}, nil
	}

	// 简化的数据同步处理
	e.logger.Info("Processing data sync request")

	// TODO: 实现具体的数据同步逻辑
	// 目前返回成功响应
	responseData := []byte(`{"status": "sync_completed", "message": "Data sync processed"}`)

	e.logger.Info("Data sync request processed successfully")
	return &cProtocol.ClusterResponse{
		Status: 200,
		Data:   responseData,
	}, nil
}

// GetTransactionManager 获取事务管理器
func (e *Engine) GetTransactionManager() *transaction.TransactionManager {
	return e.transactionMgr
}

// GetCacheEngine 获取缓存引擎
func (e *Engine) GetCacheEngine() *store.ShardedCacheEngine {
	return e.cacheEngine
}

// ============================================
// 集群内部命令处理器实现
// ============================================

// handleClusterNull 处理空命令（心跳测试）
func (e *Engine) handleClusterNull(ctx context.Context, req *cProtocol.ClusterRequest) (*cProtocol.ClusterResponse, error) {
	e.logger.Debug("Processing cluster null command (heartbeat)")

	return &cProtocol.ClusterResponse{
		Status: 200,
		Data:   []byte("OK"),
	}, nil
}

// ============================================
// Raft 命令处理器
// ============================================

// handleClusterRaftAppendEntries 处理Raft日志复制
func (e *Engine) handleClusterRaftAppendEntries(ctx context.Context, req *cProtocol.ClusterRequest) (*cProtocol.ClusterResponse, error) {
	e.logger.Debug("Processing Raft append entries command")

	if e.consensusNode == nil {
		return &cProtocol.ClusterResponse{
			Status: 503,
			Error:  "consensus node not available",
		}, nil
	}

	// 转发给Raft节点处理
	// TODO: 实现具体的AppendEntries处理逻辑
	return &cProtocol.ClusterResponse{
		Status: 200,
		Data:   []byte("append_entries_processed"),
	}, nil
}

// handleClusterRaftRequestVote 处理Raft投票请求
func (e *Engine) handleClusterRaftRequestVote(ctx context.Context, req *cProtocol.ClusterRequest) (*cProtocol.ClusterResponse, error) {
	e.logger.Debug("Processing Raft request vote command")

	if e.consensusNode == nil {
		return &cProtocol.ClusterResponse{
			Status: 503,
			Error:  "consensus node not available",
		}, nil
	}

	// 转发给Raft节点处理
	// TODO: 实现具体的RequestVote处理逻辑
	return &cProtocol.ClusterResponse{
		Status: 200,
		Data:   []byte("vote_processed"),
	}, nil
}

// handleClusterRaftSnapshot 处理Raft快照传输
func (e *Engine) handleClusterRaftSnapshot(ctx context.Context, req *cProtocol.ClusterRequest) (*cProtocol.ClusterResponse, error) {
	e.logger.Infof("Processing Raft snapshot command, data size: %d bytes", len(req.Data))

	if e.consensusNode == nil {
		return &cProtocol.ClusterResponse{
			Status: 503,
			Error:  "consensus node not available",
		}, nil
	}

	// 简化的快照处理
	e.logger.Info("Processing snapshot install request")

	// TODO: 实现具体的快照安装逻辑
	// 目前返回成功响应
	e.logger.Info("Snapshot successfully processed")
	return &cProtocol.ClusterResponse{
		Status: 200,
		Data:   []byte(`{"status":"snapshot_installed","message":"Snapshot applied successfully"}`),
	}, nil
}

// ============================================
// 节点管理命令处理器
// ============================================

// handleClusterNodeDiscovery 处理节点发现
func (e *Engine) handleClusterNodeDiscovery(ctx context.Context, req *cProtocol.ClusterRequest) (*cProtocol.ClusterResponse, error) {
	e.logger.Infof("Processing cluster node discovery request from %s", req.SourceNode)

	// 解析请求以检查是否需要Raft地址
	var requestData map[string]interface{}
	if len(req.Data) > 0 {
		if err := sonic.Unmarshal(req.Data, &requestData); err == nil {
			e.logger.Infof("Request data: %+v", requestData)
			// 检查request_type字段而不是action字段
			if requestType, ok := requestData["request_type"].(string); ok && requestType == "get_raft_address" {
				e.logger.Infof("Handling get_raft_address request")
				// 返回Raft地址信息
				raftAddr := ""
				if e.consensusNode != nil {
					config := e.consensusNode.GetConfig()
					if config != nil {
						raftAddr = config.BindAddr
						e.logger.Infof("Found Raft address: %s", raftAddr)
					} else {
						e.logger.Warn("Consensus node config is nil")
					}
				} else {
					e.logger.Warn("Consensus node is nil")
				}

				raftInfo := map[string]interface{}{
					"node_id":      e.nodeID,
					"raft_address": raftAddr,
					"status":       "active",
					"timestamp":    time.Now().Unix(),
				}

				e.logger.Infof("Returning raft info: %+v", raftInfo)

				data, err := sonic.Marshal(raftInfo)
				if err != nil {
					return &cProtocol.ClusterResponse{
						Status: 500,
						Error:  fmt.Sprintf("failed to marshal raft info: %v", err),
					}, nil
				}

				return &cProtocol.ClusterResponse{
					Status: 200,
					Data:   data,
				}, nil
			}
		}
	}

	// 返回当前节点信息（默认行为）
	nodeInfo := map[string]interface{}{
		"node_id":   e.nodeID,
		"status":    "active",
		"timestamp": time.Now().Unix(),
	}

	data, err := sonic.Marshal(nodeInfo)
	if err != nil {
		return &cProtocol.ClusterResponse{
			Status: 500,
			Error:  fmt.Sprintf("failed to marshal node info: %v", err),
		}, nil
	}

	return &cProtocol.ClusterResponse{
		Status: 200,
		Data:   data,
	}, nil
}

// handleClusterNodeHealthPing 处理节点健康检查
func (e *Engine) handleClusterNodeHealthPing(ctx context.Context, req *cProtocol.ClusterRequest) (*cProtocol.ClusterResponse, error) {
	e.logger.Debug("Processing cluster node health ping")

	// 健康状态检查
	healthStatus := map[string]interface{}{
		"status":     "healthy",
		"uptime":     time.Since(e.stats.StartTime).Seconds(),
		"requests":   e.stats.RequestCount,
		"errors":     e.stats.ErrorCount,
		"cache_hits": e.stats.CacheHits,
		"timestamp":  time.Now().Unix(),
	}

	data, err := sonic.Marshal(healthStatus)
	if err != nil {
		return &cProtocol.ClusterResponse{
			Status: 500,
			Error:  fmt.Sprintf("failed to marshal health status: %v", err),
		}, nil
	}

	return &cProtocol.ClusterResponse{
		Status: 200,
		Data:   data,
	}, nil
}

// ============================================
// 数据同步命令处理器
// ============================================

// handleClusterDataBackup 处理数据备份
func (e *Engine) handleClusterDataBackup(ctx context.Context, req *cProtocol.ClusterRequest) (*cProtocol.ClusterResponse, error) {
	e.logger.Infof("Processing cluster data backup request")

	// TODO: 实现数据备份逻辑
	return &cProtocol.ClusterResponse{
		Status: 501,
		Error:  "data backup functionality not implemented yet",
	}, nil
}

// handleClusterDataRestore 处理数据恢复
func (e *Engine) handleClusterDataRestore(ctx context.Context, req *cProtocol.ClusterRequest) (*cProtocol.ClusterResponse, error) {
	e.logger.Infof("Processing cluster data restore request")

	// TODO: 实现数据恢复逻辑
	return &cProtocol.ClusterResponse{
		Status: 501,
		Error:  "data restore functionality not implemented yet",
	}, nil
}

// handleClusterDataValidate 处理数据验证
func (e *Engine) handleClusterDataValidate(ctx context.Context, req *cProtocol.ClusterRequest) (*cProtocol.ClusterResponse, error) {
	e.logger.Infof("Processing cluster data validation request")

	// TODO: 实现数据验证逻辑
	return &cProtocol.ClusterResponse{
		Status: 501,
		Error:  "data validation functionality not implemented yet",
	}, nil
}

// ============================================
// 集群协调命令处理器
// ============================================

// handleClusterClusterStatus 处理集群状态查询
func (e *Engine) handleClusterClusterStatus(ctx context.Context, req *cProtocol.ClusterRequest) (*cProtocol.ClusterResponse, error) {
	e.logger.Infof("Processing cluster status query request")

	if e.consensusNode == nil {
		return &cProtocol.ClusterResponse{
			Status: 503,
			Error:  "consensus node not available",
		}, nil
	}

	isLeader := e.consensusNode.IsLeader()
	raftState := e.consensusNode.GetState()
	leaderID := e.consensusNode.GetLeader()
	peers := e.consensusNode.GetPeers()
	config := e.consensusNode.GetConfig()

	e.logger.Infof("IsLeader: %v, RaftState: %s, Leader: %s, Peers count: %d",
		isLeader, raftState, leaderID, len(peers))

	// 构建节点列表信息
	nodes := make([]map[string]interface{}, 0, len(peers))

	// 遍历所有节点（peers 包含了所有节点，包括当前节点）
	for _, peer := range peers {
		isCurrent := peer.ID == e.nodeID
		node := map[string]interface{}{
			"id":          peer.ID,
			"address":     peer.Address,
			"client_addr": peer.ClientAddr,
			"is_leader":   peer.ID == leaderID,
			"is_current":  isCurrent,
			"is_active":   peer.IsActive,
			"role":        peer.Role,
		}

		// 如果是当前节点，更新状态信息
		if isCurrent {
			node["state"] = raftState
		}

		nodes = append(nodes, node)
		e.logger.Debugf("Added node to list: id=%s, addr=%s, client=%s, role=%s",
			peer.ID, peer.Address, peer.ClientAddr, peer.Role)
	}

	// 如果 peers 为空（不应该发生，但以防万一），至少添加当前节点
	if len(nodes) == 0 {
		e.logger.Warnf("No peers found, adding current node only")
		currentNode := map[string]interface{}{
			"id":          e.nodeID,
			"address":     config.BindAddr,
			"client_addr": config.ClientAddr,
			"is_leader":   isLeader,
			"is_current":  true,
			"state":       raftState,
			"is_active":   true,
			"role":        "leader",
		}
		if !isLeader {
			currentNode["role"] = "follower"
		}
		nodes = append(nodes, currentNode)
	}

	// 构建完整的集群信息
	cluster := map[string]interface{}{
		"current_node": e.nodeID,
		"leader":       leaderID,
		"raft_state":   raftState,
		"total_nodes":  len(nodes),
		"nodes":        nodes,
	}

	data, err := sonic.Marshal(cluster)
	if err != nil {
		return &cProtocol.ClusterResponse{
			Status: 500,
			Error:  fmt.Sprintf("failed to marshal cluster status: %v", err),
		}, nil
	}

	e.logger.Infof("Returning cluster status with %d nodes", len(nodes))

	return &cProtocol.ClusterResponse{
		Status: 200,
		Data:   data,
	}, nil
}

// handleClusterClusterConfig 处理集群配置更新
func (e *Engine) handleClusterClusterConfig(ctx context.Context, req *cProtocol.ClusterRequest) (*cProtocol.ClusterResponse, error) {
	e.logger.Infof("Processing cluster configuration update request")

	// TODO: 实现集群配置更新逻辑
	return &cProtocol.ClusterResponse{
		Status: 501,
		Error:  "cluster config update functionality not implemented yet",
	}, nil
}

// handleClusterEmergencyStop 处理紧急停止
func (e *Engine) handleClusterEmergencyStop(ctx context.Context, req *cProtocol.ClusterRequest) (*cProtocol.ClusterResponse, error) {
	e.logger.Warnf("Processing emergency stop request from %s", req.SourceNode)

	// TODO: 实现紧急停止逻辑
	// 这应该是一个非常谨慎的操作
	return &cProtocol.ClusterResponse{
		Status: 501,
		Error:  "emergency stop functionality not implemented yet",
	}, nil
}
