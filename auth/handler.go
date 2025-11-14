package auth

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/qorm/burin/cProtocol"

	"github.com/sirupsen/logrus"
)

// AuthHandler 认证命令处理器
type AuthHandler struct {
	authManager *AuthManager
	logger      *logrus.Logger
}

// NewAuthHandler 创建认证处理器
func NewAuthHandler(authManager *AuthManager, logger *logrus.Logger) *AuthHandler {
	return &AuthHandler{
		authManager: authManager,
		logger:      logger,
	}
}

// HandleAuthCommand 处理认证命令
func (ah *AuthHandler) HandleAuthCommand(ctx context.Context, head *cProtocol.HEAD, body []byte, username string) ([]byte, error) {
	// 解析命令
	cmd := cProtocol.AuthCommand(head.GetCommand())

	ah.logger.Debugf("Handling auth command: %s from user: %s", cmd.String(), username)

	// 根据命令类型处理
	switch cmd {
	case cProtocol.AuthCommandLogin:
		return ah.handleLogin(ctx, body)
	case cProtocol.AuthCommandCreateUser:
		return ah.handleCreateUser(ctx, body, username)
	case cProtocol.AuthCommandUpdateUser:
		return ah.handleUpdateUser(ctx, body, username)
	case cProtocol.AuthCommandDeleteUser:
		return ah.handleDeleteUser(ctx, body, username)
	case cProtocol.AuthCommandGetUser:
		return ah.handleGetUser(ctx, body, username)
	case cProtocol.AuthCommandListUsers:
		return ah.handleListUsers(ctx, username)
	case cProtocol.AuthCommandChangePass:
		return ah.handleChangePassword(ctx, body, username)
	case cProtocol.AuthCommandResetPass:
		return ah.handleResetPassword(ctx, body, username)
	case cProtocol.AuthCommandGrantPerm:
		return ah.handleGrantPermission(ctx, body, username)
	case cProtocol.AuthCommandRevokePerm:
		return ah.handleRevokePermission(ctx, body, username)
	case cProtocol.AuthCommandListPerm:
		return ah.handleListPermissions(ctx, body, username)
	case cProtocol.AuthCommandCheckPerm:
		return ah.handleCheckPermission(ctx, body, username)
	default:
		return nil, fmt.Errorf("unknown auth command: %d", cmd)
	}
}

// LoginRequest 登录请求
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginResponse 登录响应
type LoginResponse struct {
	Success  bool   `json:"success"`
	Username string `json:"username,omitempty"` // 用户名
	Role     string `json:"role,omitempty"`     // 角色
	Error    string `json:"error,omitempty"`
}

func (ah *AuthHandler) handleLogin(ctx context.Context, body []byte) ([]byte, error) {
	var req LoginRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return ah.errorResponse("invalid request format")
	}

	_, err := ah.authManager.Authenticate(ctx, req.Username, req.Password)
	if err != nil {
		return ah.errorResponse(err.Error())
	}

	// 获取用户信息以返回角色
	user, err := ah.authManager.GetUser(ctx, req.Username)
	if err != nil {
		return ah.errorResponse("failed to get user info")
	}

	// 返回登录成功响应（不包含token，使用连接级别认证）
	resp := LoginResponse{
		Success:  true,
		Username: req.Username,
		Role:     string(user.Role),
	}

	return json.Marshal(resp)
}

// CreateUserRequest 创建用户请求
type CreateUserRequest struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	Role        string `json:"role"`
	Description string `json:"description"`
}

func (ah *AuthHandler) handleCreateUser(ctx context.Context, body []byte, currentUser string) ([]byte, error) {
	// 检查权限 - 只有管理员可以创建用户
	user, _ := ah.authManager.GetUser(ctx, currentUser)
	if user == nil || !user.IsAdmin() {
		return ah.errorResponse("permission denied: admin role required")
	}

	var req CreateUserRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return ah.errorResponse("invalid request format")
	}

	newUser := &User{
		Username:     req.Username,
		PasswordHash: HashPassword(req.Password),
		Role:         Role(req.Role),
		Enabled:      true,
		Description:  req.Description,
	}

	if err := ah.authManager.CreateUser(ctx, newUser); err != nil {
		return ah.errorResponse(err.Error())
	}

	return ah.successResponse()
}

// UpdateUserRequest 更新用户请求
type UpdateUserRequest struct {
	Username    string `json:"username"`
	Role        string `json:"role,omitempty"`
	Enabled     *bool  `json:"enabled,omitempty"`
	Description string `json:"description,omitempty"`
}

func (ah *AuthHandler) handleUpdateUser(ctx context.Context, body []byte, currentUser string) ([]byte, error) {
	// 检查权限
	operator, _ := ah.authManager.GetUser(ctx, currentUser)
	if operator == nil || !operator.IsAdmin() {
		return ah.errorResponse("permission denied: admin role required")
	}

	var req UpdateUserRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return ah.errorResponse("invalid request format")
	}

	user, err := ah.authManager.GetUser(ctx, req.Username)
	if err != nil {
		return ah.errorResponse(err.Error())
	}

	// 更新字段
	if req.Role != "" {
		user.Role = Role(req.Role)
	}
	if req.Enabled != nil {
		user.Enabled = *req.Enabled
	}
	if req.Description != "" {
		user.Description = req.Description
	}

	if err := ah.authManager.UpdateUser(ctx, user); err != nil {
		return ah.errorResponse(err.Error())
	}

	return ah.successResponse()
}

// DeleteUserRequest 删除用户请求
type DeleteUserRequest struct {
	Username string `json:"username"`
}

func (ah *AuthHandler) handleDeleteUser(ctx context.Context, body []byte, currentUser string) ([]byte, error) {
	// 检查权限
	operator, _ := ah.authManager.GetUser(ctx, currentUser)
	if operator == nil || !operator.IsAdmin() {
		return ah.errorResponse("permission denied: admin role required")
	}

	var req DeleteUserRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return ah.errorResponse("invalid request format")
	}

	if err := ah.authManager.DeleteUser(ctx, req.Username); err != nil {
		return ah.errorResponse(err.Error())
	}

	return ah.successResponse()
}

// GetUserRequest 获取用户请求
type GetUserRequest struct {
	Username string `json:"username"`
}

// GetUserResponse 获取用户响应
type GetUserResponse struct {
	Success bool   `json:"success"`
	User    *User  `json:"user,omitempty"`
	Error   string `json:"error,omitempty"`
}

func (ah *AuthHandler) handleGetUser(ctx context.Context, body []byte, currentUser string) ([]byte, error) {
	var req GetUserRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return ah.errorResponse("invalid request format")
	}

	// 用户只能查看自己的信息，除非是管理员
	if req.Username != currentUser {
		operator, _ := ah.authManager.GetUser(ctx, currentUser)
		if operator == nil || !operator.IsAdmin() {
			return ah.errorResponse("permission denied")
		}
	}

	user, err := ah.authManager.GetUser(ctx, req.Username)
	if err != nil {
		return ah.errorResponse(err.Error())
	}

	// 不返回密码哈希
	user.PasswordHash = ""

	resp := GetUserResponse{
		Success: true,
		User:    user,
	}

	return json.Marshal(resp)
}

// ListUsersResponse 列出用户响应
type ListUsersResponse struct {
	Success bool    `json:"success"`
	Users   []*User `json:"users,omitempty"`
	Error   string  `json:"error,omitempty"`
}

func (ah *AuthHandler) handleListUsers(ctx context.Context, currentUser string) ([]byte, error) {
	// 检查权限
	operator, _ := ah.authManager.GetUser(ctx, currentUser)
	if operator == nil || !operator.IsAdmin() {
		return ah.errorResponse("permission denied: admin role required")
	}

	users, err := ah.authManager.ListUsers(ctx)
	if err != nil {
		return ah.errorResponse(err.Error())
	}

	// 不返回密码哈希
	for _, user := range users {
		user.PasswordHash = ""
	}

	resp := ListUsersResponse{
		Success: true,
		Users:   users,
	}

	return json.Marshal(resp)
}

// ChangePasswordRequest 修改密码请求
type ChangePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

func (ah *AuthHandler) handleChangePassword(ctx context.Context, body []byte, currentUser string) ([]byte, error) {
	var req ChangePasswordRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return ah.errorResponse("invalid request format")
	}

	if err := ah.authManager.ChangePassword(ctx, currentUser, req.OldPassword, req.NewPassword); err != nil {
		return ah.errorResponse(err.Error())
	}

	return ah.successResponse()
}

// ResetPasswordRequest 重置密码请求
type ResetPasswordRequest struct {
	Username    string `json:"username"`
	NewPassword string `json:"new_password"`
}

func (ah *AuthHandler) handleResetPassword(ctx context.Context, body []byte, currentUser string) ([]byte, error) {
	// 检查权限
	operator, _ := ah.authManager.GetUser(ctx, currentUser)
	if operator == nil || !operator.IsAdmin() {
		return ah.errorResponse("permission denied: admin role required")
	}

	var req ResetPasswordRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return ah.errorResponse("invalid request format")
	}

	if err := ah.authManager.ResetPassword(ctx, req.Username, req.NewPassword); err != nil {
		return ah.errorResponse(err.Error())
	}

	return ah.successResponse()
}

// GrantPermissionRequest 授权请求
type GrantPermissionRequest struct {
	Username    string   `json:"username"`
	Database    string   `json:"database"`
	Permissions []string `json:"permissions"`
}

func (ah *AuthHandler) handleGrantPermission(ctx context.Context, body []byte, currentUser string) ([]byte, error) {
	// 检查权限
	operator, _ := ah.authManager.GetUser(ctx, currentUser)
	if operator == nil || !operator.IsAdmin() {
		return ah.errorResponse("permission denied: admin role required")
	}

	var req GrantPermissionRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return ah.errorResponse("invalid request format")
	}

	// 转换权限
	perms := make([]Permission, len(req.Permissions))
	for i, p := range req.Permissions {
		perms[i] = Permission(p)
	}

	if err := ah.authManager.GrantPermission(ctx, req.Username, req.Database, perms, currentUser); err != nil {
		return ah.errorResponse(err.Error())
	}

	return ah.successResponse()
}

// RevokePermissionRequest 撤销权限请求
type RevokePermissionRequest struct {
	Username string `json:"username"`
	Database string `json:"database"`
}

func (ah *AuthHandler) handleRevokePermission(ctx context.Context, body []byte, currentUser string) ([]byte, error) {
	// 检查权限
	operator, _ := ah.authManager.GetUser(ctx, currentUser)
	if operator == nil || !operator.IsAdmin() {
		return ah.errorResponse("permission denied: admin role required")
	}

	var req RevokePermissionRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return ah.errorResponse("invalid request format")
	}

	if err := ah.authManager.RevokePermission(ctx, req.Username, req.Database); err != nil {
		return ah.errorResponse(err.Error())
	}

	return ah.successResponse()
}

// ListPermissionsRequest 列出权限请求
type ListPermissionsRequest struct {
	Username string `json:"username"`
}

// ListPermissionsResponse 列出权限响应
type ListPermissionsResponse struct {
	Success     bool                  `json:"success"`
	Permissions []*DatabasePermission `json:"permissions,omitempty"`
	Error       string                `json:"error,omitempty"`
}

func (ah *AuthHandler) handleListPermissions(ctx context.Context, body []byte, currentUser string) ([]byte, error) {
	var req ListPermissionsRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return ah.errorResponse("invalid request format")
	}

	// 用户只能查看自己的权限，除非是管理员
	if req.Username != currentUser {
		operator, _ := ah.authManager.GetUser(ctx, currentUser)
		if operator == nil || !operator.IsAdmin() {
			return ah.errorResponse("permission denied")
		}
	}

	perms, err := ah.authManager.ListUserPermissions(ctx, req.Username)
	if err != nil {
		return ah.errorResponse(err.Error())
	}

	resp := ListPermissionsResponse{
		Success:     true,
		Permissions: perms,
	}

	return json.Marshal(resp)
}

// CheckPermissionRequest 检查权限请求
type CheckPermissionRequest struct {
	Username   string `json:"username"`
	Database   string `json:"database"`
	Permission string `json:"permission"`
}

// CheckPermissionResponse 检查权限响应
type CheckPermissionResponse struct {
	Success       bool   `json:"success"`
	HasPermission bool   `json:"has_permission"`
	Error         string `json:"error,omitempty"`
}

func (ah *AuthHandler) handleCheckPermission(ctx context.Context, body []byte, currentUser string) ([]byte, error) {
	var req CheckPermissionRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return ah.errorResponse("invalid request format")
	}

	hasPerm, err := ah.authManager.CheckPermission(ctx, req.Username, req.Database, Permission(req.Permission))
	if err != nil {
		return ah.errorResponse(err.Error())
	}

	resp := CheckPermissionResponse{
		Success:       true,
		HasPermission: hasPerm,
	}

	return json.Marshal(resp)
}

// 辅助方法
func (ah *AuthHandler) successResponse() ([]byte, error) {
	resp := map[string]interface{}{
		"success": true,
	}
	return json.Marshal(resp)
}

func (ah *AuthHandler) errorResponse(message string) ([]byte, error) {
	resp := map[string]interface{}{
		"success": false,
		"error":   message,
	}
	return json.Marshal(resp)
}
