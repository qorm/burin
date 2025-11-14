package business

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bytedance/sonic"

	"github.com/qorm/burin/cProtocol"
)

// handleHealth 处理健康检查命令
func (e *Engine) handleHealth(ctx context.Context, req *cProtocol.ProtocolRequest) (*cProtocol.ProtocolResponse, error) {
	e.updateStats()

	health := map[string]interface{}{
		"node_id":   e.nodeID,
		"status":    "healthy",
		"timestamp": time.Now(),
		"uptime":    time.Since(e.stats.StartTime).Seconds(),
		"components": map[string]bool{
			"cache":       e.cacheEngine != nil,
			"consensus":   e.consensusNode != nil,
			"transaction": e.transactionMgr != nil,
		},
		"stats": e.GetStats(),
	}

	data, _ := sonic.Marshal(health)
	return &cProtocol.ProtocolResponse{
		Status: 200,
		Data:   data,
	}, nil
}

// handleCount 处理统计命令
func (e *Engine) handleCount(ctx context.Context, req *cProtocol.ProtocolRequest) (*cProtocol.ProtocolResponse, error) {
	e.updateStats()
	e.logger.Infof("Received COUNT command with data: %s", string(req.Data))

	// 解析请求
	countReq, err := parseCountRequest(req.Data)
	if err != nil {
		e.logger.Errorf("Failed to parse count request: %v", err)
		return &cProtocol.ProtocolResponse{
			Status: 400,
			Error:  "invalid request format: " + err.Error(),
		}, nil
	}

	// 确定数据库：优先使用请求中的database，其次使用连接当前数据库，最后使用default
	database := countReq.Database
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

	// 权限检查：需要读权限
	if req.Connection != nil && e.burinServer != nil {
		if !e.burinServer.HasPermission(req.Connection, database, "read") {
			e.logger.Warnf("Permission denied for COUNT: user=%s, database=%s",
				req.Meta["username"], database)
			errorResp := map[string]interface{}{
				"error":  fmt.Sprintf("permission denied: no read permission on database '%s'", database),
				"status": "forbidden",
			}
			respData, _ := sonic.Marshal(errorResp)
			return &cProtocol.ProtocolResponse{
				Status: 403,
				Data:   respData,
				Error:  fmt.Sprintf("permission denied: no read permission on database '%s'", database),
			}, nil
		}
	}

	e.logger.Infof("Processing COUNT for database=%s, prefix=%s", database, countReq.Prefix)

	// 统计键数量
	count, err := e.cacheEngine.CountKeysWithDatabase(database, countReq.Prefix)
	if err != nil {
		e.logger.Errorf("Failed to count keys in database=%s with prefix=%s: %v", database, countReq.Prefix, err)
		e.stats.mu.Lock()
		e.stats.ErrorCount++
		e.stats.mu.Unlock()

		return &cProtocol.ProtocolResponse{
			Status: 500,
			Error:  "internal server error: " + err.Error(),
		}, nil
	}

	e.logger.Infof("COUNT result: database=%s, prefix=%s, count=%d", database, countReq.Prefix, count)

	// 构建响应
	response := &CountResponse{
		Database: database,
		Prefix:   countReq.Prefix,
		Count:    count,
		Status:   "success",
	}

	responseData, err := sonic.Marshal(response)
	if err != nil {
		return &cProtocol.ProtocolResponse{
			Status: 500,
			Error:  "failed to marshal response: " + err.Error(),
		}, nil
	}

	e.logger.Debugf("Count keys in database=%s with prefix=%s: %d", database, countReq.Prefix, count)

	return &cProtocol.ProtocolResponse{
		Status: 200,
		Data:   responseData,
	}, nil
}

// handleList 处理列表命令
func (e *Engine) handleList(ctx context.Context, req *cProtocol.ProtocolRequest) (*cProtocol.ProtocolResponse, error) {
	e.updateStats()

	// 解析请求
	listReq, err := parseListRequest(req.Data)
	if err != nil {
		return &cProtocol.ProtocolResponse{
			Status: 400,
			Error:  "invalid request format: " + err.Error(),
		}, nil
	}

	// 确定数据库：优先使用请求中的database，其次使用连接当前数据库，最后使用default
	database := listReq.Database
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
	if database == "" {
		database = "default"
	}

	// 权限检查：需要读权限
	if req.Connection != nil && e.burinServer != nil {
		if !e.burinServer.HasPermission(req.Connection, database, "read") {
			e.logger.Warnf("Permission denied for LIST: user=%s, database=%s",
				req.Meta["username"], database)
			errorResp := map[string]interface{}{
				"error":  fmt.Sprintf("permission denied: no read permission on database '%s'", database),
				"status": "forbidden",
			}
			respData, _ := sonic.Marshal(errorResp)
			return &cProtocol.ProtocolResponse{
				Status: 403,
				Data:   respData,
				Error:  fmt.Sprintf("permission denied: no read permission on database '%s'", database),
			}, nil
		}
	}

	// 设置默认分页参数
	offset := listReq.Offset
	if offset < 0 {
		offset = 0
	}

	limit := listReq.Limit
	if limit == 0 {
		limit = -1 // 默认返回所有
	}

	// 列出键
	keys, total, err := e.cacheEngine.ListKeysWithDatabase(database, listReq.Prefix, offset, limit)
	if err != nil {
		e.logger.Errorf("Failed to list keys in database=%s with prefix=%s: %v", database, listReq.Prefix, err)
		e.stats.mu.Lock()
		e.stats.ErrorCount++
		e.stats.mu.Unlock()

		return &cProtocol.ProtocolResponse{
			Status: 500,
			Error:  "internal server error: " + err.Error(),
		}, nil
	}

	// 判断是否还有更多数据
	hasMore := false
	if limit > 0 && int64(offset+limit) < total {
		hasMore = true
	}

	// 构建响应
	response := &ListResponse{
		Database: database,
		Prefix:   listReq.Prefix,
		Keys:     keys,
		Total:    total,
		Offset:   offset,
		Limit:    limit,
		HasMore:  hasMore,
		Status:   "success",
	}

	responseData, err := sonic.Marshal(response)
	if err != nil {
		return &cProtocol.ProtocolResponse{
			Status: 500,
			Error:  "failed to marshal response: " + err.Error(),
		}, nil
	}

	e.logger.Debugf("List keys in database=%s with prefix=%s: found %d keys (total: %d, offset: %d, limit: %d)",
		database, listReq.Prefix, len(keys), total, offset, limit)

	return &cProtocol.ProtocolResponse{
		Status: 200,
		Data:   responseData,
	}, nil
}

// ============================================
// 数据库创建命令处理器
// ============================================

// handleCreateCacheDB 处理创建 Cache 类型数据库命令
func (e *Engine) handleCreateCacheDB(ctx context.Context, req *cProtocol.ProtocolRequest) (*cProtocol.ProtocolResponse, error) {
	// 解析请求数据
	var createReq struct {
		Database string `json:"database"`
	}

	if err := sonic.Unmarshal(req.Data, &createReq); err != nil {
		return &cProtocol.ProtocolResponse{
			Status: 400,
			Error:  fmt.Sprintf("Failed to parse request: %v", err),
		}, nil
	}

	if createReq.Database == "" {
		return &cProtocol.ProtocolResponse{
			Status: 400,
			Error:  "database name is required",
		}, nil
	}

	// 禁止创建系统保留数据库
	if strings.HasPrefix(createReq.Database, "__burin_") {
		e.logger.Warnf("Attempt to create system reserved database '%s'", createReq.Database)
		return &cProtocol.ProtocolResponse{
			Status: 403,
			Error:  fmt.Sprintf("database name '__burin_*' is reserved for system use and cannot be created manually"),
		}, nil
	}

	// 检查数据库是否已存在
	if e.cacheEngine.DatabaseExists(createReq.Database) {
		return &cProtocol.ProtocolResponse{
			Status: 409,
			Error:  fmt.Sprintf("Database '%s' already exists", createReq.Database),
		}, nil
	}

	// 通过Raft协议将创建数据库操作同步到集群（包括本节点）
	if e.consensusNode != nil && e.consensusNode.IsLeader() {
		e.logger.Infof("Starting Raft replication for create cache database: %s", createReq.Database)

		// 创建Raft命令
		raftCommand := map[string]interface{}{
			"operation": "create_database",
			"database":  createReq.Database,
			"data_type": "cache",
			"timestamp": time.Now().UnixNano(),
		}

		commandData, err := sonic.Marshal(raftCommand)
		if err != nil {
			return &cProtocol.ProtocolResponse{
				Status: 500,
				Error:  fmt.Sprintf("Failed to marshal Raft command: %v", err),
			}, nil
		}

		// 通过Raft Apply命令到集群（包括本节点）
		err = e.consensusNode.ApplyCommand(commandData)
		if err != nil {
			e.logger.Errorf("Raft replication failed for create database %s: %v", createReq.Database, err)
			return &cProtocol.ProtocolResponse{
				Status: 500,
				Error:  fmt.Sprintf("Failed to replicate create database via Raft: %v", err),
			}, nil
		}

		e.logger.Infof("Create database replicated via Raft successfully: %s", createReq.Database)
	} else {
		// 非 Leader 节点不应该处理写操作
		return &cProtocol.ProtocolResponse{
			Status: 503,
			Error:  "Not leader, cannot create database",
		}, nil
	}

	// 自动切换到新创建的数据库
	if req.Connection != nil && e.burinServer != nil {
		e.burinServer.SetConnectionCurrentDB(req.Connection, createReq.Database)
		e.logger.Infof("Automatically switched to newly created database: %s", createReq.Database)
	}

	response := map[string]interface{}{
		"message":  "Cache database created successfully",
		"database": createReq.Database,
		"type":     "cache",
		"switched": true,
	}

	responseData, _ := sonic.Marshal(response)
	return &cProtocol.ProtocolResponse{
		Status: 200,
		Data:   responseData,
	}, nil
}
