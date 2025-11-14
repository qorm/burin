package main

import (
	"bytes"
	"fmt"
	"sync"
	"time"

	"github.com/qorm/burin/cid"
)

func test6_ConcurrentOperations() {
	printTestHeader("测试6: 并发操作")
	startTime := time.Now()

	// 检查连接池是否可用
	if clientPool == nil {
		recordTest("并发操作", false, "连接池未初始化，无法执行并发测试", time.Since(startTime))
		return
	}

	concurrency := 20     // 并发goroutine数
	opsPerGoroutine := 10 // 每个goroutine的操作数
	keyPrefix := "test:concurrent:" + cid.Generate()

	printSubTest(fmt.Sprintf("6.1 启动%d个并发goroutine，每个执行%d次操作", concurrency, opsPerGoroutine))

	var wg sync.WaitGroup
	errorChan := make(chan error, concurrency)

	// 性能统计
	type OpMetric struct {
		latency time.Duration
	}
	metricsChan := make(chan OpMetric, concurrency*opsPerGoroutine*3)

	concurrentStart := time.Now()

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// 从连接池获取客户端
			pc, err := clientPool.Get()
			if err != nil {
				errorChan <- fmt.Errorf("goroutine %d获取客户端失败: %v", id, err)
				return
			}
			defer pc.Close()

			c := pc.Get()

			for j := 0; j < opsPerGoroutine; j++ {
				key := fmt.Sprintf("%s:g%d:k%d", keyPrefix, id, j)
				value := []byte(fmt.Sprintf("value-g%d-k%d", id, j))

				// 写入
				opStart := time.Now()
				if err := c.Set(key, value); err != nil {
					errorChan <- fmt.Errorf("goroutine %d写入失败: %v", id, err)
					return
				}
				metricsChan <- OpMetric{latency: time.Since(opStart)}

				// 立即读取验证
				opStart = time.Now()
				resp, err := c.Get(key)
				if err != nil {
					errorChan <- fmt.Errorf("goroutine %d读取失败: %v", id, err)
					return
				}
				metricsChan <- OpMetric{latency: time.Since(opStart)}

				if !bytes.Equal(resp.Value, value) {
					errorChan <- fmt.Errorf("goroutine %d值不匹配", id)
					return
				}

				// 删除
				opStart = time.Now()
				if err := c.Delete(key); err != nil {
					errorChan <- fmt.Errorf("goroutine %d删除失败: %v", id, err)
					return
				}
				metricsChan <- OpMetric{latency: time.Since(opStart)}
			}
		}(i)
	}

	wg.Wait()
	close(errorChan)
	close(metricsChan)

	// 检查错误
	if len(errorChan) > 0 {
		err := <-errorChan
		recordTest("并发操作", false, err.Error(), time.Since(startTime))
		return
	}

	// 计算性能指标
	var totalLatency time.Duration
	var minLatency time.Duration = time.Hour
	var maxLatency time.Duration = 0
	opsCount := int64(0)

	for metric := range metricsChan {
		totalLatency += metric.latency
		if metric.latency < minLatency {
			minLatency = metric.latency
		}
		if metric.latency > maxLatency {
			maxLatency = metric.latency
		}
		opsCount++
	}

	avgLatency := time.Duration(0)
	if opsCount > 0 {
		avgLatency = totalLatency / time.Duration(opsCount)
	}

	totalOps := concurrency * opsPerGoroutine * 3 // 写、读、删各一次
	duration := time.Since(concurrentStart)
	printSuccess(fmt.Sprintf("并发操作成功，共完成%d次操作，耗时: %v", totalOps, duration.Truncate(time.Millisecond)))

	recordTestWithMetrics("并发操作", true,
		fmt.Sprintf("%d个并发goroutine正常完成", concurrency),
		time.Since(startTime),
		opsCount,
		avgLatency,
		minLatency,
		maxLatency)
}

// 测试7: 大值处理
func test7_LargeValueHandling() {
	printTestHeader("测试7: 大值处理")
	startTime := time.Now()

	sizes := []int{
		1024,             // 1KB
		10 * 1024,        // 10KB
		100 * 1024,       // 100KB
		500 * 1024,       // 500KB
		1024 * 1024,      // 1MB
		2 * 1024 * 1024,  // 2MB
		5 * 1024 * 1024,  // 5MB
		10 * 1024 * 1024, // 10MB
	}

	var totalLatency time.Duration
	var minLatency time.Duration = time.Hour
	var maxLatency time.Duration = 0
	opsCount := int64(0)

	for _, size := range sizes {
		sizeKB := size / 1024
		if sizeKB >= 1024 {
			printSubTest(fmt.Sprintf("7.%d 测试%.1fMB数据", sizeKB, float64(size)/(1024*1024)))
		} else {
			printSubTest(fmt.Sprintf("7.%d 测试%dKB数据", sizeKB, sizeKB))
		}

		testKey := fmt.Sprintf("test:large:%d:%s", size, cid.Generate())
		testValue := make([]byte, size)
		for i := range testValue {
			testValue[i] = byte(i % 256)
		}

		// 写入
		opStart := time.Now()
		if err := cacheSet(testKey, testValue, 0); err != nil {
			recordTest(fmt.Sprintf("大值处理-%dKB", size/1024), false,
				fmt.Sprintf("写入失败: %v", err), time.Since(startTime))
			return
		}
		writeLatency := time.Since(opStart)
		totalLatency += writeLatency
		if writeLatency < minLatency {
			minLatency = writeLatency
		}
		if writeLatency > maxLatency {
			maxLatency = writeLatency
		}
		opsCount++

		sizeStr := fmt.Sprintf("%dKB", size/1024)
		if size >= 1024*1024 {
			sizeStr = fmt.Sprintf("%.1fMB", float64(size)/(1024*1024))
		}
		printInfo(fmt.Sprintf("写入%s耗时: %v", sizeStr, writeLatency.Truncate(time.Millisecond)))

		// 等待大值写入完成（根据大小动态调整等待时间）
		var waitTime time.Duration
		if size < 100*1024 {
			waitTime = 200 * time.Millisecond
		} else if size < 1024*1024 {
			waitTime = 2 * time.Second
		} else if size < 5*1024*1024 {
			waitTime = 5 * time.Second
		} else {
			waitTime = 10 * time.Second
		}
		time.Sleep(waitTime)

		// 立即从leader读取验证
		opStart = time.Now()
		readValue, err := cacheGetFromLeaderWithRetry(testKey, 0, 15)
		if err != nil {
			recordTest(fmt.Sprintf("大值处理-%dKB-Leader", size/1024), false,
				fmt.Sprintf("从Leader读取失败: %v", err), time.Since(startTime))
			return
		}
		readLatency := time.Since(opStart)
		totalLatency += readLatency
		if readLatency < minLatency {
			minLatency = readLatency
		}
		if readLatency > maxLatency {
			maxLatency = readLatency
		}
		opsCount++

		readSizeStr := fmt.Sprintf("%dKB", size/1024)
		if size >= 1024*1024 {
			readSizeStr = fmt.Sprintf("%.1fMB", float64(size)/(1024*1024))
		}
		printInfo(fmt.Sprintf("读取%s耗时: %v", readSizeStr, readLatency.Truncate(time.Millisecond)))

		if !bytes.Equal(readValue, testValue) {
			recordTest(fmt.Sprintf("大值处理-%dKB-Leader", size/1024), false,
				"Leader数据不匹配", time.Since(startTime))
			return
		}

		// 等待数据同步到follower（大值需要更长时间）
		time.Sleep(2 * time.Second)

		// 从follower读取验证
		readValue, err = cacheGet(testKey)
		if err != nil {
			recordTest(fmt.Sprintf("大值处理-%dKB-Follower", size/1024), false,
				fmt.Sprintf("从Follower读取失败: %v", err), time.Since(startTime))
			return
		}

		// 验证
		if !bytes.Equal(readValue, testValue) {
			recordTest(fmt.Sprintf("大值处理-%dKB-Follower", size/1024), false,
				"Follower数据不匹配", time.Since(startTime))
			return
		}

		// 清理
		cacheDelete(testKey)

		successSizeStr := fmt.Sprintf("%dKB", size/1024)
		if size >= 1024*1024 {
			successSizeStr = fmt.Sprintf("%.1fMB", float64(size)/(1024*1024))
		}
		printSuccess(fmt.Sprintf("%s数据处理成功", successSizeStr))
	}

	// 计算平均延迟
	avgLatency := time.Duration(0)
	if opsCount > 0 {
		avgLatency = totalLatency / time.Duration(opsCount)
	}

	recordTestWithMetrics("大值处理", true,
		"所有大小数据处理正常",
		time.Since(startTime),
		opsCount,
		avgLatency,
		minLatency,
		maxLatency)
}

// 测试8: 队列操作
