package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/qorm/burin/cli/i18n"
	"github.com/qorm/burin/client"
	"github.com/qorm/burin/client/interfaces"

	"github.com/spf13/cobra"
)

// Version command
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Burin CLI v1.0.0")
	},
}

// Ping command
var pingCmd = &cobra.Command{
	Use:   "ping",
	Short: "Test connection",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := context.Background()
		start := time.Now()

		_, err := burinClient.GetClusterInfo(ctx)
		elapsed := time.Since(start)

		if err != nil {
			fmt.Printf(i18n.T(i18n.ErrPingFailed, err, elapsed) + "\n")
			os.Exit(1)
		}

		fmt.Printf("✅ PONG %s\n", i18n.T(i18n.MsgTime, elapsed))
	},
}

// Get command
var getCmd = &cobra.Command{
	Use:   "get <key>",
	Short: "Get value by key",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		key := args[0]
		start := time.Now()

		resp, err := burinClient.Get(key)
		elapsed := time.Since(start)

		if err != nil {
			fmt.Printf(i18n.T(i18n.ErrGetFailed, err, elapsed) + "\n")
			os.Exit(1)
		}

		if !resp.Found {
			fmt.Printf("%s %s\n", i18n.T(i18n.MsgNotFound), i18n.T(i18n.MsgTime, elapsed))
			return
		}

		fmt.Printf("%s\n", string(resp.Value))
		if resp.TTL > 0 {
			fmt.Printf("TTL: %v\n", resp.TTL)
		}
		fmt.Printf("%s\n", i18n.T(i18n.MsgTime, elapsed))
	},
}

// Set command
var (
	setTTL time.Duration
	setCmd = &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set key-value pair",
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
				fmt.Printf(i18n.T(i18n.ErrSetFailed, err, elapsed) + "\n")
				os.Exit(1)
			}

			fmt.Printf("%s %s\n", i18n.T(i18n.MsgOK), i18n.T(i18n.MsgTime, elapsed))
		},
	}
)

func init() {
	setCmd.Flags().DurationVar(&setTTL, "ttl", 0, "TTL duration")
}

// Delete command
var delCmd = &cobra.Command{
	Use:   "del <key>",
	Short: "Delete key",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		key := args[0]
		start := time.Now()

		err := burinClient.Delete(key)
		elapsed := time.Since(start)

		if err != nil {
			fmt.Printf(i18n.T(i18n.ErrDelFailed, err, elapsed) + "\n")
			os.Exit(1)
		}

		fmt.Printf("%s %s\n", i18n.T(i18n.MsgOK), i18n.T(i18n.MsgTime, elapsed))
	},
}

// Exists command
var existsCmd = &cobra.Command{
	Use:   "exists <key>",
	Short: "Check if key exists",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		key := args[0]
		start := time.Now()

		exists, err := burinClient.Exists(key)
		elapsed := time.Since(start)

		if err != nil {
			fmt.Printf(i18n.T(i18n.ErrExistsFailed, err, elapsed) + "\n")
			os.Exit(1)
		}

		if exists {
			fmt.Printf("%s %s\n", i18n.T(i18n.MsgExists), i18n.T(i18n.MsgTime, elapsed))
		} else {
			fmt.Printf("%s %s\n", i18n.T(i18n.MsgNotExists), i18n.T(i18n.MsgTime, elapsed))
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
		Short: "List keys",
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
				fmt.Printf(i18n.T(i18n.ErrListFailed, err, elapsed) + "\n")
				os.Exit(1)
			}

			if len(keys) == 0 {
				fmt.Printf("%s %s\n", i18n.T(i18n.MsgEmpty), i18n.T(i18n.MsgTime, elapsed))
				return
			}

			for i, key := range keys {
				fmt.Printf("%d) %s\n", i+1, key)
			}
			fmt.Printf("\n%s %s\n", i18n.T(i18n.MsgTotal, total), i18n.T(i18n.MsgTime, elapsed))
		},
	}
)

func init() {
	listCmd.Flags().StringVarP(&listPrefix, "prefix", "p", "", "Key prefix")
	listCmd.Flags().IntVarP(&listLimit, "limit", "l", 100, "Max results")
	listCmd.Flags().IntVar(&listOffset, "offset", 0, "Offset")
}

// Count command
var (
	countPrefix string
	countCmd    = &cobra.Command{
		Use:   "count",
		Short: "Count keys",
		Run: func(cmd *cobra.Command, args []string) {
			start := time.Now()

			var opts []interfaces.CacheOption
			if countPrefix != "" {
				opts = append(opts, client.WithPrefix(countPrefix))
			}

			count, err := burinClient.CountKeys(opts...)
			elapsed := time.Since(start)

			if err != nil {
				fmt.Printf(i18n.T(i18n.ErrCountFailed, err, elapsed) + "\n")
				os.Exit(1)
			}

			fmt.Printf("%s %s\n", i18n.T(i18n.MsgTotal, count), i18n.T(i18n.MsgTime, elapsed))
		},
	}
)

func init() {
	countCmd.Flags().StringVarP(&countPrefix, "prefix", "p", "", "Key prefix")
}

// Geo commands
var geoCmd = &cobra.Command{
	Use:   "geo",
	Short: "Geographic operations",
}

var geoAddCmd = &cobra.Command{
	Use:   "add <key> <member> <lon> <lat>",
	Short: "Add geographic location",
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
			fmt.Printf(i18n.T(i18n.ErrGeoAddFailed, err, elapsed) + "\n")
			os.Exit(1)
		}

		fmt.Printf("%s %s\n", i18n.T(i18n.MsgOK), i18n.T(i18n.MsgTime, elapsed))
	},
}

var geoDistCmd = &cobra.Command{
	Use:   "dist <key> <member1> <member2> [unit]",
	Short: "Calculate distance",
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
			fmt.Printf(i18n.T(i18n.ErrGeoDistFailed, err, elapsed) + "\n")
			os.Exit(1)
		}

		fmt.Printf("%s %s\n", i18n.T(i18n.MsgDistance, dist, unit), i18n.T(i18n.MsgTime, elapsed))
	},
}

var geoRadiusCmd = &cobra.Command{
	Use:   "radius <key> <lon> <lat> <radius> [unit]",
	Short: i18n.T(i18n.CmdGeoRadius),
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
			fmt.Printf(i18n.T(i18n.ErrGeoRadiusFailed, err, elapsed) + "\n")
			os.Exit(1)
		}

		if len(results) == 0 {
			fmt.Printf("%s %s\n", i18n.T(i18n.MsgEmpty), i18n.T(i18n.MsgTime, elapsed))
			return
		}

		for i, result := range results {
			fmt.Printf("%d) %s (%s)\n", i+1, result.Name, i18n.T(i18n.MsgDistance, result.Distance, unit))
		}
		fmt.Printf("%s\n", i18n.T(i18n.MsgTime, elapsed))
	},
}

var geoPosCmd = &cobra.Command{
	Use:   "pos <key> <member...>",
	Short: i18n.T(i18n.CmdGeoPos),
	Args:  cobra.MinimumNArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		key := args[0]
		members := args[1:]
		start := time.Now()

		positions, err := burinClient.GeoPos(key, members)
		elapsed := time.Since(start)

		if err != nil {
			fmt.Printf(i18n.T(i18n.ErrGeoPosFailed, err, elapsed) + "\n")
			os.Exit(1)
		}

		for i, pos := range positions {
			if pos.Name != "" {
				fmt.Printf("%s: (%.6f, %.6f)\n", pos.Name, pos.Longitude, pos.Latitude)
			} else {
				fmt.Printf("%s: %s\n", members[i], i18n.T(i18n.MsgNotFound))
			}
		}
		fmt.Printf("%s\n", i18n.T(i18n.MsgTime, elapsed))
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
	Short: i18n.T(i18n.CmdCluster),
	Run: func(cmd *cobra.Command, args []string) {
		ctx := context.Background()
		start := time.Now()

		info, err := burinClient.GetClusterInfo(ctx)
		elapsed := time.Since(start)

		if err != nil {
			fmt.Printf(i18n.T(i18n.ErrClusterFailed, err, elapsed) + "\n")
			os.Exit(1)
		}

		fmt.Println(i18n.T(i18n.ClusterInfo))
		for key, value := range info {
			fmt.Printf("%s: %v\n", key, value)
		}

		leader, err := burinClient.GetLeader(ctx)
		if err == nil {
			fmt.Printf("\n%s\n", i18n.T(i18n.ClusterLeader, leader))
		}
		fmt.Printf("%s\n", i18n.T(i18n.MsgTime, elapsed))
	},
}

// Health command
var healthCmd = &cobra.Command{
	Use:   "health",
	Short: i18n.T(i18n.CmdHealth),
	Run: func(cmd *cobra.Command, args []string) {
		ctx := context.Background()
		start := time.Now()

		info, err := burinClient.GetClusterInfo(ctx)
		elapsed := time.Since(start)

		if err != nil {
			fmt.Printf(i18n.T(i18n.ErrHealthFailed, err, elapsed) + "\n")
			os.Exit(1)
		}

		fmt.Println("=== " + i18n.T(i18n.CmdHealth) + " ===")

		if status, ok := info["status"]; ok {
			fmt.Println(i18n.T(i18n.HealthStatus, status))
		}
		if nodeID, ok := info["node_id"]; ok {
			fmt.Println(i18n.T(i18n.HealthNodeID, nodeID))
		}
		if isLeader, ok := info["is_leader"]; ok {
			fmt.Println(i18n.T(i18n.HealthIsLeader, isLeader))
		}
		if uptime, ok := info["uptime"]; ok {
			fmt.Println(i18n.T(i18n.HealthUptime, uptime))
		}

		if components, ok := info["components"].(map[string]interface{}); ok {
			fmt.Println("\n" + i18n.T(i18n.HealthComponents))
			for name, status := range components {
				fmt.Printf("  %s: %v\n", name, status)
			}
		}

		fmt.Printf("\n✅ %s %s\n", i18n.T(i18n.CmdHealth), i18n.T(i18n.MsgTime, elapsed))
	},
}
