package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/qorm/burin/client"

	"github.com/sirupsen/logrus"
)

const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorPurple = "\033[35m"
	colorCyan   = "\033[36m"
	colorWhite  = "\033[37m"
)

var (
	logger      *logrus.Logger
	burinPath   string
	testResults []TestResult
	burinClient *client.BurinClient
	clientPool  *client.ClientPool // 添加客户端连接池

	// 性能统计
	totalTestStartTime time.Time
	performanceMetrics PerformanceMetrics

	// 子步骤时间跟踪
	subStepStartTime time.Time
	lastSubStepName  string

	// 节点配置（注意：客户端现在只连接单个节点，以下变量用于节点启动和状态检查）
	node1Endpoint = "127.0.0.1:8099"
	node2Endpoint = "127.0.0.1:8090"
	node3Endpoint = "127.0.0.1:8199"
	node4Endpoint = "127.0.0.1:8191" // 第四个节点

	// 所有节点端点列表（用于启动和检查集群状态，但客户端只连接 node1）
	allEndpoints = []string{node1Endpoint, node2Endpoint, node3Endpoint}

	// 包含第四个节点的所有端点
	allEndpointsWithNode4 = []string{node1Endpoint, node2Endpoint, node3Endpoint, node4Endpoint}

	// 命令行参数
	testList    = flag.String("tests", "all", "要运行的测试，用逗号分隔 (例如: test1,test2,test5) 或 'all' 运行全部")
	showHelp    = flag.Bool("help", false, "显示帮助信息")
	listTests   = flag.Bool("list", false, "列出所有可用的测试")
	skipCleanup = flag.Bool("skip-cleanup", false, "跳过测试数据清理")
	rootPath    = flag.String("root", "", "Burin 项目根目录（如果不指定，将自动检测）")
)

type TestResult struct {
	Name       string
	Success    bool
	Message    string
	Duration   time.Duration
	OpsCount   int64         // 操作总数
	Throughput float64       // 吞吐量 (ops/sec)
	AvgLatency time.Duration // 平均延迟
	MinLatency time.Duration // 最小延迟
	MaxLatency time.Duration // 最大延迟
}

type PerformanceMetrics struct {
	TotalOps        int64
	TotalDuration   time.Duration
	TotalThroughput float64
	AvgThroughput   float64
}

func init() {
	logger = logrus.New()
	logger.SetLevel(logrus.WarnLevel) // 只显示警告和错误，减少输出噪音
	logger.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})
	logger.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})
}

func main() {
	flag.Parse()

	// 获取burin项目根目录
	if *rootPath != "" {
		burinPath = *rootPath
	} else {
		currentDir, _ := os.Getwd()
		burinPath = filepath.Join(currentDir, "..")
	}

	// 显示帮助信息
	if *showHelp {
		printUsage()
		os.Exit(0)
	}

	// 列出所有测试
	if *listTests {
		printAvailableTests()
		os.Exit(0)
	}

	printBanner()

	// 记录测试开始时间
	totalTestStartTime = time.Now()

	// 步骤1：验证集群状态
	if !verifyClusterStatus() {
		printError("集群状态验证失败 - 请确保三节点集群已启动")
		printInfo("提示: 在 burin 目录运行 './start.sh start node1,node2,node3' 启动集群")
		os.Exit(1)
	}

	// 步骤2：初始化客户端
	if !initBurinClient() {
		printError("Burin 客户端初始化失败")
		os.Exit(1)
	}
	defer func() {
		if burinClient != nil {
			burinClient.Disconnect()
		}
		if clientPool != nil {
			clientPool.Close()
		}
	}()

	// 步骤3：运行测试
	runSelectedTests()

	// 步骤4：打印测试结果
	printTestSummary()
}

func printUsage() {
	fmt.Println("Burin 测试套件")
	fmt.Println()
	fmt.Println("用法:")
	fmt.Println("  ./complete_test [选项]")
	fmt.Println()
	fmt.Println("选项:")
	fmt.Println("  -tests string")
	fmt.Println("        要运行的测试，用逗号分隔 (例如: test1,test2,test5) 或 'all' 运行全部 (默认 \"all\")")
	fmt.Println("  -list")
	fmt.Println("        列出所有可用的测试")
	fmt.Println("  -skip-cleanup")
	fmt.Println("        跳过测试数据清理")
	fmt.Println("  -help")
	fmt.Println("        显示此帮助信息")
	fmt.Println()
	fmt.Println("示例:")
	fmt.Println("  ./complete_test                          # 运行所有测试")
	fmt.Println("  ./complete_test -tests test1,test2       # 只运行 test1 和 test2")
	fmt.Println("  ./complete_test -tests test5 -skip-cleanup  # 运行 test5 并跳过清理")
	fmt.Println("  ./complete_test -list                    # 列出所有测试")
	fmt.Println()
	fmt.Println("性能展示功能:")
	fmt.Println("  • 实时进度条 - 显示操作进度和吞吐量")
	fmt.Println("  • 详细指标 - 操作数、吞吐量、延迟统计")
	fmt.Println("  • 性能汇总 - 总体性能统计和对比图表")
	fmt.Println("  • 推荐测试: test2 (批量), test6 (并发), test7 (大值)")
	fmt.Println()
	fmt.Println("查看性能指南: cat PERFORMANCE_GUIDE.md")
	fmt.Println("使用演示脚本: ./run_performance_demo.sh")
	fmt.Println()
	fmt.Println("注意: 运行测试前请确保三节点集群已启动:")
	fmt.Println("  cd ../.. && ./start.sh start node1,node2,node3")
}

func printAvailableTests() {
	fmt.Println("可用的测试列表:")
	fmt.Println()
	fmt.Println("  test1   - 基本缓存操作 (SET/GET/DELETE/EXISTS)")
	fmt.Println("  test3   - 数据一致性验证")
	fmt.Println("  test4   - TTL过期测试")
	fmt.Println("  test6   - 并发操作测试 ⚡ [性能展示]")
	fmt.Println("  test7   - 大数据量测试 ⚡ [性能展示]")
	fmt.Println("  test9   - 集群数据同步测试")
	fmt.Println("  test10  - 错误处理测试")
	fmt.Println("  test11  - 新节点加入同步测试")
	fmt.Println("  test12  - 基本事务操作 (开始/提交/读写) 🔄")
	fmt.Println("  test13  - 事务回滚测试 🔄")
	fmt.Println("  test14  - 事务隔离级别测试 🔄")
	fmt.Println("  test15  - 并发事务测试 🔄⚡")
	fmt.Println("  test16  - 事务超时测试 🔄")
	fmt.Println("  test17  - 用户管理基础功能 🔐")
	fmt.Println("  test18  - 认证流程验证 🔐")
	fmt.Println("  test19  - 基于角色的访问控制 🔐")
	fmt.Println("  test20  - 用户创建和管理 🔑")
	fmt.Println("  test21  - 权限授予和撤销 🔑")
	fmt.Println("  test22  - 数据库访问控制 🔑")
	fmt.Println("  test23  - 密码管理 🔑")
	fmt.Println("  test24  - 授权系统集成测试 🔑")
	fmt.Println("  testdb  - 数据库管理操作 💾")
	fmt.Println("  testgeo - 地理位置操作测试 (GeoAdd/GeoDist/GeoRadius)")
	fmt.Println()
	fmt.Println("⚡ 带性能展示的测试包含:")
	fmt.Println("   • 实时进度条显示")
	fmt.Println("   • 详细性能指标 (吞吐量、延迟)")
	fmt.Println("   • 性能统计汇总")
	fmt.Println()
	fmt.Println("🔄 事务测试包含:")
	fmt.Println("   • ACID 事务支持")
	fmt.Println("   • 多种隔离级别")
	fmt.Println("   • 并发事务处理")
	fmt.Println("   • 超时和回滚机制")
	fmt.Println()
	fmt.Println("🔐 用户管理和认证测试包含:")
	fmt.Println("   • 用户认证和权限验证")
	fmt.Println("   • 基于角色的访问控制")
	fmt.Println("   • 数据库级别权限管理")
	fmt.Println()
	fmt.Println("🔑 授权系统测试包含:")
	fmt.Println("   • 用户创建和生命周期管理")
	fmt.Println("   • 权限授予和撤销机制")
	fmt.Println("   • 数据库访问控制验证")
	fmt.Println("   • 密码安全管理")
	fmt.Println("   • 完整的授权流程集成")
	fmt.Println()
	fmt.Println("💾 数据库管理测试包含:")
	fmt.Println("   • 数据库创建和基本操作")
	fmt.Println("   • 系统数据库保护（底层实现）")
	fmt.Println("   • 权限隔离验证")
	fmt.Println("   • 有权限用户的数据库删除保护")
	fmt.Println("   • 安全认证机制测试")
	fmt.Println()
	fmt.Println("使用 -tests 参数指定要运行的测试，例如:")
	fmt.Println("  ./test_suite -tests test1,test3,test4")
	fmt.Println("  ./test_suite -tests test6,test7                         # 运行所有性能测试")
	fmt.Println("  ./test_suite -tests test12,test13,test14,test15,test16  # 运行所有事务测试")
	fmt.Println("  ./test_suite -tests test17,test18,test19                # 运行所有认证测试")
	fmt.Println("  ./test_suite -tests testauth                            # 运行所有授权测试(test20-24)")
	fmt.Println("  ./test_suite -tests testdb                              # 运行数据库管理测试")
	fmt.Println("  ./test_suite -tests testgeo                             # 运行GEO测试")
	fmt.Println()
	fmt.Println("更多信息:")
	fmt.Println("  性能指南: cat PERFORMANCE_GUIDE.md")
	fmt.Println("  演示脚本: ./run_performance_demo.sh")
}

func runSelectedTests() {
	testsToRun := parseTestList(*testList)

	if len(testsToRun) == 0 {
		printError("没有找到要运行的测试")
		return
	}

	printSection(fmt.Sprintf("运行 %d 个测试", len(testsToRun)))

	for _, testName := range testsToRun {
		switch testName {
		case "test1":
			test1_BasicCacheOperations()
		case "test2":
			test2_BatchOperations()
		case "test3":
			test3_DataConsistency()
		case "test4":
			test4_TTLExpiration()
		case "test6":
			test6_ConcurrentOperations()
		case "test7":
			test7_LargeValueHandling()
		case "test9":
			test9_ClusterDataSync()
		case "test10":
			test10_ErrorHandling()
		case "test11":
			test11_NewNodeJoinAndSync()
		case "test12":
			test12_BasicTransactions()
		case "test13":
			test13_TransactionRollback()
		case "test14":
			test14_TransactionIsolation()
		case "test15":
			test15_ConcurrentTransactions()
		case "test16":
			test16_TransactionTimeout()
		case "test17":
			test17_UserManagement()
		case "test18":
			test18_AuthenticationFlow()
		case "test19":
			test19_RoleBasedAccess()
		case "test20":
			test20_UserCreationAndManagement()
		case "test21":
			test21_PermissionGrantAndRevoke()
		case "test22":
			test22_DatabaseAccessControl()
		case "test23":
			test23_PasswordManagement()
		case "test24":
			test24_AuthorizationIntegration()
		case "testdb":
			testDB_Operations()
		case "testauth":
			// 运行所有授权相关测试
			test20_UserCreationAndManagement()
			test21_PermissionGrantAndRevoke()
			test22_DatabaseAccessControl()
			test23_PasswordManagement()
			test24_AuthorizationIntegration()
		case "testgeo":
			testGeo_AdvancedOperations() // 先执行高级测试（包含数据库创建）
			testGeo_BasicOperations()
			testGeo_EdgeCases()
		default:
			printWarning(fmt.Sprintf("未知的测试: %s", testName))
		}
	}
}

func parseTestList(testStr string) []string {
	if testStr == "all" {
		return []string{
			"test1", "test3", "test4", "test6",
			"test7", "test9", "test10", "test11",
			"test12", "test13", "test14", "test15", "test16",
			"test17", "test18", "test19",
			"testgeo",
		}
	}

	tests := strings.Split(testStr, ",")
	result := make([]string, 0, len(tests))
	for _, test := range tests {
		test = strings.TrimSpace(test)
		if test != "" {
			result = append(result, test)
		}
	}
	return result
}

func printBanner() {
	banner := `
╔════════════════════════════════════════════════════════════════════════════╗
║                 Burin 完整功能测试套件                                     ║
║           使用 Burin Client 进行全面的功能验证测试                         ║
╚════════════════════════════════════════════════════════════════════════════╝
`
	fmt.Println(colorCyan + banner + colorReset)
	fmt.Println(colorBlue + "测试时间: " + time.Now().Format("2006-01-02 15:04:05") + colorReset)
	fmt.Println()
}

func printSection(title string) {
	fmt.Println()
	fmt.Println(colorPurple + strings.Repeat("━", 80) + colorReset)
	fmt.Println(colorPurple + title + colorReset)
	fmt.Println(colorPurple + strings.Repeat("━", 80) + colorReset)
}

func printInfo(message string) {
	fmt.Println(colorBlue + "ℹ " + message + colorReset)
}

func printSuccess(message string) {
	// 如果有正在进行的子步骤，显示其执行时间
	if !subStepStartTime.IsZero() {
		elapsed := time.Since(subStepStartTime)
		fmt.Printf(colorGreen+"✓ %s "+colorYellow+"[%v]"+colorReset+"\n", message, elapsed.Round(time.Microsecond))
		subStepStartTime = time.Time{} // 重置
	} else {
		fmt.Println(colorGreen + "✓ " + message + colorReset)
	}
}

func printSuccessWithTime(message string, elapsed time.Duration) {
	fmt.Printf(colorGreen+"✓ %s "+colorYellow+"[%v]"+colorReset+"\n", message, elapsed.Round(time.Microsecond))
}

func printWarning(message string) {
	fmt.Println(colorYellow + "⚠ " + message + colorReset)
}

func printError(message string) {
	fmt.Println(colorRed + "✗ " + message + colorReset)
}

func printTestHeader(title string) {
	fmt.Println()
	fmt.Println(colorBlue + strings.Repeat("=", 80) + colorReset)
	fmt.Println(colorBlue + title + colorReset)
	fmt.Println(colorBlue + strings.Repeat("=", 80) + colorReset)

	// 重置子步骤计时
	subStepStartTime = time.Time{}
	lastSubStepName = ""
}

func printSubTest(title string) {
	// 如果有上一个子步骤还没结束，显示其执行时间
	if !subStepStartTime.IsZero() && lastSubStepName != "" {
		elapsed := time.Since(subStepStartTime)
		fmt.Printf(colorYellow+"  ⏱ %s 耗时: %v"+colorReset+"\n", lastSubStepName, elapsed.Round(time.Microsecond))
	}

	fmt.Println()
	fmt.Println(colorCyan + "  → " + title + colorReset)

	// 记录新的子步骤开始时间
	subStepStartTime = time.Now()
	lastSubStepName = title
}

func recordTest(name string, success bool, message string, duration time.Duration) {
	testResults = append(testResults, TestResult{
		Name:     name,
		Success:  success,
		Message:  message,
		Duration: duration,
	})
}

// 记录测试结果（包含性能指标）
func recordTestWithMetrics(name string, success bool, message string, duration time.Duration,
	opsCount int64, avgLatency, minLatency, maxLatency time.Duration) {
	throughput := 0.0
	if duration.Seconds() > 0 {
		throughput = float64(opsCount) / duration.Seconds()
	}

	testResults = append(testResults, TestResult{
		Name:       name,
		Success:    success,
		Message:    message,
		Duration:   duration,
		OpsCount:   opsCount,
		Throughput: throughput,
		AvgLatency: avgLatency,
		MinLatency: minLatency,
		MaxLatency: maxLatency,
	})

	// 更新全局性能统计
	performanceMetrics.TotalOps += opsCount
	performanceMetrics.TotalDuration += duration
	performanceMetrics.TotalThroughput += throughput
}

// 打印性能进度条
func printPerformanceBar(current, total int64, startTime time.Time, label string) {
	elapsed := time.Since(startTime)
	percent := float64(current) / float64(total) * 100

	barLength := 40
	filledLength := int(float64(barLength) * percent / 100)

	bar := strings.Repeat("█", filledLength) + strings.Repeat("░", barLength-filledLength)

	throughput := float64(current) / elapsed.Seconds()

	fmt.Printf("\r%s [%s] %.1f%% | %d/%d ops | %.0f ops/s | 耗时: %v",
		label, bar, percent, current, total, throughput, elapsed.Truncate(time.Millisecond))

	if current >= total {
		fmt.Println()
	}
}

// 清理环境
func cleanEnvironment() bool {
	printSection("步骤1: 清理环境")

	// 停止所有节点
	printInfo("停止所有现有节点...")
	cmd := exec.Command("bash", "-c", fmt.Sprintf("cd %s && ./start.sh stop", burinPath))
	if err := cmd.Run(); err != nil {
		printWarning("停止节点时出现问题，可能没有运行的节点")
	}

	time.Sleep(2 * time.Second)

	// 清空数据目录
	printInfo("清空数据目录...")
	dataDir := filepath.Join(burinPath, "data")
	if err := os.RemoveAll(dataDir); err != nil {
		printError(fmt.Sprintf("清空数据目录失败: %v", err))
		return false
	}

	printSuccess("环境清理完成")
	return true
}

// 启动三节点集群
func startThreeNodeCluster() bool {
	printSection("步骤2: 启动三节点集群")

	printInfo("启动 Node1, Node2, Node3...")
	cmd := exec.Command("bash", "-c",
		fmt.Sprintf("cd %s && ./start.sh start node1,node2,node3", burinPath))

	output, err := cmd.CombinedOutput()
	if err != nil {
		printError(fmt.Sprintf("启动集群失败: %v", err))
		printError(string(output))
		return false
	}

	printInfo("等待集群完全启动...")
	time.Sleep(10 * time.Second)

	printSuccess("三节点集群启动成功")
	return true
}

// 验证集群状态
func verifyClusterStatus() bool {
	printSection("步骤3: 验证集群状态")

	// 检查每个节点是否可以连接
	for i, endpoint := range allEndpoints {
		printInfo(fmt.Sprintf("检查 Node%d (%s)...", i+1, endpoint))

		if !checkNodeAlive(endpoint) {
			printError(fmt.Sprintf("Node%d 不可访问", i+1))
			return false
		}

		printSuccess(fmt.Sprintf("Node%d 运行正常", i+1))
	}

	printSuccess("所有节点状态正常")
	return true
}

// 检查节点是否存活
func checkNodeAlive(endpoint string) bool {
	// 使用简单的TCP连接检查
	conn, err := net.DialTimeout("tcp", endpoint, 3*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// 初始化 Burin 客户端
func initBurinClient() bool {
	printSection("步骤4: 初始化 Burin 客户端")

	config := client.NewDefaultConfig()

	// 配置大值支持（用于测试）
	config.Cache.MaxValueSize = 20 * 1024 * 1024      // 20MB
	config.Connection.ReadTimeout = 60 * time.Second  // 增加读取超时
	config.Connection.WriteTimeout = 60 * time.Second // 增加写入超时

	// 配置端点 - 尝试连接多个节点，找到 leader
	// 优先尝试 node1, node2, node3
	endpoints := []string{node1Endpoint, node2Endpoint, node3Endpoint}
	var selectedEndpoint string
	var bc *client.BurinClient
	var lastErr error

	for _, endpoint := range endpoints {
		config.Connection.Endpoint = endpoint
		printInfo(fmt.Sprintf("尝试连接端点: %s", endpoint))

		// 配置认证信息（会自动登录）
		config.Auth.Username = "burin"
		config.Auth.Password = "burin@secret"

		// 设置日志级别
		config.Logging.Level = "warn" // 只显示警告和错误

		// 创建客户端
		tempClient, err := client.NewClient(config)
		if err != nil {
			lastErr = err
			continue
		}

		// 连接到服务器（会自动为每个连接执行登录）
		if err := tempClient.Connect(); err != nil {
			lastErr = err
			continue
		}

		// 连接成功
		bc = tempClient
		selectedEndpoint = endpoint
		break
	}

	if bc == nil {
		printError(fmt.Sprintf("无法连接到任何节点: %v", lastErr))
		return false
	}

	burinClient = bc
	printSuccess(fmt.Sprintf("Burin 客户端连接成功（端点: %s，已自动认证）", selectedEndpoint))

	// 创建客户端连接池用于并发测试
	printInfo("创建客户端连接池（用于并发测试）...")
	pool, err := client.NewClientPool(
		config,
		20,             // 最大20个连接
		5,              // 最小5个连接
		5*time.Minute,  // 空闲超时
		30*time.Minute, // 最大生命周期
	)
	if err != nil {
		printWarning(fmt.Sprintf("创建连接池失败: %v（并发测试可能受影响）", err))
	} else {
		clientPool = pool
		printSuccess("客户端连接池创建成功")
	}

	return true
} // 停止集群
func stopCluster() {
	printInfo("停止集群...")
	cmd := exec.Command("bash", "-c", fmt.Sprintf("cd %s && ./start.sh stop", burinPath))
	cmd.Run()
	time.Sleep(2 * time.Second)
}

// 运行完整测试套件
func runCompleteTestSuite() {
	printSection("步骤5: 运行完整测试套件")

	// 测试1: 基本缓存操作
	test1_BasicCacheOperations()

	// 测试3: 数据一致性
	test3_DataConsistency()

	// 测试4: TTL过期
	test4_TTLExpiration()

	// 测试6: 并发操作
	test6_ConcurrentOperations()

	// 测试7: 大值处理
	test7_LargeValueHandling()

	// 测试9: 集群数据同步
	test9_ClusterDataSync()

	// 测试10: 错误处理
	test10_ErrorHandling()

	// 测试11: 新节点加入和数据同步
	test11_NewNodeJoinAndSync()

	// 测试12: 事务功能
	// TODO: 事务功能需要使用 execute 模式，暂时跳过
	// test12_TransactionOperations()
	printInfo("跳过测试12: 事务功能（需要重构为 execute 模式）")
}

// 打印测试结果汇总
func printTestSummary() {
	printSection("步骤6: 测试结果汇总")

	totalTestDuration := time.Since(totalTestStartTime)

	passed := 0
	failed := 0

	fmt.Println()
	fmt.Println("详细结果:")
	fmt.Println(strings.Repeat("=", 80))

	for i, result := range testResults {
		fmt.Printf("\n[%d] %s\n", i+1, result.Name)
		fmt.Printf("    耗时: %v\n", result.Duration)

		// 显示性能指标（如果有）
		if result.OpsCount > 0 {
			fmt.Printf("    操作数: %d\n", result.OpsCount)
			fmt.Printf("    吞吐量: %.2f ops/sec\n", result.Throughput)
			if result.AvgLatency > 0 {
				fmt.Printf("    平均延迟: %v\n", result.AvgLatency)
				fmt.Printf("    延迟范围: %v ~ %v\n", result.MinLatency, result.MaxLatency)
			}
		}

		if result.Success {
			fmt.Printf("    状态: %s✓ 通过%s\n", colorGreen, colorReset)
			passed++
		} else {
			fmt.Printf("    状态: %s✗ 失败%s\n", colorRed, colorReset)
			fmt.Printf("    原因: %s\n", result.Message)
			failed++
		}
	}

	fmt.Println()
	fmt.Println(strings.Repeat("=", 80))

	// 性能统计汇总
	if performanceMetrics.TotalOps > 0 {
		fmt.Println()
		fmt.Println(colorCyan + "性能统计汇总" + colorReset)
		fmt.Println(strings.Repeat("-", 80))
		fmt.Printf("总操作数:     %d\n", performanceMetrics.TotalOps)
		fmt.Printf("总耗时:       %v\n", totalTestDuration.Truncate(time.Millisecond))

		overallThroughput := float64(performanceMetrics.TotalOps) / totalTestDuration.Seconds()
		fmt.Printf("总体吞吐量:   %.2f ops/sec\n", overallThroughput)

		testsWithMetrics := 0
		for _, result := range testResults {
			if result.OpsCount > 0 {
				testsWithMetrics++
			}
		}
		if testsWithMetrics > 0 {
			avgThroughput := performanceMetrics.TotalThroughput / float64(testsWithMetrics)
			fmt.Printf("平均吞吐量:   %.2f ops/sec\n", avgThroughput)
		}

		// 性能可视化条形图
		fmt.Println()
		fmt.Println("吞吐量对比:")
		printPerformanceChart(testResults)
	}

	fmt.Println()
	fmt.Println(strings.Repeat("=", 80))
	fmt.Printf("\n总计: %d 个测试\n", len(testResults))
	fmt.Printf("%s通过: %d%s\n", colorGreen, passed, colorReset)
	if failed > 0 {
		fmt.Printf("%s失败: %d%s\n", colorRed, failed, colorReset)
	}

	successRate := float64(passed) / float64(len(testResults)) * 100
	fmt.Printf("\n成功率: %.1f%%\n", successRate)
	fmt.Printf("总耗时: %v\n", totalTestDuration.Truncate(time.Millisecond))

	if failed == 0 {
		fmt.Printf("\n%s🎉 所有测试通过！%s\n", colorGreen, colorReset)
	} else {
		fmt.Printf("\n%s⚠️  有测试失败，请检查日志%s\n", colorYellow, colorReset)
	}
}

// 打印性能对比图表
func printPerformanceChart(results []TestResult) {
	// 找出最大吞吐量用于缩放
	maxThroughput := 0.0
	for _, result := range results {
		if result.Throughput > maxThroughput {
			maxThroughput = result.Throughput
		}
	}

	if maxThroughput == 0 {
		return
	}

	const maxBarLength = 50
	for _, result := range results {
		if result.OpsCount == 0 {
			continue
		}

		barLength := int(float64(maxBarLength) * result.Throughput / maxThroughput)
		bar := strings.Repeat("█", barLength)

		// 截取测试名称，保证对齐
		testName := result.Name
		if len(testName) > 25 {
			testName = testName[:22] + "..."
		}

		fmt.Printf("  %-25s %s %.2f ops/s\n", testName, bar, result.Throughput)
	}
}
