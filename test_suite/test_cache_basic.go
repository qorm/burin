package main

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/qorm/burin/cid"
)

func test1_BasicCacheOperations() {
	printTestHeader("测试1: 基本缓存操作")
	startTime := time.Now()

	// 子测试1: SET操作
	printSubTest("1.1 SET操作")
	testKey := "test:basic:" + cid.Generate()
	testValue := []byte("Hello Burin!")

	if err := cacheSet(testKey, testValue, 0); err != nil {
		recordTest("基本缓存操作-SET", false, fmt.Sprintf("SET失败: %v", err), time.Since(startTime))
		return
	}
	printSuccess("SET操作成功")

	// 子测试2: GET操作（从leader读取，强一致性）
	printSubTest("1.2 GET操作（从leader读取）")
	getValue, err := cacheGetFromLeaderWithRetry(testKey, 0, 10)
	if err != nil {
		recordTest("基本缓存操作-GET-Leader", false, fmt.Sprintf("从Leader GET失败: %v", err), time.Since(startTime))
		return
	}
	if !bytes.Equal(getValue, testValue) {
		recordTest("基本缓存操作-GET-Leader", false,
			fmt.Sprintf("Leader GET值不匹配，期望: %s, 实际: %s", testValue, getValue), time.Since(startTime))
		return
	}
	printSuccess("从Leader GET操作成功，值匹配")

	// 等待Raft同步到所有节点
	printInfo("等待数据同步到所有follower节点...")
	time.Sleep(2 * time.Second)

	// 子测试3: GET操作（从follower读取）
	printSubTest("1.3 GET操作（从follower读取）")
	getValue, err = cacheGet(testKey)
	if err != nil {
		recordTest("基本缓存操作-GET-Follower", false, fmt.Sprintf("从Follower GET失败: %v", err), time.Since(startTime))
		return
	}
	if !bytes.Equal(getValue, testValue) {
		recordTest("基本缓存操作-GET-Follower", false,
			fmt.Sprintf("Follower GET值不匹配，期望: %s, 实际: %s", testValue, getValue), time.Since(startTime))
		return
	}
	printSuccess("从Follower GET操作成功，数据已同步")

	// 子测试4: GET存在性验证（替代EXISTS）
	printSubTest("1.4 GET存在性验证（Leader重试）")
	getValue, err = cacheGetFromLeaderWithRetry(testKey, 0, 10)
	if err != nil {
		recordTest("基本缓存操作-GET-EXISTS", false, fmt.Sprintf("Leader GET失败: %v", err), time.Since(startTime))
		return
	}
	if !bytes.Equal(getValue, testValue) {
		recordTest("基本缓存操作-GET-EXISTS", false,
			fmt.Sprintf("Leader GET值不匹配，期望: %s, 实际: %s", testValue, getValue), time.Since(startTime))
		return
	}
	printSuccess("Leader GET存在性验证成功")

	// 子测试5: DELETE操作
	printSubTest("1.5 DELETE操作")
	if err := cacheDelete(testKey); err != nil {
		recordTest("基本缓存操作-DELETE", false, fmt.Sprintf("DELETE失败: %v", err), time.Since(startTime))
		return
	}
	printSuccess("DELETE命令已执行")

	// 等待DELETE同步到所有节点
	printInfo("等待DELETE同步（3秒）...")
	time.Sleep(3 * time.Second)

	// 使用GET来验证键是否被删除（404表示不存在）
	printSubTest("1.6 验证DELETE效果")
	_, getErr := cacheGet(testKey)
	if getErr != nil {
		// 404错误或"键不存在"表示键不存在，这是预期的
		if strings.Contains(getErr.Error(), "404") || strings.Contains(getErr.Error(), "键不存在") {
			printSuccess("DELETE操作成功，键已删除")
			recordTest("基本缓存操作", true, "所有操作成功", time.Since(startTime))
			return
		}
		// 其他错误
		recordTest("基本缓存操作-DELETE验证", false, fmt.Sprintf("验证出现错误: %v", getErr), time.Since(startTime))
		return
	}
	// 如果能读到数据，说明删除失败
	recordTest("基本缓存操作-DELETE验证", false, "键仍然存在（能读取到数据）", time.Since(startTime))
}

// 测试2: 批量操作
func test2_BatchOperations() {
	printTestHeader("测试2: 批量操作")
	startTime := time.Now()

	printSubTest("2.1 批量写入100个键值对")
	count := 100
	keyPrefix := "test:batch:" + cid.Generate()

	// 性能统计变量
	var totalLatency time.Duration
	var minLatency time.Duration = time.Hour
	var maxLatency time.Duration = 0
	opsCount := int64(0)

	writeStart := time.Now()
	for i := 0; i < count; i++ {
		key := fmt.Sprintf("%s:key%d", keyPrefix, i)
		value := []byte(fmt.Sprintf("value%d", i))

		opStart := time.Now()
		if err := cacheSet(key, value, 0); err != nil {
			recordTest("批量操作-写入", false, fmt.Sprintf("第%d个键写入失败: %v", i, err), time.Since(startTime))
			return
		}
		opLatency := time.Since(opStart)
		totalLatency += opLatency
		if opLatency < minLatency {
			minLatency = opLatency
		}
		if opLatency > maxLatency {
			maxLatency = opLatency
		}
		opsCount++

		// 每10个操作显示一次进度
		if (i+1)%10 == 0 {
			printPerformanceBar(int64(i+1), int64(count), writeStart, "写入进度")
		}
	}
	printPerformanceBar(int64(count), int64(count), writeStart, "写入进度")
	printPerformanceBar(int64(count), int64(count), writeStart, "写入进度")
	printSuccess(fmt.Sprintf("成功写入%d个键值对", count))

	// 等待Raft共识同步
	printInfo("等待数据同步...")
	time.Sleep(2 * time.Second)

	printSubTest("2.2 批量读取验证")
	successCount := 0

	// 重置性能统计
	totalLatency = 0
	minLatency = time.Hour
	maxLatency = 0
	readOpsCount := int64(0)

	readStart := time.Now()
	for i := 0; i < count; i++ {
		key := fmt.Sprintf("%s:key%d", keyPrefix, i)
		expectedValue := []byte(fmt.Sprintf("value%d", i))

		opStart := time.Now()
		value, err := cacheGet(key)
		opLatency := time.Since(opStart)

		if err != nil {
			recordTest("批量操作-读取", false, fmt.Sprintf("第%d个键读取失败: %v", i, err), time.Since(startTime))
			return
		}
		if !bytes.Equal(value, expectedValue) {
			recordTest("批量操作-读取", false,
				fmt.Sprintf("第%d个键值不匹配，期望: %s, 实际: %s", i, expectedValue, value), time.Since(startTime))
			return
		}

		totalLatency += opLatency
		if opLatency < minLatency {
			minLatency = opLatency
		}
		if opLatency > maxLatency {
			maxLatency = opLatency
		}
		readOpsCount++
		successCount++

		// 每10个操作显示一次进度
		if (i+1)%10 == 0 {
			printPerformanceBar(int64(i+1), int64(count), readStart, "读取进度")
		}
	}
	printPerformanceBar(int64(count), int64(count), readStart, "读取进度")
	printPerformanceBar(int64(count), int64(count), readStart, "读取进度")
	printSuccess(fmt.Sprintf("成功读取验证%d个键值对", successCount))

	printSubTest("2.3 批量删除")
	for i := 0; i < count; i++ {
		key := fmt.Sprintf("%s:key%d", keyPrefix, i)
		if err := cacheDelete(key); err != nil {
			recordTest("批量操作-删除", false, fmt.Sprintf("第%d个键删除失败: %v", i, err), time.Since(startTime))
			return
		}
	}
	printSuccess(fmt.Sprintf("成功删除%d个键值对", count))

	// 计算平均延迟
	avgLatency := totalLatency / time.Duration(readOpsCount)
	totalOps := opsCount + readOpsCount

	recordTestWithMetrics("批量操作", true,
		fmt.Sprintf("成功完成%d个键的写入、读取、删除", count),
		time.Since(startTime),
		totalOps,
		avgLatency,
		minLatency,
		maxLatency)
}

// 测试3: 数据一致性（集群三节点）
func test3_DataConsistency() {
	printTestHeader("测试3: 数据一致性（集群验证）")
	startTime := time.Now()

	printSubTest("3.1 写入数据到集群")
	testKey := "test:consistency:" + cid.Generate()
	testValue := []byte("consistency-test-value")

	if err := cacheSet(testKey, testValue, 0); err != nil {
		recordTest("数据一致性-写入", false, fmt.Sprintf("写入失败: %v", err), time.Since(startTime))
		return
	}
	printSuccess("数据写入成功")

	printSubTest("3.2 等待集群同步")
	time.Sleep(2 * time.Second) // 等待集群同步
	printSuccess("等待完成")

	printSubTest("3.3 从不同节点读取验证")
	// 注意：客户端只连接单个节点，通过该节点验证集群数据一致性
	// Raft协议确保所有节点的数据保持同步
	for i, endpoint := range allEndpoints {
		printInfo(fmt.Sprintf("验证Node%d (%s)...", i+1, endpoint))

		value, err := cacheGetFromNode(testKey, endpoint)
		if err != nil {
			recordTest("数据一致性-读取", false,
				fmt.Sprintf("从Node%d读取失败: %v", i+1, err), time.Since(startTime))
			return
		}

		if !bytes.Equal(value, testValue) {
			recordTest("数据一致性-验证", false,
				fmt.Sprintf("Node%d数据不一致，期望: %s, 实际: %s", i+1, testValue, value), time.Since(startTime))
			return
		}
		printSuccess(fmt.Sprintf("Node%d数据一致", i+1))
	}

	// 清理
	cacheDelete(testKey)

	recordTest("数据一致性", true, "所有节点数据一致", time.Since(startTime))
}

// 测试4: TTL过期
func test4_TTLExpiration() {
	printTestHeader("测试4: TTL过期")
	startTime := time.Now()

	printSubTest("4.1 写入带TTL的数据（3秒过期）")
	testKey := "test:ttl:" + cid.Generate()
	testValue := []byte("ttl-test-value")
	ttl := 3 * time.Second

	if err := cacheSet(testKey, testValue, ttl); err != nil {
		recordTest("TTL过期-写入", false, fmt.Sprintf("写入失败: %v", err), time.Since(startTime))
		return
	}
	printSuccess("带TTL数据写入成功")

	printSubTest("4.2 立即读取验证")
	value, err := cacheGet(testKey)
	if err != nil {
		recordTest("TTL过期-立即读取", false, fmt.Sprintf("读取失败: %v", err), time.Since(startTime))
		return
	}
	if !bytes.Equal(value, testValue) {
		recordTest("TTL过期-立即读取", false, "值不匹配", time.Since(startTime))
		return
	}
	printSuccess("立即读取成功，值正确")

	printSubTest("4.3 等待TTL过期")
	printInfo("等待4秒...")
	time.Sleep(4 * time.Second)
	printSuccess("等待完成")

	printSubTest("4.4 验证数据已过期")
	exists, err := cacheExists(testKey)
	if err != nil {
		// 404错误表示键不存在，这是预期的
		if strings.Contains(err.Error(), "404") {
			printSuccess("数据已正确过期（键不存在）")
			recordTest("TTL过期", true, "TTL机制正常工作", time.Since(startTime))
			return
		}
		recordTest("TTL过期-验证", false, fmt.Sprintf("验证失败: %v", err), time.Since(startTime))
		return
	}
	if exists {
		recordTest("TTL过期-验证", false, "数据未过期", time.Since(startTime))
		return
	}
	printSuccess("数据已正确过期")

	recordTest("TTL过期", true, "TTL机制正常工作", time.Since(startTime))
}

// 测试5: 数据库隔离
func test5_DatabaseIsolation() {
	printTestHeader("测试5: 数据库隔离")
	startTime := time.Now()

	sameKey := "test:db:isolation"
	value1 := []byte("db0-value")
	value2 := []byte("db1-value")

	printSubTest("5.1 在DB0写入数据")
	if err := cacheSetDB(sameKey, value1, 0, 0); err != nil {
		recordTest("数据库隔离-DB0写入", false, fmt.Sprintf("DB0写入失败: %v", err), time.Since(startTime))
		return
	}
	printSuccess("DB0写入成功")
	time.Sleep(100 * time.Millisecond) // 等待写入完成

	printSubTest("5.2 在DB1写入相同键但不同值")
	if err := cacheSetDB(sameKey, value2, 1, 0); err != nil {
		recordTest("数据库隔离-DB1写入", false, fmt.Sprintf("DB1写入失败: %v", err), time.Since(startTime))
		return
	}
	printSuccess("DB1写入成功")
	time.Sleep(100 * time.Millisecond) // 等待写入完成

	// 先从leader读取验证（强一致性）
	printSubTest("5.3 从Leader验证DB0的值")
	v1, err := cacheGetFromLeaderWithRetry(sameKey, 0, 10)
	if err != nil {
		recordTest("数据库隔离-DB0-Leader读取", false, fmt.Sprintf("DB0 Leader读取失败: %v", err), time.Since(startTime))
		return
	}
	if !bytes.Equal(v1, value1) {
		recordTest("数据库隔离-DB0-Leader读取", false, "DB0 Leader值不匹配", time.Since(startTime))
		return
	}
	printSuccess("DB0 Leader值正确")

	printSubTest("5.4 从Leader验证DB1的值")
	v2, err := cacheGetFromLeaderWithRetry(sameKey, 1, 10)
	if err != nil {
		recordTest("数据库隔离-DB1-Leader读取", false, fmt.Sprintf("DB1 Leader读取失败: %v", err), time.Since(startTime))
		return
	}
	if !bytes.Equal(v2, value2) {
		recordTest("数据库隔离-DB1-Leader读取", false, "DB1 Leader值不匹配", time.Since(startTime))
		return
	}
	printSuccess("DB1 Leader值正确")

	// 等待数据同步到所有节点
	printInfo("等待数据同步到所有follower节点...")
	time.Sleep(2 * time.Second)

	printSubTest("5.5 从Follower验证DB0的值")
	v1, err = cacheGetDB(sameKey, 0)
	if err != nil {
		recordTest("数据库隔离-DB0读取", false, fmt.Sprintf("DB0读取失败: %v", err), time.Since(startTime))
		return
	}
	if !bytes.Equal(v1, value1) {
		recordTest("数据库隔离-DB0读取", false, "DB0值不匹配", time.Since(startTime))
		return
	}
	printSuccess("DB0 Follower值正确")

	printSubTest("5.6 从Follower验证DB1的值")
	v2, err = cacheGetDB(sameKey, 1)
	if err != nil {
		recordTest("数据库隔离-DB1读取", false, fmt.Sprintf("DB1读取失败: %v", err), time.Since(startTime))
		return
	}
	if !bytes.Equal(v2, value2) {
		recordTest("数据库隔离-DB1读取", false, "DB1值不匹配", time.Since(startTime))
		return
	}
	printSuccess("DB1 Follower值正确")

	// 清理
	cacheDeleteDB(sameKey, 0)
	cacheDeleteDB(sameKey, 1)

	recordTest("数据库隔离", true, "数据库隔离正常工作", time.Since(startTime))
}

// 测试6: 并发操作
