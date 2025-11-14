package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/qorm/burin/auth"
	"github.com/qorm/burin/cProtocol"

	"github.com/sirupsen/logrus"
)

// 这是一个认证系统的使用示例
// 展示如何：
// 1. 用户登录
// 2. 创建用户
// 3. 授予权限
// 4. 检查权限

func main() {
	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)

	fmt.Println("=== Burin 认证系统示例 ===")
	fmt.Println()

	// 注意：实际使用时，这些命令应该通过网络协议发送到Burin服务器
	// 这里仅演示如何构造请求和响应

	// 示例1：用户登录
	fmt.Println("1. 用户登录示例")
	loginReq := auth.LoginRequest{
		Username: "burin",
		Password: "burin@secret",
	}
	loginData, _ := json.Marshal(loginReq)
	fmt.Printf("   请求: %s\n", string(loginData))
	fmt.Println("   命令类型: AuthCommandLogin")
	fmt.Println("   响应包含: token和过期时间")
	fmt.Println()

	// 示例2：创建用户
	fmt.Println("2. 创建用户示例")
	createUserReq := auth.CreateUserRequest{
		Username:    "testuser",
		Password:    "testpass123",
		Role:        "readwrite",
		Description: "测试用户",
	}
	createUserData, _ := json.Marshal(createUserReq)
	fmt.Printf("   请求: %s\n", string(createUserData))
	fmt.Println("   命令类型: AuthCommandCreateUser")
	fmt.Println("   需要: 管理员权限，在Meta中传递token")
	fmt.Println()

	// 示例3：授予数据库权限
	fmt.Println("3. 授予权限示例")
	grantPermReq := auth.GrantPermissionRequest{
		Username:    "testuser",
		Database:    "mydb",
		Permissions: []string{"read", "write"},
	}
	grantPermData, _ := json.Marshal(grantPermReq)
	fmt.Printf("   请求: %s\n", string(grantPermData))
	fmt.Println("   命令类型: AuthCommandGrantPerm")
	fmt.Println("   需要: 管理员权限")
	fmt.Println()

	// 示例4：检查权限
	fmt.Println("4. 检查权限示例")
	checkPermReq := auth.CheckPermissionRequest{
		Username:   "testuser",
		Database:   "mydb",
		Permission: "read",
	}
	checkPermData, _ := json.Marshal(checkPermReq)
	fmt.Printf("   请求: %s\n", string(checkPermData))
	fmt.Println("   命令类型: AuthCommandCheckPerm")
	fmt.Println()

	// 协议头构造示例
	fmt.Println("5. 协议头构造示例")
	fmt.Println("   认证命令使用单独的命令类型: CommandTypeAuth (4)")

	loginHead := cProtocol.CreateAuthCommandHead(cProtocol.AuthCommandLogin)
	fmt.Printf("   登录命令头: CommandType=%d, Command=%d\n",
		loginHead.GetCommandType(), loginHead.GetCommand())

	createUserHead := cProtocol.CreateAuthCommandHead(cProtocol.AuthCommandCreateUser)
	fmt.Printf("   创建用户命令头: CommandType=%d, Command=%d\n",
		createUserHead.GetCommandType(), createUserHead.GetCommand())
	fmt.Println()

	// 系统数据库说明
	fmt.Println("6. 系统数据库")
	fmt.Printf("   系统数据库名: %s\n", auth.SystemDatabase)
	fmt.Println("   用途: 存储用户信息、权限信息、认证令牌")
	fmt.Println("   特点: 不允许普通用户直接访问，只能通过认证命令访问")
	fmt.Println()

	// 用户角色说明
	fmt.Println("7. 用户角色")
	fmt.Println("   - superadmin: 超级管理员，拥有所有权限")
	fmt.Println("   - admin: 管理员，可以管理用户和数据库")
	fmt.Println("   - readwrite: 读写用户，可以读写数据")
	fmt.Println("   - readonly: 只读用户，只能读取数据")
	fmt.Println()

	// 权限类型说明
	fmt.Println("8. 权限类型")
	fmt.Println("   - read: 读权限")
	fmt.Println("   - write: 写权限")
	fmt.Println("   - delete: 删除权限")
	fmt.Println("   - admin: 管理权限（创建/删除数据库等）")
	fmt.Println("   - all: 所有权限")
	fmt.Println()

	fmt.Println("=== 示例结束 ===")

	// 实际使用示例（需要运行Burin服务器）
	demonstrateActualUsage()
}

func demonstrateActualUsage() {
	fmt.Println()
	fmt.Println("=== 实际使用流程 ===")
	fmt.Println("1. 启动Burin服务器（会自动创建默认超级管理员）")
	fmt.Println("   用户名: admin")
	fmt.Println("   密码: burin2025")
	fmt.Println()
	fmt.Println("2. 使用客户端连接并登录")
	fmt.Println("   发送 AuthCommandLogin 命令")
	fmt.Println("   获取 token")
	fmt.Println()
	fmt.Println("3. 使用token创建新用户")
	fmt.Println("   在请求Meta中包含 token 字段")
	fmt.Println("   发送 AuthCommandCreateUser 命令")
	fmt.Println()
	fmt.Println("4. 为用户授予数据库权限")
	fmt.Println("   发送 AuthCommandGrantPerm 命令")
	fmt.Println()
	fmt.Println("5. 用户登录并使用被授权的数据库")
	fmt.Println("   使用自己的用户名密码登录")
	fmt.Println("   获取token后即可访问被授权的数据库")
	fmt.Println()
	fmt.Println("注意：")
	fmt.Println("- 所有认证命令都使用 CommandTypeAuth (4)")
	fmt.Println("- 除登录外的命令都需要在Meta中传递token")
	fmt.Println("- 系统数据库 __burin_system__ 不允许直接访问")
	fmt.Println("- 超级管理员拥有所有权限，无需额外授权")

	ctx := context.Background()
	_ = ctx
	_ = log.Print
}
