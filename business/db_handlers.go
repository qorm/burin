package business

import (
	"context"
	"fmt"
	"strings"
	"time"

	"burin/cProtocol"

	"github.com/bytedance/sonic"
)

// ============================================
// 数据库管理命令处理器
// ============================================

// handleUseDB 处理切换当前数据库命令
func (e *Engine) handleUseDB(ctx context.Context, req *cProtocol.ProtocolRequest) (*cProtocol.ProtocolResponse, error) {
	// 解析请求数据
	var useReq struct {
		Database string `json:"database"`
	}

	if err := sonic.Unmarshal(req.Data, &useReq); err != nil {
		return &cProtocol.ProtocolResponse{
			Status: 400,
			Error:  fmt.Sprintf("Failed to parse request: %v", err),
		}, nil
	}

	if useReq.Database == "" {
		return &cProtocol.ProtocolResponse{
			Status: 400,
			Error:  "database name is required",
		}, nil
	}

	// 禁止切换到系统保留数据库
	if strings.HasPrefix(useReq.Database, "__burin_") {
		e.logger.Warnf("Attempt to switch to system reserved database '%s'", useReq.Database)
		return &cProtocol.ProtocolResponse{
			Status: 403,
			Error:  fmt.Sprintf("database '__burin_*' is reserved for system use and cannot be used directly"),
		}, nil
	}

	// 检查数据库是否存在
	exists := e.cacheEngine.DatabaseExists(useReq.Database)
	if !exists {
		return &cProtocol.ProtocolResponse{
			Status: 404,
			Error:  fmt.Sprintf("Database '%s' does not exist", useReq.Database),
		}, nil
	}

	// 检查用户是否有权限访问该数据库
	conn := req.Connection
	if conn != nil {
		if !e.burinServer.HasPermission(conn, useReq.Database, "read") {
			return &cProtocol.ProtocolResponse{
				Status: 403,
				Error:  fmt.Sprintf("No permission to access database '%s'", useReq.Database),
			}, nil
		}

		// 更新连接的当前数据库
		e.burinServer.SetConnectionCurrentDB(conn, useReq.Database)
	}

	e.logger.Infof("Switched to database: %s", useReq.Database)

	response := map[string]interface{}{
		"message":  "Database switched successfully",
		"database": useReq.Database,
	}

	responseData, _ := sonic.Marshal(response)
	return &cProtocol.ProtocolResponse{
		Status: 200,
		Data:   responseData,
	}, nil
}

// handleListDBs 处理列出所有数据库命令
func (e *Engine) handleListDBs(ctx context.Context, req *cProtocol.ProtocolRequest) (*cProtocol.ProtocolResponse, error) {
	// 获取所有数据库列表
	databases := e.cacheEngine.ListDatabases()

	// 如果有连接信息，过滤用户有权限的数据库
	conn := req.Connection
	if conn != nil {
		var accessibleDatabases []string
		for _, db := range databases {
			if e.burinServer.HasPermission(conn, db, "read") {
				accessibleDatabases = append(accessibleDatabases, db)
			}
		}
		databases = accessibleDatabases
	}

	response := map[string]interface{}{
		"databases": databases,
		"count":     len(databases),
	}

	responseData, _ := sonic.Marshal(response)
	return &cProtocol.ProtocolResponse{
		Status: 200,
		Data:   responseData,
	}, nil
}

// handleDBExists 处理检查数据库是否存在命令
func (e *Engine) handleDBExists(ctx context.Context, req *cProtocol.ProtocolRequest) (*cProtocol.ProtocolResponse, error) {
	// 解析请求数据
	var existsReq struct {
		Database string `json:"database"`
	}

	if err := sonic.Unmarshal(req.Data, &existsReq); err != nil {
		return &cProtocol.ProtocolResponse{
			Status: 400,
			Error:  fmt.Sprintf("Failed to parse request: %v", err),
		}, nil
	}

	if existsReq.Database == "" {
		return &cProtocol.ProtocolResponse{
			Status: 400,
			Error:  "database name is required",
		}, nil
	}

	// 检查数据库是否存在
	exists := e.cacheEngine.DatabaseExists(existsReq.Database)

	response := map[string]interface{}{
		"database": existsReq.Database,
		"exists":   exists,
	}

	responseData, _ := sonic.Marshal(response)
	return &cProtocol.ProtocolResponse{
		Status: 200,
		Data:   responseData,
	}, nil
}

// handleDBInfo 处理获取数据库信息命令
func (e *Engine) handleDBInfo(ctx context.Context, req *cProtocol.ProtocolRequest) (*cProtocol.ProtocolResponse, error) {
	// 解析请求数据
	var infoReq struct {
		Database string `json:"database"`
	}

	if err := sonic.Unmarshal(req.Data, &infoReq); err != nil {
		return &cProtocol.ProtocolResponse{
			Status: 400,
			Error:  fmt.Sprintf("Failed to parse request: %v", err),
		}, nil
	}

	if infoReq.Database == "" {
		return &cProtocol.ProtocolResponse{
			Status: 400,
			Error:  "database name is required",
		}, nil
	}

	// 检查数据库是否存在
	exists := e.cacheEngine.DatabaseExists(infoReq.Database)
	if !exists {
		return &cProtocol.ProtocolResponse{
			Status: 404,
			Error:  fmt.Sprintf("Database '%s' does not exist", infoReq.Database),
		}, nil
	}

	// 获取数据库类型
	dbType, err := e.cacheEngine.GetDatabaseType(infoReq.Database)
	if err != nil {
		return &cProtocol.ProtocolResponse{
			Status: 500,
			Error:  fmt.Sprintf("Failed to get database type: %v", err),
		}, nil
	}

	// 获取数据库中的键数量
	count, err := e.cacheEngine.GetDatabaseKeyCount(infoReq.Database)
	if err != nil {
		// 如果方法不存在，设置为-1表示未知
		count = -1
	}

	response := map[string]interface{}{
		"database": infoReq.Database,
		"type":     string(dbType),
		"exists":   true,
	}

	if count >= 0 {
		response["key_count"] = count
	}

	responseData, _ := sonic.Marshal(response)
	return &cProtocol.ProtocolResponse{
		Status: 200,
		Data:   responseData,
	}, nil
}

// handleDeleteDB 处理删除数据库命令
func (e *Engine) handleDeleteDB(ctx context.Context, req *cProtocol.ProtocolRequest) (*cProtocol.ProtocolResponse, error) {
	// 解析请求数据
	var deleteReq struct {
		Database string `json:"database"`
	}

	if err := sonic.Unmarshal(req.Data, &deleteReq); err != nil {
		return &cProtocol.ProtocolResponse{
			Status: 400,
			Error:  fmt.Sprintf("Failed to parse request: %v", err),
		}, nil
	}

	if deleteReq.Database == "" {
		return &cProtocol.ProtocolResponse{
			Status: 400,
			Error:  "database name is required",
		}, nil
	}

	// 检查数据库是否存在
	exists := e.cacheEngine.DatabaseExists(deleteReq.Database)
	if !exists {
		return &cProtocol.ProtocolResponse{
			Status: 404,
			Error:  fmt.Sprintf("Database '%s' does not exist", deleteReq.Database),
		}, nil
	}

	// 检查是否有其他用户对该数据库有权限
	if e.authManager != nil {
		users, err := e.authManager.GetUsersWithPermissionOnDatabase(ctx, deleteReq.Database)
		if err != nil {
			e.logger.Warnf("Failed to check users with permissions: %v", err)
		} else if len(users) > 0 {
			// 过滤掉超级管理员用户，因为他们总是有权限的
			nonSuperAdminUsers := make([]string, 0)
			for _, username := range users {
				user, err := e.authManager.GetUser(ctx, username)
				if err == nil && !user.IsSuperAdmin() {
					nonSuperAdminUsers = append(nonSuperAdminUsers, username)
				}
			}

			// 如果还有普通用户有权限，则不允许删除
			if len(nonSuperAdminUsers) > 0 {
				return &cProtocol.ProtocolResponse{
					Status: 409,
					Error: fmt.Sprintf("Cannot delete database '%s': %d user(s) still have permissions on it: %v",
						deleteReq.Database, len(nonSuperAdminUsers), nonSuperAdminUsers),
				}, nil
			}
		}
	}

	// 通过Raft协议将删除数据库操作同步到集群（包括本节点）
	if e.consensusNode != nil && e.consensusNode.IsLeader() {
		e.logger.Infof("Starting Raft replication for delete database: %s", deleteReq.Database)

		// 创建Raft命令
		raftCommand := map[string]interface{}{
			"operation": "delete_database",
			"database":  deleteReq.Database,
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
			e.logger.Errorf("Raft replication failed for delete database %s: %v", deleteReq.Database, err)
			return &cProtocol.ProtocolResponse{
				Status: 500,
				Error:  fmt.Sprintf("Failed to replicate delete database via Raft: %v", err),
			}, nil
		}

		e.logger.Infof("Delete database replicated via Raft successfully: %s", deleteReq.Database)
	} else {
		// 非 Leader 节点不应该处理写操作
		return &cProtocol.ProtocolResponse{
			Status: 503,
			Error:  "Not leader, cannot delete database",
		}, nil
	}

	response := map[string]interface{}{
		"message":  "Database deleted successfully",
		"database": deleteReq.Database,
	}

	responseData, _ := sonic.Marshal(response)
	return &cProtocol.ProtocolResponse{
		Status: 200,
		Data:   responseData,
	}, nil
}
