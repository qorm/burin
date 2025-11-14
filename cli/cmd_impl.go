package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/qorm/burin/client"
	"github.com/qorm/burin/client/interfaces"

	"github.com/spf13/cobra"
)

// Version command
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "显示版本信息",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Burin CLI v1.0.0")
	},
}

// Ping command
var pingCmd = &cobra.Command{
	Use:   "ping",
	Short: "测试连接",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := context.Background()
		start := time.Now()

		_, err := burinClient.GetClusterInfo(ctx)
		elapsed := time.Since(start)

		if err != nil {
			fmt.Printf("❌ PING 失败: %v (耗时: %v)\n", err, elapsed)
			os.Exit(1)
		}

		fmt.Printf("✅ PONG (耗时: %v)\n", elapsed)
	},
}

// Get command
var getCmd = &cobra.Command{
	Use:   "get <key>",
	Short: "获取键的值",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		key := args[0]
		start := time.Now()

		resp, err := burinClient.Get(key)
		elapsed := time.Since(start)

		if err != nil {
			fmt.Printf("❌ 获取失败: %v (耗时: %v)\n", err, elapsed)
			os.Exit(1)
		}

		if !resp.Found {
			fmt.Printf("(nil) (耗时: %v)\n", elapsed)
			return
		}

		fmt.Printf("%s\n", string(resp.Value))
		if resp.TTL > 0 {
			fmt.Printf("TTL: %v\n", resp.TTL)
		}
		fmt.Printf("(耗时: %v)\n", elapsed)
	},
}

// Set command
var (
	setTTL time.Duration
	setCmd = &cobra.Command{
		Use:   "set <key> <value>",
		Short: "设置键值对",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			key := args[0]
			value := []byte(args[1])
			start := time.Now()

			var opts []interfaces.CacheOption
			if setTTL > 0 {
				opts = append(opts, client.WithTTL(setTTL))
			}

			err := burinClient.Set(key, value, opts...)
			elapsed := time.Since(start)

			if err != nil {
				fmt.Printf("❌ 设置失败: %v (耗时: %v)\n", err, elapsed)
				os.Exit(1)
			}

			fmt.Printf("✅ OK (耗时: %v)\n", elapsed)
		},
	}
)

func init() {
	setCmd.Flags().DurationVar(&setTTL, "ttl", 0, "过期时间 (例如: 10s, 5m, 1h)")
}

// Delete command
var delCmd = &cobra.Command{
	Use:   "del <key>",
	Short: "删除键",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		key := args[0]
		start := time.Now()

		err := burinClient.Delete(key)
		elapsed := time.Since(start)

		if err != nil {
			fmt.Printf("❌ 删除失败: %v (耗时: %v)\n", err, elapsed)
			os.Exit(1)
		}

		fmt.Printf("✅ OK (耗时: %v)\n", elapsed)
	},
}

// Exists command
var existsCmd = &cobra.Command{
	Use:   "exists <key>",
	Short: "检查键是否存在",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		key := args[0]
		start := time.Now()

		exists, err := burinClient.Exists(key)
		elapsed := time.Since(start)

		if err != nil {
			fmt.Printf("❌ 检查失败: %v (耗时: %v)\n", err, elapsed)
			os.Exit(1)
		}

		if exists {
			fmt.Printf("✅ 存在 (耗时: %v)\n", elapsed)
		} else {
			fmt.Printf("❌ 不存在 (耗时: %v)\n", elapsed)
		}
	},
}

// List command
var (
	listPrefix string
	listLimit  int
	listOffset int
	listCmd    = &cobra.Command{
		Use:   "list",
		Short: "列出键",
		Run: func(cmd *cobra.Command, args []string) {
			start := time.Now()

			var opts []interfaces.CacheOption
			if listPrefix != "" {
				opts = append(opts, client.WithPrefix(listPrefix))
			}
			if listLimit > 0 {
				opts = append(opts, client.WithLimit(listLimit))
			}
			if listOffset > 0 {
				opts = append(opts, client.WithOffset(listOffset))
			}

			keys, total, err := burinClient.ListKeys(opts...)
			elapsed := time.Since(start)

			if err != nil {
				fmt.Printf("❌ 列出键失败: %v (耗时: %v)\n", err, elapsed)
				os.Exit(1)
			}

			if len(keys) == 0 {
				fmt.Printf("(空) (耗时: %v)\n", elapsed)
				return
			}

			for i, key := range keys {
				fmt.Printf("%d) %s\n", i+1, key)
			}
			fmt.Printf("\n总计: %d 个键 (耗时: %v)\n", total, elapsed)
		},
	}
)

func init() {
	listCmd.Flags().StringVarP(&listPrefix, "prefix", "p", "", "键前缀")
	listCmd.Flags().IntVarP(&listLimit, "limit", "l", 100, "最大返回数量")
	listCmd.Flags().IntVar(&listOffset, "offset", 0, "偏移量")
}

// Count command
var (
	countPrefix string
	countCmd    = &cobra.Command{
		Use:   "count",
		Short: "统计键数量",
		Run: func(cmd *cobra.Command, args []string) {
			start := time.Now()

			var opts []interfaces.CacheOption
			if countPrefix != "" {
				opts = append(opts, client.WithPrefix(countPrefix))
			}

			count, err := burinClient.CountKeys(opts...)
			elapsed := time.Since(start)

			if err != nil {
				fmt.Printf("❌ 统计失败: %v (耗时: %v)\n", err, elapsed)
				os.Exit(1)
			}

			fmt.Printf("键数量: %d (耗时: %v)\n", count, elapsed)
		},
	}
)

func init() {
	countCmd.Flags().StringVarP(&countPrefix, "prefix", "p", "", "键前缀")
}

// Geo commands
var geoCmd = &cobra.Command{
	Use:   "geo",
	Short: "地理位置操作",
}

var geoAddCmd = &cobra.Command{
	Use:   "add <key> <member> <lon> <lat>",
	Short: "添加地理位置",
	Args:  cobra.ExactArgs(4),
	Run: func(cmd *cobra.Command, args []string) {
		key := args[0]
		member := args[1]
		var lon, lat float64
		fmt.Sscanf(args[2], "%f", &lon)
		fmt.Sscanf(args[3], "%f", &lat)
		start := time.Now()

		members := []interfaces.GeoMember{
			{Name: member, Longitude: lon, Latitude: lat},
		}

		err := burinClient.GeoAdd(key, members)
		elapsed := time.Since(start)

		if err != nil {
			fmt.Printf("❌ 添加失败: %v (耗时: %v)\n", err, elapsed)
			os.Exit(1)
		}

		fmt.Printf("✅ OK (耗时: %v)\n", elapsed)
	},
}

var geoDistCmd = &cobra.Command{
	Use:   "dist <key> <member1> <member2> [unit]",
	Short: "计算两点距离",
	Args:  cobra.RangeArgs(3, 4),
	Run: func(cmd *cobra.Command, args []string) {
		key := args[0]
		member1 := args[1]
		member2 := args[2]
		unit := "m"
		if len(args) > 3 {
			unit = args[3]
		}
		start := time.Now()

		dist, err := burinClient.GeoDist(key, member1, member2, unit)
		elapsed := time.Since(start)

		if err != nil {
			fmt.Printf("❌ 计算失败: %v (耗时: %v)\n", err, elapsed)
			os.Exit(1)
		}

		fmt.Printf("距离: %.2f %s (耗时: %v)\n", dist, unit, elapsed)
	},
}

var geoRadiusCmd = &cobra.Command{
	Use:   "radius <key> <lon> <lat> <radius> [unit]",
	Short: "查询范围内的位置",
	Args:  cobra.RangeArgs(4, 5),
	Run: func(cmd *cobra.Command, args []string) {
		key := args[0]
		var lon, lat, radius float64
		fmt.Sscanf(args[1], "%f", &lon)
		fmt.Sscanf(args[2], "%f", &lat)
		fmt.Sscanf(args[3], "%f", &radius)
		unit := "m"
		if len(args) > 4 {
			unit = args[4]
		}
		start := time.Now()

		results, err := burinClient.GeoRadius(key, lon, lat, radius, unit)
		elapsed := time.Since(start)

		if err != nil {
			fmt.Printf("❌ 查询失败: %v (耗时: %v)\n", err, elapsed)
			os.Exit(1)
		}

		if len(results) == 0 {
			fmt.Printf("(空) (耗时: %v)\n", elapsed)
			return
		}

		for i, result := range results {
			fmt.Printf("%d) %s (距离: %.2f %s)\n", i+1, result.Name, result.Distance, unit)
		}
		fmt.Printf("(耗时: %v)\n", elapsed)
	},
}

var geoPosCmd = &cobra.Command{
	Use:   "pos <key> <member...>",
	Short: "获取位置坐标",
	Args:  cobra.MinimumNArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		key := args[0]
		members := args[1:]
		start := time.Now()

		positions, err := burinClient.GeoPos(key, members)
		elapsed := time.Since(start)

		if err != nil {
			fmt.Printf("❌ 获取失败: %v (耗时: %v)\n", err, elapsed)
			os.Exit(1)
		}

		for i, pos := range positions {
			if pos.Name != "" {
				fmt.Printf("%s: (%.6f, %.6f)\n", pos.Name, pos.Longitude, pos.Latitude)
			} else {
				fmt.Printf("%s: (nil)\n", members[i])
			}
		}
		fmt.Printf("(耗时: %v)\n", elapsed)
	},
}

func init() {
	geoCmd.AddCommand(geoAddCmd)
	geoCmd.AddCommand(geoDistCmd)
	geoCmd.AddCommand(geoRadiusCmd)
	geoCmd.AddCommand(geoPosCmd)
}

// Cluster command
var clusterCmd = &cobra.Command{
	Use:   "cluster",
	Short: "集群信息",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := context.Background()
		start := time.Now()

		info, err := burinClient.GetClusterInfo(ctx)
		elapsed := time.Since(start)

		if err != nil {
			fmt.Printf("❌ 获取集群信息失败: %v (耗时: %v)\n", err, elapsed)
			os.Exit(1)
		}

		fmt.Println("=== 集群信息 ===")
		for key, value := range info {
			fmt.Printf("%s: %v\n", key, value)
		}

		leader, err := burinClient.GetLeader(ctx)
		if err == nil {
			fmt.Printf("\nLeader: %s\n", leader)
		}
		fmt.Printf("(耗时: %v)\n", elapsed)
	},
}

// Health command
var healthCmd = &cobra.Command{
	Use:   "health",
	Short: "健康检查",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := context.Background()
		start := time.Now()

		info, err := burinClient.GetClusterInfo(ctx)
		elapsed := time.Since(start)

		if err != nil {
			fmt.Printf("❌ 健康检查失败: %v (耗时: %v)\n", err, elapsed)
			os.Exit(1)
		}

		fmt.Println("=== 健康状态 ===")

		if status, ok := info["status"]; ok {
			fmt.Printf("状态: %v\n", status)
		}
		if nodeID, ok := info["node_id"]; ok {
			fmt.Printf("节点ID: %v\n", nodeID)
		}
		if isLeader, ok := info["is_leader"]; ok {
			fmt.Printf("是否Leader: %v\n", isLeader)
		}
		if uptime, ok := info["uptime"]; ok {
			fmt.Printf("运行时间: %v\n", uptime)
		}

		if components, ok := info["components"].(map[string]interface{}); ok {
			fmt.Println("\n组件状态:")
			for name, status := range components {
				fmt.Printf("  %s: %v\n", name, status)
			}
		}

		fmt.Printf("\n✅ 健康 (耗时: %v)\n", elapsed)
	},
}
