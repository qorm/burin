package business

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bytedance/sonic"

	"github.com/qorm/burin/cProtocol"
)

// ============================================
// 缓存操作处理器
// ============================================

// isSystemReservedDatabase 检查是否是系统保留数据库
// 系统保留数据库以 __burin_ 开头，不允许通过普通缓存命令访问
func isSystemReservedDatabase(database string) bool {
	return strings.HasPrefix(database, "__burin_")
}

// handleGet 处理获取命令
func (e *Engine) handleGet(ctx context.Context, req *cProtocol.ProtocolRequest) (*cProtocol.ProtocolResponse, error) {
	e.updateStats()

	// 解析请求
	cacheReq, err := parseCacheRequest(req.Data)
	if err != nil {
		return &cProtocol.ProtocolResponse{
			Status: 400,
			Error:  "invalid request format: " + err.Error(),
		}, nil
	}

	// 确定数据库
	database := e.determineCacheDatabase(cacheReq, req)

	// 禁止普通命令操作系统保留数据库
	if isSystemReservedDatabase(database) {
		e.logger.Warnf("Attempt to access reserved database '%s' via cache GET command", database)
		return &cProtocol.ProtocolResponse{
			Status: 403,
			Error:  fmt.Sprintf("database '%s' is a system reserved database and cannot be accessed via cache commands. Use appropriate API commands instead.", database),
		}, nil
	}

	// 验证key
	if strings.TrimSpace(cacheReq.Key) == "" {
		e.logger.Warnf("handleGet: Empty key provided")
		return &cProtocol.ProtocolResponse{
			Status: 400,
			Error:  "key cannot be empty",
		}, nil
	}

	// 从共识状态机读取数据
	e.logger.Infof("Processing GET - reading from consensus FSM (database=%s, key=%s)", database, cacheReq.Key)
	value, found, err := e.getFromConsensusStore(database, cacheReq.Key)

	// 构建响应
	cacheResp := buildCacheResponse(cacheReq.Key, value, found, err)
	respData, _ := sonic.Marshal(cacheResp)

	if err != nil {
		e.logger.Errorf("Failed to get key=%s (db=%s): %v", cacheReq.Key, database, err)
		return &cProtocol.ProtocolResponse{
			Status: 500,
			Data:   respData,
			Error:  "internal server error",
		}, nil
	}

	if found {
		e.logger.Debugf("Get key=%s (db=%s): found", cacheReq.Key, database)
		return &cProtocol.ProtocolResponse{
			Status: 200,
			Data:   respData,
		}, nil
	}

	e.logger.Debugf("Get key=%s (db=%s): not found", cacheReq.Key, database)
	return &cProtocol.ProtocolResponse{
		Status: 404,
		Data:   respData,
	}, nil
}

// handleSet 处理设置命令
func (e *Engine) handleSet(ctx context.Context, req *cProtocol.ProtocolRequest) (*cProtocol.ProtocolResponse, error) {
	e.logger.Infof("handleSet called with data: %s", string(req.Data))
	e.updateStats()

	// 检查是否是转发的请求
	var reqData map[string]interface{}
	if err := sonic.Unmarshal(req.Data, &reqData); err == nil {
		if forwarded, ok := reqData["_forwarded"].(bool); ok && forwarded {
			e.logger.Infof("Received forwarded SET request from node: %v", reqData["_from_node"])
		}
	}

	// 解析请求
	cacheReq, err := parseCacheRequest(req.Data)
	if err != nil {
		e.logger.Errorf("Failed to parse cache request: %v", err)
		return &cProtocol.ProtocolResponse{
			Status: 400,
			Error:  "invalid request format: " + err.Error(),
		}, nil
	}

	// 确定数据库
	database := e.determineCacheDatabase(cacheReq, req)

	// 禁止普通命令操作系统保留数据库
	if isSystemReservedDatabase(database) {
		e.logger.Warnf("Attempt to access system reserved database '%s' via cache SET command", database)
		return &cProtocol.ProtocolResponse{
			Status: 403,
			Error:  fmt.Sprintf("database '%s' is a system reserved database and cannot be accessed via cache commands. Use appropriate API commands instead.", database),
		}, nil
	}

	// 检查数据库是否存在
	if !e.cacheEngine.DatabaseExists(database) {
		e.logger.Warnf("Database %s does not exist for SET operation", database)
		return &cProtocol.ProtocolResponse{
			Status: 404,
			Error:  fmt.Sprintf("database '%s' does not exist", database),
		}, nil
	}

	// 验证数据库类型
	dbType, err := e.cacheEngine.GetDatabaseType(database)
	if err != nil {
		e.logger.Errorf("Failed to get database type for %s: %v", database, err)
		return &cProtocol.ProtocolResponse{
			Status: 500,
			Error:  fmt.Sprintf("failed to get database type: %v", err),
		}, nil
	}

	if dbType != "cache" {
		e.logger.Warnf("Database %s is type %s, cannot be used for cache operations", database, dbType)
		return &cProtocol.ProtocolResponse{
			Status: 400,
			Error:  fmt.Sprintf("database '%s' is type %s, cannot be used for cache operations", database, dbType),
		}, nil
	}

	// 权限检查
	if req.Connection != nil && e.burinServer != nil {
		if !e.burinServer.HasPermission(req.Connection, database, "write") {
			e.logger.Warnf("Permission denied for SET: user=%s, database=%s", req.Meta["username"], database)
			return &cProtocol.ProtocolResponse{
				Status: 403,
				Error:  fmt.Sprintf("permission denied: no write permission on database '%s'", database),
			}, nil
		}
	}

	// 非 leader 且 forward_leader=false 时禁止写入
	if !cacheReq.IsReplicated && !e.isForwardedRequest(req.Data) {
		if e.consensusNode != nil && !e.consensusNode.IsLeader() {
			if e.burinConfig != nil && !e.burinConfig.ForwardLeader {
				e.logger.Warnf("SET denied: node is not leader and forward_leader is false (key=%s)", cacheReq.Key)
				return &cProtocol.ProtocolResponse{
					Status: 403,
					Error:  "write denied: only leader node can accept writes when forward_leader is false",
				}, nil
			}
		}
	}

	// 检查是否是leader节点，如果不是且未被转发，并且 forward_leader 配置为 true，则转发到leader
	if !cacheReq.IsReplicated && !e.isForwardedRequest(req.Data) {
		if e.consensusNode != nil && !e.consensusNode.IsLeader() && e.burinConfig != nil && e.burinConfig.ForwardLeader {
			e.logger.Infof("Forwarding SET operation to leader for key: %s", cacheReq.Key)
			return e.forwardToLeader(ctx, req)
		}
	}

	// 验证key和value
	if strings.TrimSpace(cacheReq.Key) == "" {
		return &cProtocol.ProtocolResponse{
			Status: 400,
			Error:  "key cannot be empty",
		}, nil
	}

	if cacheReq.Value == "" {
		return &cProtocol.ProtocolResponse{
			Status: 400,
			Error:  "value cannot be empty",
		}, nil
	}

	// 一致性优先模式：所有数据库都通过 Raft 先同步，再由 FSM 回调写入本地
	if !cacheReq.IsReplicated {
		e.logger.Infof("Consistency-first mode: using Raft for key=%s in database=%s", cacheReq.Key, database)

		if e.consensusNode == nil || !e.consensusNode.IsLeader() {
			return &cProtocol.ProtocolResponse{
				Status: 500,
				Error:  "consensus node not available or not leader",
			}, nil
		}

		// 通过Raft提交命令
		raftCommand := map[string]interface{}{
			"operation": "set",
			"database":  database,
			"key":       cacheReq.Key,
			"value":     cacheReq.Value,
			"ttl":       int64(cacheReq.TTL.Duration().Seconds()),
			"timestamp": time.Now().UnixNano(),
		}

		commandData, err := sonic.Marshal(raftCommand)
		if err != nil {
			e.logger.Errorf("Failed to marshal Raft command for key=%s: %v", cacheReq.Key, err)
			return &cProtocol.ProtocolResponse{
				Status: 500,
				Error:  "failed to marshal command: " + err.Error(),
			}, nil
		}

		err = e.consensusNode.ApplyCommand(commandData)
		if err != nil {
			e.logger.Errorf("Raft replication failed for key=%s: %v", cacheReq.Key, err)
			return &cProtocol.ProtocolResponse{
				Status: 500,
				Error:  "failed to replicate via raft: " + err.Error(),
			}, nil
		}

		e.logger.Infof("Data replicated via Raft successfully for key=%s in database=%s", cacheReq.Key, database)
	} else {
		// 这是从其他节点复制过来的请求，直接写本地
		e.logger.Infof("Processing replicated SET - storing in cache engine (database=%s, key=%s)", database, cacheReq.Key)
		err = e.cacheEngine.SetWithDatabase(database, cacheReq.Key, []byte(cacheReq.Value), cacheReq.TTL.Duration())

		if err != nil {
			e.logger.Errorf("Failed to set replicated key=%s (db=%s): %v", cacheReq.Key, database, err)
			return &cProtocol.ProtocolResponse{
				Status: 500,
				Error:  "failed to store replicated data: " + err.Error(),
			}, nil
		}

		e.logger.Infof("Successfully set replicated key=%s (db=%s) in cache engine", cacheReq.Key, database)
	}

	// 构建响应
	response := map[string]interface{}{
		"key":    cacheReq.Key,
		"status": "success",
	}

	respData, _ := sonic.Marshal(response)
	return &cProtocol.ProtocolResponse{
		Status: 200,
		Data:   respData,
	}, nil
}

// handleDelete 处理删除命令
func (e *Engine) handleDelete(ctx context.Context, req *cProtocol.ProtocolRequest) (*cProtocol.ProtocolResponse, error) {
	e.updateStats()

	// 解析请求
	cacheReq, err := parseCacheRequest(req.Data)
	if err != nil {
		return &cProtocol.ProtocolResponse{
			Status: 400,
			Error:  "invalid request format: " + err.Error(),
		}, nil
	}

	database := e.determineCacheDatabase(cacheReq, req)

	// 禁止普通命令操作系统保留数据库
	if isSystemReservedDatabase(database) {
		e.logger.Warnf("Attempt to access system reserved database '%s' via cache DELETE command", database)
		return &cProtocol.ProtocolResponse{
			Status: 403,
			Error:  fmt.Sprintf("database '%s' is a system reserved database and cannot be accessed via cache commands. Use appropriate API commands instead.", database),
		}, nil
	}

	// 验证key
	if strings.TrimSpace(cacheReq.Key) == "" {
		return &cProtocol.ProtocolResponse{
			Status: 400,
			Error:  "key cannot be empty",
		}, nil
	}

	// 权限检查
	if req.Connection != nil && e.burinServer != nil {
		if !e.burinServer.HasPermission(req.Connection, database, "write") {
			return &cProtocol.ProtocolResponse{
				Status: 403,
				Error:  fmt.Sprintf("permission denied: no write permission on database '%s'", database),
			}, nil
		}
	}

	// 非 leader 且 forward_leader=false 时禁止删除
	if !cacheReq.IsReplicated && !e.isForwardedRequest(req.Data) {
		if e.consensusNode != nil && !e.consensusNode.IsLeader() {
			if e.burinConfig != nil && !e.burinConfig.ForwardLeader {
				e.logger.Warnf("DELETE denied: node is not leader and forward_leader is false (key=%s)", cacheReq.Key)
				return &cProtocol.ProtocolResponse{
					Status: 403,
					Error:  "delete denied: only leader node can accept deletes when forward_leader is false",
				}, nil
			}
		}
	}

	// 检查是否是leader节点，如果不是且未被转发，并且 forward_leader 配置为 true，则转发到leader
	if !cacheReq.IsReplicated && !e.isForwardedRequest(req.Data) {
		if e.consensusNode != nil && !e.consensusNode.IsLeader() && e.burinConfig != nil && e.burinConfig.ForwardLeader {
			e.logger.Infof("Forwarding DELETE operation to leader for key: %s", cacheReq.Key)
			return e.forwardToLeader(ctx, req)
		}
	}

	// 一致性优先模式：所有数据库都通过 Raft 先同步删除
	if !cacheReq.IsReplicated {
		e.logger.Infof("Consistency-first mode: using Raft for DELETE key=%s in database=%s", cacheReq.Key, database)

		if e.consensusNode == nil || !e.consensusNode.IsLeader() {
			return &cProtocol.ProtocolResponse{
				Status: 500,
				Error:  "consensus node not available or not leader",
			}, nil
		}

		// 通过Raft提交DELETE命令
		raftCommand := map[string]interface{}{
			"operation": "delete",
			"database":  database,
			"key":       cacheReq.Key,
			"timestamp": time.Now().UnixNano(),
		}

		commandData, err := sonic.Marshal(raftCommand)
		if err != nil {
			e.logger.Errorf("Failed to marshal Raft DELETE command for key=%s: %v", cacheReq.Key, err)
			return &cProtocol.ProtocolResponse{
				Status: 500,
				Error:  "failed to marshal command: " + err.Error(),
			}, nil
		}

		err = e.consensusNode.ApplyCommand(commandData)
		if err != nil {
			e.logger.Errorf("Raft DELETE replication failed for key=%s: %v", cacheReq.Key, err)
			return &cProtocol.ProtocolResponse{
				Status: 500,
				Error:  "failed to replicate via raft: " + err.Error(),
			}, nil
		}

		e.logger.Infof("DELETE operation replicated via Raft successfully for key=%s in database=%s", cacheReq.Key, database)
	} else {
		// 这是从其他节点复制过来的请求，直接删除本地
		e.logger.Infof("Processing replicated DELETE - removing from cache engine (database=%s, key=%s)", database, cacheReq.Key)
		err = e.cacheEngine.DeleteWithDatabase(database, cacheReq.Key)

		if err != nil {
			e.logger.Errorf("Failed to delete replicated key=%s (db=%s): %v", cacheReq.Key, database, err)
			return &cProtocol.ProtocolResponse{
				Status: 500,
				Error:  "failed to delete replicated data: " + err.Error(),
			}, nil
		}

		e.logger.Infof("Successfully deleted replicated key=%s (db=%s) from cache engine", cacheReq.Key, database)
	}

	response := map[string]interface{}{
		"key":    cacheReq.Key,
		"status": "success",
	}

	respData, _ := sonic.Marshal(response)
	return &cProtocol.ProtocolResponse{
		Status: 200,
		Data:   respData,
	}, nil
}

// handleExists 处理存在性检查命令
func (e *Engine) handleExists(ctx context.Context, req *cProtocol.ProtocolRequest) (*cProtocol.ProtocolResponse, error) {
	e.updateStats()

	// 解析请求
	cacheReq, err := parseCacheRequest(req.Data)
	if err != nil {
		return &cProtocol.ProtocolResponse{
			Status: 400,
			Error:  "invalid request format: " + err.Error(),
		}, nil
	}

	database := e.determineCacheDatabase(cacheReq, req)

	// 验证key
	if strings.TrimSpace(cacheReq.Key) == "" {
		return &cProtocol.ProtocolResponse{
			Status: 400,
			Error:  "key cannot be empty",
		}, nil
	}

	// 检查是否存在
	_, found, err := e.cacheEngine.GetWithDatabase(database, cacheReq.Key)

	cacheResp := buildCacheResponse(cacheReq.Key, nil, found, err)

	if err != nil {
		e.logger.Errorf("Failed to check exists key=%s: %v", cacheReq.Key, err)
		respData, _ := sonic.Marshal(cacheResp)
		return &cProtocol.ProtocolResponse{
			Status: 500,
			Data:   respData,
			Error:  "internal server error",
		}, nil
	}

	respData, _ := sonic.Marshal(cacheResp)

	if found {
		return &cProtocol.ProtocolResponse{
			Status: 200,
			Data:   respData,
		}, nil
	}

	return &cProtocol.ProtocolResponse{
		Status: 404,
		Data:   respData,
	}, nil
}

// handlePing 处理ping命令
func (e *Engine) handlePing(ctx context.Context, req *cProtocol.ProtocolRequest) (*cProtocol.ProtocolResponse, error) {
	e.updateStats()

	return &cProtocol.ProtocolResponse{
		Status: 200,
		Data:   []byte(`{"status":"pong"}`),
	}, nil
}

// ============================================
// 辅助方法
// ============================================

// determineCacheDatabase 确定缓存操作使用的数据库
func (e *Engine) determineCacheDatabase(cacheReq *CacheRequest, req *cProtocol.ProtocolRequest) string {
	database := cacheReq.Database
	if database == "" && req.Connection != nil && e.burinServer != nil {
		currentDB := e.burinServer.GetConnectionCurrentDB(req.Connection)
		if currentDB != "" {
			database = currentDB
			e.logger.Debugf("Using connection's current database: %s", database)
		}
	}
	if database == "" {
		database = "default"
	}
	return database
}

// replicateSetViaRaft 通过Raft复制SET操作
func (e *Engine) replicateSetViaRaft(database string, cacheReq *CacheRequest) {
	e.logger.Infof("Starting Raft replication for key=%s to cluster nodes", cacheReq.Key)

	raftCommand := map[string]interface{}{
		"operation": "set",
		"database":  database,
		"key":       cacheReq.Key,
		"value":     cacheReq.Value,
		"ttl":       int64(cacheReq.TTL.Duration().Seconds()),
		"timestamp": time.Now().UnixNano(),
	}

	commandData, err := sonic.Marshal(raftCommand)
	if err != nil {
		e.logger.Errorf("Failed to marshal Raft command for key=%s: %v", cacheReq.Key, err)
		return
	}

	err = e.consensusNode.ApplyCommand(commandData)
	if err != nil {
		e.logger.Warnf("Raft replication failed for key=%s: %v", cacheReq.Key, err)
	} else {
		e.logger.Infof("Data replicated via Raft successfully for key=%s", cacheReq.Key)
	}
}

// replicateDeleteViaRaft 通过Raft复制DELETE操作
func (e *Engine) replicateDeleteViaRaft(database string, cacheReq *CacheRequest) {
	raftCommand := map[string]interface{}{
		"operation": "delete",
		"database":  database,
		"key":       cacheReq.Key,
		"timestamp": time.Now().UnixNano(),
	}

	commandData, err := sonic.Marshal(raftCommand)
	if err != nil {
		e.logger.Errorf("Failed to marshal Raft DELETE command for key=%s: %v", cacheReq.Key, err)
		return
	}

	err = e.consensusNode.ApplyCommand(commandData)
	if err != nil {
		e.logger.Warnf("Raft DELETE replication failed for key=%s: %v", cacheReq.Key, err)
	} else {
		e.logger.Infof("DELETE operation replicated via Raft successfully for key=%s", cacheReq.Key)
	}
}

// getFromConsensusStore 从共识存储中获取数据
func (e *Engine) getFromConsensusStore(database, key string) ([]byte, bool, error) {
	return e.cacheEngine.GetWithDatabase(database, key)
}
