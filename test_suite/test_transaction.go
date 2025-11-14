package main

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/qorm/burin/cid"
	"github.com/qorm/burin/client/interfaces"
)

// test12_BasicTransactions 基本事务测试
func test12_BasicTransactions() {
	printTestHeader("测试12: 基本事务操作")
	startTime := time.Now()

	printSubTest("12.1 开始事务并执行操作")
	testKey := "test:tx:basic:" + cid.Generate()
	testValue := []byte("transaction-value")

	tx, err := burinClient.BeginTransaction()
	if err != nil {
		recordTest("基本事务-开始", false, fmt.Sprintf("开始事务失败: %v", err), time.Since(startTime))
		return
	}
	printSuccess(fmt.Sprintf("事务已开始，ID: %s", tx.ID()))

	printSubTest("12.2 在事务中写入数据")
	if err := tx.Set(testKey, testValue); err != nil {
		tx.Rollback()
		recordTest("基本事务-写入", false, fmt.Sprintf("事务写入失败: %v", err), time.Since(startTime))
		return
	}
	printSuccess("事务中写入数据成功")

	printSubTest("12.3 在事务中读取数据")
	value, err := tx.Get(testKey)
	if err != nil {
		tx.Rollback()
		recordTest("基本事务-读取", false, fmt.Sprintf("事务读取失败: %v", err), time.Since(startTime))
		return
	}

	if !bytes.Equal(value, testValue) {
		tx.Rollback()
		recordTest("基本事务-验证", false, "事务中读取的值不匹配", time.Since(startTime))
		return
	}
	printSuccess("事务中读取数据正确")

	printSubTest("12.4 提交事务")
	if err := tx.Commit(); err != nil {
		recordTest("基本事务-提交", false, fmt.Sprintf("提交事务失败: %v", err), time.Since(startTime))
		return
	}
	printSuccess("事务提交成功")

	printSubTest("12.5 验证事务提交后数据可见")
	time.Sleep(500 * time.Millisecond) // 等待提交完成
	readValue, err := cacheGet(testKey)
	if err != nil {
		recordTest("基本事务-提交验证", false, fmt.Sprintf("读取提交后数据失败: %v", err), time.Since(startTime))
		return
	}

	if !bytes.Equal(readValue, testValue) {
		recordTest("基本事务-提交验证", false, "提交后数据不匹配", time.Since(startTime))
		return
	}
	printSuccess("事务提交后数据正确可见")

	// 清理
	cacheDelete(testKey)

	recordTest("基本事务操作", true, "事务基本操作正常", time.Since(startTime))
}

// test13_TransactionRollback 事务回滚测试
func test13_TransactionRollback() {
	printTestHeader("测试13: 事务回滚")
	startTime := time.Now()

	printSubTest("13.1 开始事务并写入数据")
	testKey := "test:tx:rollback:" + cid.Generate()
	testValue := []byte("will-be-rolled-back")

	tx, err := burinClient.BeginTransaction()
	if err != nil {
		recordTest("事务回滚-开始", false, fmt.Sprintf("开始事务失败: %v", err), time.Since(startTime))
		return
	}
	printSuccess(fmt.Sprintf("事务已开始，ID: %s", tx.ID()))

	if err := tx.Set(testKey, testValue); err != nil {
		tx.Rollback()
		recordTest("事务回滚-写入", false, fmt.Sprintf("事务写入失败: %v", err), time.Since(startTime))
		return
	}
	printSuccess("事务中写入数据成功")

	printSubTest("13.2 回滚事务")
	if err := tx.Rollback(); err != nil {
		recordTest("事务回滚-执行", false, fmt.Sprintf("回滚事务失败: %v", err), time.Since(startTime))
		return
	}
	printSuccess("事务回滚成功")

	printSubTest("13.3 验证回滚后数据不可见")
	time.Sleep(500 * time.Millisecond) // 等待回滚完成
	_, err = cacheGet(testKey)
	if err == nil {
		recordTest("事务回滚-验证", false, "回滚后数据仍然可见", time.Since(startTime))
		return
	}
	printSuccess("事务回滚后数据不可见（符合预期）")

	recordTest("事务回滚", true, "事务回滚功能正常", time.Since(startTime))
}

// test14_TransactionIsolation 事务隔离级别测试
func test14_TransactionIsolation() {
	printTestHeader("测试14: 事务隔离级别")
	startTime := time.Now()

	testKey := "test:tx:isolation:" + cid.Generate()
	initialValue := []byte("initial-value")

	printSubTest("14.1 写入初始数据")
	if err := cacheSet(testKey, initialValue, 0); err != nil {
		recordTest("事务隔离-初始化", false, fmt.Sprintf("写入初始数据失败: %v", err), time.Since(startTime))
		return
	}
	time.Sleep(500 * time.Millisecond)
	printSuccess("初始数据写入成功")

	printSubTest("14.2 可重复读隔离级别测试")
	tx1, err := burinClient.BeginTransaction(
		interfaces.WithIsolationLevel(interfaces.RepeatableRead),
	)
	if err != nil {
		recordTest("事务隔离-开始事务1", false, fmt.Sprintf("开始事务失败: %v", err), time.Since(startTime))
		return
	}
	printSuccess("事务1已开始（可重复读）")

	// 事务1第一次读取
	value1, err := tx1.Get(testKey)
	if err != nil {
		tx1.Rollback()
		recordTest("事务隔离-第一次读取", false, fmt.Sprintf("事务1读取失败: %v", err), time.Since(startTime))
		return
	}
	printSuccess("事务1第一次读取成功")

	printSubTest("14.3 在另一个事务中修改数据")
	tx2, err := burinClient.BeginTransaction()
	if err != nil {
		tx1.Rollback()
		recordTest("事务隔离-开始事务2", false, fmt.Sprintf("开始事务2失败: %v", err), time.Since(startTime))
		return
	}

	newValue := []byte("modified-value")
	if err := tx2.Set(testKey, newValue); err != nil {
		tx1.Rollback()
		tx2.Rollback()
		recordTest("事务隔离-事务2写入", false, fmt.Sprintf("事务2写入失败: %v", err), time.Since(startTime))
		return
	}

	if err := tx2.Commit(); err != nil {
		tx1.Rollback()
		recordTest("事务隔离-事务2提交", false, fmt.Sprintf("事务2提交失败: %v", err), time.Since(startTime))
		return
	}
	printSuccess("事务2修改并提交成功")

	time.Sleep(500 * time.Millisecond)

	printSubTest("14.4 验证事务1的可重复读")
	value2, err := tx1.Get(testKey)
	if err != nil {
		tx1.Rollback()
		recordTest("事务隔离-第二次读取", false, fmt.Sprintf("事务1第二次读取失败: %v", err), time.Since(startTime))
		return
	}

	// 在可重复读隔离级别下，事务1应该读到相同的值
	if !bytes.Equal(value1, value2) {
		tx1.Rollback()
		recordTest("事务隔离-可重复读验证", false,
			fmt.Sprintf("可重复读失败：第一次读取 %s，第二次读取 %s", value1, value2),
			time.Since(startTime))
		return
	}
	printSuccess("事务1可重复读验证成功（读到相同值）")

	tx1.Commit()

	// 清理
	cacheDelete(testKey)

	recordTest("事务隔离级别", true, "事务隔离级别功能正常", time.Since(startTime))
}

// test15_ConcurrentTransactions 并发事务测试
func test15_ConcurrentTransactions() {
	printTestHeader("测试15: 并发事务")
	startTime := time.Now()

	// 检查连接池是否可用
	if clientPool == nil {
		recordTest("并发事务", false, "连接池未初始化，无法执行并发事务测试", time.Since(startTime))
		return
	}

	concurrency := 10
	keyPrefix := "test:tx:concurrent:" + cid.Generate()

	printSubTest(fmt.Sprintf("15.1 启动%d个并发事务", concurrency))

	var wg sync.WaitGroup
	errorChan := make(chan error, concurrency)
	successCount := 0
	var mu sync.Mutex

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// 从连接池获取客户端
			pc, err := clientPool.Get()
			if err != nil {
				errorChan <- fmt.Errorf("goroutine %d: 获取客户端失败: %v", id, err)
				return
			}
			defer pc.Close()

			c := pc.Get()
			key := fmt.Sprintf("%s:key%d", keyPrefix, id)
			value := []byte(fmt.Sprintf("value-%d", id))

			// 开始事务
			tx, err := c.BeginTransaction()
			if err != nil {
				errorChan <- fmt.Errorf("goroutine %d: 开始事务失败: %v", id, err)
				return
			}

			// 在事务中写入
			if err := tx.Set(key, value); err != nil {
				tx.Rollback()
				errorChan <- fmt.Errorf("goroutine %d: 事务写入失败: %v", id, err)
				return
			}

			// 在事务中读取验证
			readValue, err := tx.Get(key)
			if err != nil {
				tx.Rollback()
				errorChan <- fmt.Errorf("goroutine %d: 事务读取失败: %v", id, err)
				return
			}

			if !bytes.Equal(readValue, value) {
				tx.Rollback()
				errorChan <- fmt.Errorf("goroutine %d: 事务中值不匹配", id)
				return
			}

			// 提交事务
			if err := tx.Commit(); err != nil {
				errorChan <- fmt.Errorf("goroutine %d: 提交事务失败: %v", id, err)
				return
			}

			mu.Lock()
			successCount++
			mu.Unlock()
		}(i)
	}

	wg.Wait()
	close(errorChan)

	// 检查错误
	errors := []error{}
	for err := range errorChan {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		printWarning(fmt.Sprintf("有 %d 个并发事务失败", len(errors)))
		for i, err := range errors {
			if i < 3 { // 只显示前3个错误
				printWarning(fmt.Sprintf("  - %v", err))
			}
		}
	}

	printSuccess(fmt.Sprintf("并发事务完成：成功 %d/%d", successCount, concurrency))

	printSubTest("15.2 验证并发事务提交的数据")
	time.Sleep(time.Second) // 等待所有事务提交完成

	verifiedCount := 0
	for i := 0; i < successCount; i++ {
		key := fmt.Sprintf("%s:key%d", keyPrefix, i)
		expectedValue := []byte(fmt.Sprintf("value-%d", i))

		value, err := cacheGet(key)
		if err == nil && bytes.Equal(value, expectedValue) {
			verifiedCount++
		}
	}

	printInfo(fmt.Sprintf("验证结果: %d/%d 数据正确", verifiedCount, successCount))

	// 清理
	for i := 0; i < concurrency; i++ {
		key := fmt.Sprintf("%s:key%d", keyPrefix, i)
		cacheDelete(key)
	}

	if successCount >= int(float64(concurrency)*0.8) && verifiedCount >= int(float64(successCount)*0.8) {
		recordTest("并发事务", true,
			fmt.Sprintf("并发事务测试通过：%d/%d 成功，%d/%d 验证通过",
				successCount, concurrency, verifiedCount, successCount),
			time.Since(startTime))
	} else {
		recordTest("并发事务", false,
			fmt.Sprintf("并发事务失败率过高：%d/%d 成功，%d/%d 验证通过",
				successCount, concurrency, verifiedCount, successCount),
			time.Since(startTime))
	}
}

// test16_TransactionTimeout 事务超时测试
func test16_TransactionTimeout() {
	printTestHeader("测试16: 事务超时")
	startTime := time.Now()

	printSubTest("16.1 创建短超时事务")
	testKey := "test:tx:timeout:" + cid.Generate()
	testValue := []byte("timeout-test")

	// 创建2秒超时的事务
	tx, err := burinClient.BeginTransaction(
		interfaces.WithTxTimeout(2 * time.Second),
	)
	if err != nil {
		recordTest("事务超时-开始", false, fmt.Sprintf("开始事务失败: %v", err), time.Since(startTime))
		return
	}
	txID := tx.ID()
	printSuccess(fmt.Sprintf("短超时事务已开始，ID: %s，超时: 2秒", txID))

	printSubTest("16.2 在事务中写入数据")
	if err := tx.Set(testKey, testValue); err != nil {
		tx.Rollback()
		recordTest("事务超时-写入", false, fmt.Sprintf("事务写入失败: %v", err), time.Since(startTime))
		return
	}
	printSuccess("事务中写入数据成功")

	printSubTest("16.3 验证事务当前状态")
	status := tx.Status()
	printInfo(fmt.Sprintf("当前事务状态: %v", status))
	if status != interfaces.TxStatusPending {
		recordTest("事务超时-状态", false, fmt.Sprintf("事务状态异常: %v", status), time.Since(startTime))
		return
	}

	printSubTest("16.4 等待事务超时")
	printInfo("等待3秒让事务超时（超时设置为2秒）...")
	time.Sleep(3 * time.Second)

	printSubTest("16.5 检查事务超时后的状态")
	status = tx.Status()
	printInfo(fmt.Sprintf("等待后事务状态: %v", status))

	printSubTest("16.6 尝试提交已超时的事务")
	err = tx.Commit()
	if err == nil {
		recordTest("事务超时-提交", false, "超时事务仍可提交（不符合预期）", time.Since(startTime))
		cacheDelete(testKey)
		return
	}

	// 检查错误信息是否包含超时相关的关键词
	errMsg := err.Error()
	if strings.Contains(errMsg, "timeout") || strings.Contains(errMsg, "timed out") ||
		strings.Contains(errMsg, "超时") || strings.Contains(errMsg, "expired") {
		printSuccess(fmt.Sprintf("✅ 超时事务提交被正确拒绝: %v", err))
	} else {
		printWarning(fmt.Sprintf("⚠️ 提交失败但错误信息不明确: %v", err))
	}

	printSubTest("16.7 验证超时事务未提交数据")
	value, err := cacheGet(testKey)
	if err != nil {
		// key not found 是预期的
		if strings.Contains(err.Error(), "not found") {
			printSuccess("✅ 超时事务的数据未提交（符合预期）")
		} else {
			printWarning(fmt.Sprintf("验证时发生错误: %v", err))
		}
	} else {
		recordTest("事务超时-数据验证", false,
			fmt.Sprintf("超时事务的数据不应该可见: %s", string(value)), time.Since(startTime))
		cacheDelete(testKey)
		return
	}

	// 清理（如果有数据）
	cacheDelete(testKey)

	recordTest("事务超时", true, "事务超时功能正常，超时事务被正确拒绝", time.Since(startTime))
}
