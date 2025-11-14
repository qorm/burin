package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// 系统数据库名称 - 固定的系统数据库，不允许普通用户访问
const (
	SystemDatabase = "__burin_system__" // 系统数据库
)

// 用户角色定义
type Role string

const (
	RoleSuperAdmin Role = "superadmin" // 超级管理员 - 所有权限
	RoleAdmin      Role = "admin"      // 管理员 - 可以管理用户和数据库
	RoleReadWrite  Role = "readwrite"  // 读写用户 - 可以读写数据
	RoleReadOnly   Role = "readonly"   // 只读用户 - 只能读取数据
)

// 权限类型定义
type Permission string

const (
	PermissionRead   Permission = "read"   // 读权限
	PermissionWrite  Permission = "write"  // 写权限
	PermissionDelete Permission = "delete" // 删除权限
	PermissionAdmin  Permission = "admin"  // 管理权限（创建/删除数据库等）
	PermissionAll    Permission = "all"    // 所有权限
)

// User 用户模型
type User struct {
	Username     string    `json:"username"`      // 用户名
	PasswordHash string    `json:"password_hash"` // 密码哈希
	Role         Role      `json:"role"`          // 用户角色
	Enabled      bool      `json:"enabled"`       // 是否启用
	CreatedAt    time.Time `json:"created_at"`    // 创建时间
	UpdatedAt    time.Time `json:"updated_at"`    // 更新时间
	Description  string    `json:"description"`   // 用户描述
}

// DatabasePermission 数据库权限
type DatabasePermission struct {
	Username    string       `json:"username"`    // 用户名
	Database    string       `json:"database"`    // 数据库名
	Permissions []Permission `json:"permissions"` // 权限列表
	GrantedAt   time.Time    `json:"granted_at"`  // 授权时间
	GrantedBy   string       `json:"granted_by"`  // 授权人
}

// HashPassword 哈希密码
func HashPassword(password string) string {
	hash := sha256.Sum256([]byte(password))
	return hex.EncodeToString(hash[:])
}

// VerifyPassword 验证密码
func (u *User) VerifyPassword(password string) bool {
	return u.PasswordHash == HashPassword(password)
}

// IsSuperAdmin 是否是超级管理员
func (u *User) IsSuperAdmin() bool {
	return u.Role == RoleSuperAdmin
}

// IsAdmin 是否是管理员（包括超级管理员）
func (u *User) IsAdmin() bool {
	return u.Role == RoleSuperAdmin || u.Role == RoleAdmin
}

// HasPermission 检查用户是否有特定权限
func (dp *DatabasePermission) HasPermission(perm Permission) bool {
	for _, p := range dp.Permissions {
		if p == PermissionAll || p == perm {
			return true
		}
	}
	return false
}

// RolePermissions 角色默认权限映射
var RolePermissions = map[Role][]Permission{
	RoleSuperAdmin: {PermissionAll},
	RoleAdmin:      {PermissionRead, PermissionWrite, PermissionDelete, PermissionAdmin},
	RoleReadWrite:  {PermissionRead, PermissionWrite, PermissionDelete},
	RoleReadOnly:   {PermissionRead},
}

// GetRolePermissions 获取角色的默认权限
func GetRolePermissions(role Role) []Permission {
	if perms, ok := RolePermissions[role]; ok {
		return perms
	}
	return []Permission{}
}
