package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/qorm/burin/auth"
	"github.com/qorm/burin/cProtocol"

	"github.com/spf13/cobra"
)

// User management command
var userCmd = &cobra.Command{
	Use:   "user",
	Short: "用户管理",
	Long:  "管理 Burin 用户账户和权限",
}

// Create user command
var (
	createUserRole string
	createUserDesc string
	createUserCmd  = &cobra.Command{
		Use:   "create <username> <password>",
		Short: "创建新用户",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			username := args[0]
			password := args[1]

			req := auth.CreateUserRequest{
				Username:    username,
				Password:    password,
				Role:        createUserRole,
				Description: createUserDesc,
			}

			data, err := json.Marshal(req)
			if err != nil {
				fmt.Printf("❌ 请求序列化失败: %v\n", err)
				os.Exit(1)
			}

			head := cProtocol.CreateAuthCommandHead(cProtocol.AuthCommandCreateUser)
			ctx := context.Background()

			resp, err := burinClient.SendAuthRequest(ctx, head, data)
			if err != nil {
				fmt.Printf("❌ 创建用户失败: %v\n", err)
				os.Exit(1)
			}

			if resp.Status == 200 {
				fmt.Printf("✅ 用户 '%s' 创建成功 (角色: %s)\n", username, createUserRole)
			} else {
				fmt.Printf("❌ 创建失败: %s\n", string(resp.Data))
				os.Exit(1)
			}
		},
	}
)

func init() {
	createUserCmd.Flags().StringVarP(&createUserRole, "role", "r", "readwrite", "用户角色 (superadmin/admin/readwrite/readonly)")
	createUserCmd.Flags().StringVarP(&createUserDesc, "desc", "D", "", "用户描述")
}

// Delete user command
var deleteUserCmd = &cobra.Command{
	Use:   "delete <username>",
	Short: "删除用户",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		username := args[0]

		req := auth.DeleteUserRequest{
			Username: username,
		}

		data, err := json.Marshal(req)
		if err != nil {
			fmt.Printf("❌ 请求序列化失败: %v\n", err)
			os.Exit(1)
		}

		head := cProtocol.CreateAuthCommandHead(cProtocol.AuthCommandDeleteUser)
		ctx := context.Background()

		resp, err := burinClient.SendAuthRequest(ctx, head, data)
		if err != nil {
			fmt.Printf("❌ 删除用户失败: %v\n", err)
			os.Exit(1)
		}

		if resp.Status == 200 {
			fmt.Printf("✅ 用户 '%s' 已删除\n", username)
		} else {
			fmt.Printf("❌ 删除失败: %s\n", string(resp.Data))
			os.Exit(1)
		}
	},
}

// List users command
var listUsersCmd = &cobra.Command{
	Use:   "list",
	Short: "列出所有用户",
	Run: func(cmd *cobra.Command, args []string) {
		head := cProtocol.CreateAuthCommandHead(cProtocol.AuthCommandListUsers)
		ctx := context.Background()

		resp, err := burinClient.SendAuthRequest(ctx, head, []byte("{}"))
		if err != nil {
			fmt.Printf("❌ 获取用户列表失败: %v\n", err)
			os.Exit(1)
		}

		if resp.Status == 200 {
			var result auth.ListUsersResponse
			if err := json.Unmarshal(resp.Data, &result); err != nil {
				fmt.Printf("❌ 解析响应失败: %v\n", err)
				os.Exit(1)
			}

			if len(result.Users) == 0 {
				fmt.Println("没有用户")
				return
			}

			fmt.Printf("共 %d 个用户:\n\n", len(result.Users))
			fmt.Printf("%-20s %-15s %-30s\n", "用户名", "角色", "描述")
			fmt.Println("--------------------------------------------------------------------------------")
			for _, user := range result.Users {
				fmt.Printf("%-20s %-15s %-30s\n", user.Username, user.Role, user.Description)
			}
		} else {
			fmt.Printf("❌ 获取失败: %s\n", string(resp.Data))
			os.Exit(1)
		}
	},
}

// Get user info command
var getUserCmd = &cobra.Command{
	Use:   "info <username>",
	Short: "获取用户信息",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		username := args[0]

		req := auth.GetUserRequest{
			Username: username,
		}

		data, err := json.Marshal(req)
		if err != nil {
			fmt.Printf("❌ 请求序列化失败: %v\n", err)
			os.Exit(1)
		}

		head := cProtocol.CreateAuthCommandHead(cProtocol.AuthCommandGetUser)
		ctx := context.Background()

		resp, err := burinClient.SendAuthRequest(ctx, head, data)
		if err != nil {
			fmt.Printf("❌ 获取用户信息失败: %v\n", err)
			os.Exit(1)
		}

		if resp.Status == 200 {
			var result auth.GetUserResponse
			if err := json.Unmarshal(resp.Data, &result); err != nil {
				fmt.Printf("❌ 解析响应失败: %v\n", err)
				os.Exit(1)
			}

			fmt.Printf("用户名: %s\n", result.User.Username)
			fmt.Printf("角色: %s\n", result.User.Role)
			fmt.Printf("描述: %s\n", result.User.Description)
			fmt.Printf("创建时间: %s\n", result.User.CreatedAt.Format("2006-01-02 15:04:05"))
			fmt.Printf("更新时间: %s\n", result.User.UpdatedAt.Format("2006-01-02 15:04:05"))
			fmt.Printf("状态: ")
			if result.User.Enabled {
				fmt.Println("启用")
			} else {
				fmt.Println("禁用")
			}
		} else {
			fmt.Printf("❌ 获取失败: %s\n", string(resp.Data))
			os.Exit(1)
		}
	},
}

// Change password command
var (
	changePassOld string
	changePassNew string
	changePassCmd = &cobra.Command{
		Use:   "passwd",
		Short: "修改当前用户密码",
		Run: func(cmd *cobra.Command, args []string) {
			if changePassOld == "" || changePassNew == "" {
				fmt.Println("❌ 必须提供旧密码和新密码")
				cmd.Usage()
				os.Exit(1)
			}

			req := auth.ChangePasswordRequest{
				OldPassword: changePassOld,
				NewPassword: changePassNew,
			}

			data, err := json.Marshal(req)
			if err != nil {
				fmt.Printf("❌ 请求序列化失败: %v\n", err)
				os.Exit(1)
			}

			head := cProtocol.CreateAuthCommandHead(cProtocol.AuthCommandChangePass)
			ctx := context.Background()

			resp, err := burinClient.SendAuthRequest(ctx, head, data)
			if err != nil {
				fmt.Printf("❌ 修改密码失败: %v\n", err)
				os.Exit(1)
			}

			if resp.Status == 200 {
				fmt.Println("✅ 密码修改成功")
			} else {
				fmt.Printf("❌ 修改失败: %s\n", string(resp.Data))
				os.Exit(1)
			}
		},
	}
)

func init() {
	changePassCmd.Flags().StringVar(&changePassOld, "old", "", "旧密码")
	changePassCmd.Flags().StringVar(&changePassNew, "new", "", "新密码")
	changePassCmd.MarkFlagRequired("old")
	changePassCmd.MarkFlagRequired("new")
}

// Grant permission command
var grantPermCmd = &cobra.Command{
	Use:   "grant <username> <database> <permissions>",
	Short: "授予用户数据库权限",
	Long:  "授予用户对指定数据库的权限。权限可以是: read, write, delete, admin",
	Args:  cobra.MinimumNArgs(3),
	Run: func(cmd *cobra.Command, args []string) {
		username := args[0]
		database := args[1]
		permissions := args[2:]

		req := auth.GrantPermissionRequest{
			Username:    username,
			Database:    database,
			Permissions: permissions,
		}

		data, err := json.Marshal(req)
		if err != nil {
			fmt.Printf("❌ 请求序列化失败: %v\n", err)
			os.Exit(1)
		}

		head := cProtocol.CreateAuthCommandHead(cProtocol.AuthCommandGrantPerm)
		ctx := context.Background()

		resp, err := burinClient.SendAuthRequest(ctx, head, data)
		if err != nil {
			fmt.Printf("❌ 撤销权限失败: %v\n", err)
			os.Exit(1)
		}

		if resp.Status == 200 {
			fmt.Printf("✅ 已撤销用户 '%s' 对数据库 '%s' 的权限\n", username, database)
		} else {
			fmt.Printf("❌ 撤销失败: %s\n", string(resp.Data))
			os.Exit(1)
		}
	},
}

// Revoke permission command
var revokePermCmd = &cobra.Command{
	Use:   "revoke <username> <database>",
	Short: "撤销用户数据库权限",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		username := args[0]
		database := args[1]

		req := auth.RevokePermissionRequest{
			Username: username,
			Database: database,
		}

		data, err := json.Marshal(req)
		if err != nil {
			fmt.Printf("❌ 请求序列化失败: %v\n", err)
			os.Exit(1)
		}

		head := cProtocol.CreateAuthCommandHead(cProtocol.AuthCommandRevokePerm)
		ctx := context.Background()

		resp, err := burinClient.SendAuthRequest(ctx, head, data)
		if err != nil {
			fmt.Printf("❌ 创建用户失败: %v\n", err)
			os.Exit(1)
		}

		if resp.Status == 200 {
			fmt.Printf("✅ 用户 '%s' 创建成功 (角色: %s)\n", username, createUserRole)
		} else {
			fmt.Printf("❌ 创建失败: %s\n", string(resp.Data))
			os.Exit(1)
		}
	},
}

func init() {
	userCmd.AddCommand(createUserCmd)
	userCmd.AddCommand(deleteUserCmd)
	userCmd.AddCommand(listUsersCmd)
	userCmd.AddCommand(getUserCmd)
	userCmd.AddCommand(changePassCmd)
	userCmd.AddCommand(grantPermCmd)
	userCmd.AddCommand(revokePermCmd)
}
