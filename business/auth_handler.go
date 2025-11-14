// Auth commands handler for Engine

package business

import (
	"context"
	"fmt"

	"github.com/bytedance/sonic"

	"github.com/qorm/burin/cProtocol"
)

// handleAuthCommands 处理认证相关命令
func (e *Engine) handleAuthCommands(ctx context.Context, req *cProtocol.ProtocolRequest) (*cProtocol.ProtocolResponse, error) {
	// 记录统计信息
	e.updateStats()

	// 验证命令类型
	commandType, command := cProtocol.ParseCommandFromHead(req.Head)
	if commandType != cProtocol.CommandTypeAuth {
		return &cProtocol.ProtocolResponse{
			Status: 400,
			Error:  "Invalid command type for Auth service",
		}, nil
	}

	authCmd := cProtocol.AuthCommand(command)
	e.logger.Debugf("Processing Auth command: %v (%d)", authCmd, authCmd)

	// 检查认证系统是否可用
	if e.authHandler == nil {
		return &cProtocol.ProtocolResponse{
			Status: 503,
			Error:  "Auth system not available",
		}, nil
	}

	// 从连接中提取当前用户信息（连接级别认证）
	// 对于登录命令，不需要认证
	username := ""
	if authCmd != cProtocol.AuthCommandLogin {
		// 从Meta中获取已认证的用户名
		if req.Meta != nil {
			username = req.Meta["username"]
		}

		// 某些命令需要认证
		if username == "" {
			return &cProtocol.ProtocolResponse{
				Status: 401,
				Error:  "Unauthorized: authentication required",
			}, nil
		}
	}

	// 调用认证处理器
	responseData, err := e.authHandler.HandleAuthCommand(ctx, req.Head, req.Data, username)
	if err != nil {
		e.logger.Errorf("Failed to handle auth command: %v", err)
		return &cProtocol.ProtocolResponse{
			Status: 500,
			Error:  fmt.Sprintf("Failed to handle auth command: %v", err),
		}, nil
	}

	// 如果是登录命令且成功，标记连接为已认证
	if authCmd == cProtocol.AuthCommandLogin && req.Connection != nil {
		// 解析响应以获取用户名和角色
		var loginResp struct {
			Success  bool   `json:"success"`
			Username string `json:"username"`
			Role     string `json:"role"`
		}
		if err := sonic.Unmarshal(responseData, &loginResp); err == nil && loginResp.Success {
			// 获取用户的所有数据库权限
			permissions := make(map[string][]string)

			// 获取用户的所有权限
			userPerms, err := e.authManager.ListUserPermissions(ctx, loginResp.Username)
			if err != nil {
				e.logger.Warnf("Failed to get user permissions: %v", err)
			} else {
				for _, perm := range userPerms {
					permList := make([]string, len(perm.Permissions))
					for i, p := range perm.Permissions {
						permList[i] = string(p)
					}
					permissions[perm.Database] = permList
				}
			}

			// 获取 burin server 并标记连接
			if e.burinServer != nil {
				e.burinServer.MarkConnectionAuthenticated(req.Connection, loginResp.Username, loginResp.Role, permissions)
				e.logger.Infof("Connection authenticated after login: user=%s, role=%s, databases=%d, remote=%s",
					loginResp.Username, loginResp.Role, len(permissions), req.Connection.RemoteAddr())
			}
		}
	}

	return &cProtocol.ProtocolResponse{
		Status: 200,
		Data:   responseData,
	}, nil
}
