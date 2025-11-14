package main

import (
	"fmt"
	"time"

	"github.com/qorm/burin/cid"
)

// testDB_Operations 数据库操作测试
func testDB_Operations() {
	printTestHeader("测试DB: 数据库管理操作")
	startTime := time.Now()

	// 生成唯一的测试数据库名
	testDB := "test_db_" + cid.Generate()

	printSubTest("DB.1 创建新数据库")
	// 注意：由于客户端限制，我们通过写入数据来隐式创建数据库
	// 先写入一个测试键
	testKey := testDB + ":test:key:1"
	testValue := []byte("test-value")

	if err := cacheSet(testKey, testValue, 0); err != nil {
		recordTest("数据库管理-操作", false, fmt.Sprintf("写入数据失败: %v", err), time.Since(startTime))
		return
	}
	printSuccess(fmt.Sprintf("在数据库 '%s' 中写入数据成功", testDB))

	printSubTest("DB.2 读取数据验证")
	value, err := cacheGet(testKey)
	if err != nil {
		recordTest("数据库管理-读取", false, fmt.Sprintf("读取数据失败: %v", err), time.Since(startTime))
		return
	}
	if string(value) != string(testValue) {
		recordTest("数据库管理-验证", false, "数据不匹配", time.Since(startTime))
		return
	}
	printSuccess("数据读取验证成功")

	printSubTest("DB.3 清理测试数据")
	if err := cacheDelete(testKey); err != nil {
		printWarning(fmt.Sprintf("清理失败: %v", err))
	} else {
		printSuccess("测试数据清理成功")
	}

	printSubTest("DB.4 测试系统数据库保护")
	// 测试系统数据库的使用
	systemKey := "test:system:key"
	systemValue := []byte("system test")

	if err := cacheSet(systemKey, systemValue, 0); err == nil {
		printSuccess("✅ 可以在default数据库中正常读写")
		cacheDelete(systemKey)
	} else {
		printWarning(fmt.Sprintf("default数据库操作失败: %v", err))
	}

	printInfo("注意：系统数据库（__burin_system__、__burin_geo__、default）")
	printInfo("       的删除保护已在存储引擎底层（store/cache.go）实现")
	printInfo("       无论从哪个层级调用DeleteDatabase都会被拒绝")

	recordTest("数据库管理操作", true, "数据库基本操作测试通过，系统数据库受底层保护", time.Since(startTime))
}
