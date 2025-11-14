package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// StorageBackend 存储后端接口
type StorageBackend interface {
	// Get 获取数据
	Get(ctx context.Context, database, key string) ([]byte, error)
	// Set 设置数据
	Set(ctx context.Context, database, key string, value []byte, ttl time.Duration) error
	// Delete 删除数据
	Delete(ctx context.Context, database, key string) error
	// List 列出键
	List(ctx context.Context, database, prefix string) ([]string, error)
}

// AuthManager 认证管理器
type AuthManager struct {
	storage     StorageBackend
	logger      *logrus.Logger
	config      *AuthConfig
	initialized bool
}

// AuthConfig 认证配置
type AuthConfig struct {
	Enabled        bool   `mapstructure:"enabled" yaml:"enabled"`                 // 是否启用认证
	SuperAdminUser string `mapstructure:"superadmin_user" yaml:"superadmin_user"` // 超级管理员用户名
	SuperAdminPass string `mapstructure:"superadmin_pass" yaml:"superadmin_pass"` // 超级管理员密码
	RequireAuth    bool   `mapstructure:"require_auth" yaml:"require_auth"`       // 是否强制要求认证
}

// NewAuthManager 创建认证管理器
func NewAuthManager(storage StorageBackend, logger *logrus.Logger, config *AuthConfig) *AuthManager {
	if config == nil {
		config = &AuthConfig{
			Enabled:        true,
			SuperAdminUser: "burin",
			SuperAdminPass: "burin2025",
			RequireAuth:    true,
		}
	}

	return &AuthManager{
		storage:     storage,
		logger:      logger,
		config:      config,
		initialized: false,
	}
}

// Initialize 初始化认证系统
func (am *AuthManager) Initialize(ctx context.Context) error {
	if am.initialized {
		return nil
	}

	// 创建默认超级管理员用户
	if err := am.createDefaultSuperAdmin(ctx); err != nil {
		return fmt.Errorf("failed to create default superadmin: %w", err)
	}

	am.initialized = true
	am.logger.Info("Auth system initialized successfully")
	return nil
}

// createDefaultSuperAdmin 创建默认超级管理员
func (am *AuthManager) createDefaultSuperAdmin(ctx context.Context) error {
	username := am.config.SuperAdminUser

	// 检查是否已存在
	_, err := am.GetUser(ctx, username)
	if err == nil {
		am.logger.Infof("Superadmin user '%s' already exists", username)
		return nil
	}

	// 创建超级管理员
	user := &User{
		Username:     username,
		PasswordHash: HashPassword(am.config.SuperAdminPass),
		Role:         RoleSuperAdmin,
		Enabled:      true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		Description:  "Default superadmin user",
	}

	if err := am.CreateUser(ctx, user); err != nil {
		return fmt.Errorf("failed to create superadmin: %w", err)
	}

	am.logger.Infof("Created default superadmin user: %s", username)
	return nil
}

// CreateUser 创建用户
func (am *AuthManager) CreateUser(ctx context.Context, user *User) error {
	if user.Username == "" {
		return fmt.Errorf("username cannot be empty")
	}

	// 检查用户是否已存在
	_, err := am.GetUser(ctx, user.Username)
	if err == nil {
		return fmt.Errorf("user already exists: %s", user.Username)
	}

	// 设置时间戳
	now := time.Now()
	user.CreatedAt = now
	user.UpdatedAt = now

	// 序列化用户数据
	data, err := json.Marshal(user)
	if err != nil {
		return fmt.Errorf("failed to marshal user: %w", err)
	}

	// 存储到系统数据库
	key := fmt.Sprintf("user:%s", user.Username)
	if err := am.storage.Set(ctx, SystemDatabase, key, data, 0); err != nil {
		return fmt.Errorf("failed to store user: %w", err)
	}

	am.logger.Infof("Created user: %s (role: %s)", user.Username, user.Role)
	return nil
}

// GetUser 获取用户
func (am *AuthManager) GetUser(ctx context.Context, username string) (*User, error) {
	key := fmt.Sprintf("user:%s", username)
	data, err := am.storage.Get(ctx, SystemDatabase, key)
	if err != nil {
		return nil, fmt.Errorf("user not found: %s", username)
	}

	var user User
	if err := json.Unmarshal(data, &user); err != nil {
		return nil, fmt.Errorf("failed to unmarshal user: %w", err)
	}

	return &user, nil
}

// UpdateUser 更新用户
func (am *AuthManager) UpdateUser(ctx context.Context, user *User) error {
	// 检查用户是否存在
	_, err := am.GetUser(ctx, user.Username)
	if err != nil {
		return fmt.Errorf("user not found: %s", user.Username)
	}

	// 更新时间戳
	user.UpdatedAt = time.Now()

	// 序列化用户数据
	data, err := json.Marshal(user)
	if err != nil {
		return fmt.Errorf("failed to marshal user: %w", err)
	}

	// 存储到系统数据库
	key := fmt.Sprintf("user:%s", user.Username)
	if err := am.storage.Set(ctx, SystemDatabase, key, data, 0); err != nil {
		return fmt.Errorf("failed to update user: %w", err)
	}

	am.logger.Infof("Updated user: %s", user.Username)
	return nil
}

// DeleteUser 删除用户
func (am *AuthManager) DeleteUser(ctx context.Context, username string) error {
	// 不允许删除超级管理员
	user, err := am.GetUser(ctx, username)
	if err != nil {
		return fmt.Errorf("user not found: %s", username)
	}

	if user.IsSuperAdmin() {
		return fmt.Errorf("cannot delete superadmin user")
	}

	// 删除用户
	key := fmt.Sprintf("user:%s", username)
	if err := am.storage.Delete(ctx, SystemDatabase, key); err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}

	// 删除用户的所有权限
	perms, _ := am.ListUserPermissions(ctx, username)
	for _, perm := range perms {
		am.RevokePermission(ctx, username, perm.Database)
	}

	am.logger.Infof("Deleted user: %s", username)
	return nil
}

// ListUsers 列出所有用户
func (am *AuthManager) ListUsers(ctx context.Context) ([]*User, error) {
	keys, err := am.storage.List(ctx, SystemDatabase, "user:")
	if err != nil {
		return nil, fmt.Errorf("failed to list users: %w", err)
	}

	users := make([]*User, 0, len(keys))
	for _, key := range keys {
		username := strings.TrimPrefix(key, "user:")
		user, err := am.GetUser(ctx, username)
		if err != nil {
			am.logger.Warnf("Failed to get user %s: %v", username, err)
			continue
		}
		users = append(users, user)
	}

	return users, nil
}

// Authenticate 认证用户 (连接级别认证,不使用token)
func (am *AuthManager) Authenticate(ctx context.Context, username, password string) (*User, error) {
	user, err := am.GetUser(ctx, username)
	if err != nil {
		return nil, fmt.Errorf("authentication failed: invalid username or password")
	}

	if !user.Enabled {
		return nil, fmt.Errorf("user is disabled")
	}

	if !user.VerifyPassword(password) {
		return nil, fmt.Errorf("authentication failed: invalid username or password")
	}

	am.logger.Infof("User authenticated: %s", username)
	return user, nil
}

// GrantPermission 授予权限
func (am *AuthManager) GrantPermission(ctx context.Context, username, database string, permissions []Permission, grantedBy string) error {
	// 检查用户是否存在
	_, err := am.GetUser(ctx, username)
	if err != nil {
		return fmt.Errorf("user not found: %s", username)
	}

	perm := &DatabasePermission{
		Username:    username,
		Database:    database,
		Permissions: permissions,
		GrantedAt:   time.Now(),
		GrantedBy:   grantedBy,
	}

	// 序列化权限数据
	data, err := json.Marshal(perm)
	if err != nil {
		return fmt.Errorf("failed to marshal permission: %w", err)
	}

	// 存储到系统数据库
	key := fmt.Sprintf("perm:%s:%s", username, database)
	if err := am.storage.Set(ctx, SystemDatabase, key, data, 0); err != nil {
		return fmt.Errorf("failed to store permission: %w", err)
	}

	am.logger.Infof("Granted permissions to user %s on database %s: %v", username, database, permissions)
	return nil
}

// RevokePermission 撤销权限
func (am *AuthManager) RevokePermission(ctx context.Context, username, database string) error {
	key := fmt.Sprintf("perm:%s:%s", username, database)
	if err := am.storage.Delete(ctx, SystemDatabase, key); err != nil {
		return fmt.Errorf("failed to revoke permission: %w", err)
	}

	am.logger.Infof("Revoked permissions for user %s on database %s", username, database)
	return nil
}

// GetPermission 获取用户在特定数据库的权限
func (am *AuthManager) GetPermission(ctx context.Context, username, database string) (*DatabasePermission, error) {
	key := fmt.Sprintf("perm:%s:%s", username, database)
	data, err := am.storage.Get(ctx, SystemDatabase, key)
	if err != nil {
		return nil, fmt.Errorf("permission not found")
	}

	var perm DatabasePermission
	if err := json.Unmarshal(data, &perm); err != nil {
		return nil, fmt.Errorf("failed to unmarshal permission: %w", err)
	}

	return &perm, nil
}

// ListUserPermissions 列出用户的所有权限
func (am *AuthManager) ListUserPermissions(ctx context.Context, username string) ([]*DatabasePermission, error) {
	prefix := fmt.Sprintf("perm:%s:", username)
	keys, err := am.storage.List(ctx, SystemDatabase, prefix)
	if err != nil {
		return nil, fmt.Errorf("failed to list permissions: %w", err)
	}

	permissions := make([]*DatabasePermission, 0, len(keys))
	for _, key := range keys {
		data, err := am.storage.Get(ctx, SystemDatabase, key)
		if err != nil {
			continue
		}

		var perm DatabasePermission
		if err := json.Unmarshal(data, &perm); err != nil {
			continue
		}
		permissions = append(permissions, &perm)
	}

	return permissions, nil
}

// CheckPermission 检查用户是否有特定权限
func (am *AuthManager) CheckPermission(ctx context.Context, username, database string, requiredPerm Permission) (bool, error) {
	// 获取用户信息
	user, err := am.GetUser(ctx, username)
	if err != nil {
		return false, fmt.Errorf("user not found: %s", username)
	}

	if !user.Enabled {
		return false, fmt.Errorf("user is disabled")
	}

	// 超级管理员拥有所有权限
	if user.IsSuperAdmin() {
		return true, nil
	}

	// 检查数据库权限
	perm, err := am.GetPermission(ctx, username, database)
	if err != nil {
		// 如果没有明确的数据库权限，使用角色默认权限
		rolePerms := GetRolePermissions(user.Role)
		for _, p := range rolePerms {
			if p == PermissionAll || p == requiredPerm {
				return true, nil
			}
		}
		return false, nil
	}

	return perm.HasPermission(requiredPerm), nil
}

// ChangePassword 修改密码
func (am *AuthManager) ChangePassword(ctx context.Context, username, oldPassword, newPassword string) error {
	user, err := am.GetUser(ctx, username)
	if err != nil {
		return fmt.Errorf("user not found: %s", username)
	}

	// 验证旧密码
	if !user.VerifyPassword(oldPassword) {
		return fmt.Errorf("invalid old password")
	}

	// 更新密码
	user.PasswordHash = HashPassword(newPassword)
	user.UpdatedAt = time.Now()

	if err := am.UpdateUser(ctx, user); err != nil {
		return fmt.Errorf("failed to update password: %w", err)
	}

	am.logger.Infof("Password changed for user: %s", username)
	return nil
}

// ResetPassword 重置密码（管理员操作）
func (am *AuthManager) ResetPassword(ctx context.Context, username, newPassword string) error {
	user, err := am.GetUser(ctx, username)
	if err != nil {
		return fmt.Errorf("user not found: %s", username)
	}

	// 更新密码
	user.PasswordHash = HashPassword(newPassword)
	user.UpdatedAt = time.Now()

	if err := am.UpdateUser(ctx, user); err != nil {
		return fmt.Errorf("failed to reset password: %w", err)
	}

	am.logger.Infof("Password reset for user: %s", username)
	return nil
}

// GetUsersWithPermissionOnDatabase 获取有特定数据库权限的所有用户
func (am *AuthManager) GetUsersWithPermissionOnDatabase(ctx context.Context, database string) ([]string, error) {
	// 列出所有权限记录
	keys, err := am.storage.List(ctx, SystemDatabase, "perm:")
	if err != nil {
		return nil, fmt.Errorf("failed to list permissions: %w", err)
	}

	usersSet := make(map[string]bool)

	// 检查每个权限记录
	for _, key := range keys {
		// 权限key格式: perm:username:database
		parts := strings.Split(key, ":")
		if len(parts) != 3 {
			continue
		}

		permUsername := parts[1]
		permDatabase := parts[2]

		// 如果是目标数据库
		if permDatabase == database {
			usersSet[permUsername] = true
		}
	}

	// 检查超级管理员（他们对所有数据库都有权限）
	allUsers, err := am.ListUsers(ctx)
	if err == nil {
		for _, user := range allUsers {
			if user.IsSuperAdmin() {
				usersSet[user.Username] = true
			}
		}
	}

	// 转换为列表
	users := make([]string, 0, len(usersSet))
	for username := range usersSet {
		users = append(users, username)
	}

	return users, nil
}
