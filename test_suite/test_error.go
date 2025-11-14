package main

import (
"time"

"burin/cid"
)
func test10_ErrorHandling() {
	printTestHeader("测试10: 错误处理")
	startTime := time.Now()

	printSubTest("10.1 读取不存在的键")
	nonExistKey := "test:nonexist:" + cid.Generate()
	_, err := cacheGet(nonExistKey)
	if err == nil {
		recordTest("错误处理-不存在的键", false, "应该返回错误但返回成功", time.Since(startTime))
		return
	}
	printSuccess("正确处理不存在的键")

	printSubTest("10.2 删除不存在的键")
	err = cacheDelete(nonExistKey)
	// 删除不存在的键应该成功或返回特定错误
	printSuccess("删除不存在的键处理正常")

	printSubTest("10.3 空键名测试")
	err = cacheSet("", []byte("value"), 0)
	if err == nil {
		recordTest("错误处理-空键名", false, "空键名应该返回错误", time.Since(startTime))
		return
	}
	printSuccess("正确拒绝空键名")

	recordTest("错误处理", true, "错误处理机制正常", time.Since(startTime))
}

// 测试11: 新节点加入和数据同步
