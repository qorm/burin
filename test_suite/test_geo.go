package main

import (
	"fmt"
	"math"
	"time"

	"github.com/qorm/burin/cid"
	"github.com/qorm/burin/client/interfaces"
)

const geoDBName = "geo_test" // 全局GEO数据库名称

func testGeo_BasicOperations() {
	printTestHeader("GEO测试: 基本地理位置操作")
	startTime := time.Now()

	// GEO数据库应该已经由其他测试创建
	// 不再创建数据库，直接使用

	testKey := "test:geo:" + cid.Generate() // 使用动态key
	printInfo(fmt.Sprintf("使用测试Key: %s", testKey))

	printSubTest("GEO.1 添加地理位置数据")
	members := []interfaces.GeoMember{
		{Name: "beijing", Longitude: 116.397128, Latitude: 39.916527},  // 北京天安门
		{Name: "shanghai", Longitude: 121.473701, Latitude: 31.230416}, // 上海外滩
	}

	if err := burinClient.GeoAdd(testKey, members, interfaces.WithGeoDatabase(geoDBName)); err != nil {
		recordTest("GEO基本操作-添加", false, fmt.Sprintf("GeoAdd失败: %v", err), time.Since(startTime))
		return
	}
	printSuccess("成功添加2个城市的地理位置")

	// 等待数据同步
	printInfo("等待数据同步...")
	time.Sleep(7 * time.Second) // 增加到7秒

	printSubTest("GEO.2 计算两个城市之间的距离")
	// 计算北京到上海的距离（单位：公里）
	distance, err := burinClient.GeoDist(testKey, "beijing", "shanghai", "km", interfaces.WithGeoDatabase(geoDBName))
	if err != nil {
		recordTest("GEO基本操作-距离计算", false, fmt.Sprintf("GeoDist失败: %v", err), time.Since(startTime))
		return
	}

	// 北京到上海直线距离约1068公里
	expectedDistance := 1068.0
	tolerance := 50.0 // 允许50公里误差

	if math.Abs(distance-expectedDistance) > tolerance {
		recordTest("GEO基本操作-距离计算", false,
			fmt.Sprintf("距离不准确: 期望约%.0fkm, 实际%.2fkm", expectedDistance, distance),
			time.Since(startTime))
		return
	}
	printSuccess(fmt.Sprintf("北京到上海距离: %.2f km （符合预期）", distance))

	printSubTest("GEO.3 清理测试数据")
	deletedCount, err := burinClient.GeoDel(testKey, []string{"beijing", "shanghai"}, interfaces.WithGeoDatabase(geoDBName))
	if err != nil {
		printWarning(fmt.Sprintf("清理失败: %v", err))
	} else {
		printSuccess(fmt.Sprintf("测试数据已清理 (删除%d个成员)", deletedCount))
	}

	recordTest("GEO基本操作", true, "所有GEO操作测试通过", time.Since(startTime))
}

func testGeo_AdvancedOperations() {
	printTestHeader("GEO测试: 高级操作")
	startTime := time.Now()

	// 确保GEO数据库存在
	printSubTest("GEO-ADV.0 确保GEO数据库存在")
	// GEO数据库会在首次使用时自动创建
	printInfo("GEO数据库将在首次使用时自动创建")
	// 等待数据库就绪
	time.Sleep(1 * time.Second)

	testKey := "test:geo:advanced:" + cid.Generate()

	printSubTest("GEO-ADV.1 添加更多地理位置数据")
	members := []interfaces.GeoMember{
		{Name: "london", Longitude: -0.127758, Latitude: 51.507351},       // 伦敦
		{Name: "paris", Longitude: 2.352222, Latitude: 48.856614},         // 巴黎
		{Name: "berlin", Longitude: 13.404954, Latitude: 52.520007},       // 柏林
		{Name: "newyork", Longitude: -74.005941, Latitude: 40.712784},     // 纽约
		{Name: "losangeles", Longitude: -118.243685, Latitude: 34.052234}, // 洛杉矶
		{Name: "tokyo", Longitude: 139.691706, Latitude: 35.689487},       // 东京
		{Name: "sydney", Longitude: 151.209296, Latitude: -33.868820},     // 悉尼
	}

	if err := burinClient.GeoAdd(testKey, members, interfaces.WithGeoDatabase(geoDBName)); err != nil {
		recordTest("GEO高级操作-添加", false, fmt.Sprintf("GeoAdd失败: %v", err), time.Since(startTime))
		return
	}
	printSuccess("成功添加7个国际城市的地理位置")

	// 等待数据同步
	time.Sleep(7 * time.Second) // 增加到7秒

	printSubTest("GEO-ADV.2 跨洋距离计算")
	// 纽约到伦敦（约5570公里）
	distance, err := burinClient.GeoDist(testKey, "newyork", "london", "km", interfaces.WithGeoDatabase(geoDBName))
	if err != nil {
		recordTest("GEO高级操作-跨洋距离", false, fmt.Sprintf("GeoDist失败: %v", err), time.Since(startTime))
		return
	}
	printSuccess(fmt.Sprintf("纽约到伦敦距离: %.2f km", distance))

	// 东京到悉尼
	distance, err = burinClient.GeoDist(testKey, "tokyo", "sydney", "km", interfaces.WithGeoDatabase(geoDBName))
	if err != nil {
		recordTest("GEO高级操作-跨洋距离2", false, fmt.Sprintf("GeoDist失败: %v", err), time.Since(startTime))
		return
	}
	printSuccess(fmt.Sprintf("东京到悉尼距离: %.2f km", distance))

	printSubTest("GEO-ADV.3 欧洲范围查询")
	// 查找巴黎周边1000公里内的城市
	results, err := burinClient.GeoRadius(testKey, 2.352222, 48.856614, 1000, "km", interfaces.WithGeoDatabase(geoDBName))
	if err != nil {
		recordTest("GEO高级操作-欧洲范围", false, fmt.Sprintf("GeoRadius失败: %v", err), time.Since(startTime))
		return
	}

	printSuccess(fmt.Sprintf("巴黎1000公里范围内的城市 (%d个):", len(results)))
	for _, result := range results {
		printInfo(fmt.Sprintf("  - %s: %.2f km", result.Name, result.Distance))
	}

	printSubTest("GEO-ADV.4 清理测试数据")
	if err := burinClient.Delete(testKey); err != nil {
		printWarning(fmt.Sprintf("清理失败: %v", err))
	} else {
		printSuccess("测试数据已清理")
	}

	recordTest("GEO高级操作", true, "所有高级GEO操作测试通过", time.Since(startTime))
}

func testGeo_EdgeCases() {
	printTestHeader("GEO测试: 边界情况")
	startTime := time.Now()

	testKey := "test:geo:edge:" + cid.Generate()

	printSubTest("GEO-EDGE.1 测试不存在的成员")
	// 在空key上查询距离
	_, err := burinClient.GeoDist(testKey, "nonexistent1", "nonexistent2", "km", interfaces.WithGeoDatabase(geoDBName))
	if err == nil {
		recordTest("GEO边界情况-不存在成员", false, "应该返回错误", time.Since(startTime))
		return
	}
	printSuccess("正确处理不存在的成员")

	printSubTest("GEO-EDGE.2 添加单个位置并查询")
	members := []interfaces.GeoMember{
		{Name: "origin", Longitude: 0, Latitude: 0}, // 原点
	}

	if err := burinClient.GeoAdd(testKey, members, interfaces.WithGeoDatabase(geoDBName)); err != nil {
		recordTest("GEO边界情况-单点", false, fmt.Sprintf("GeoAdd失败: %v", err), time.Since(startTime))
		return
	}
	time.Sleep(7 * time.Second) // 增加等待时间

	// 查询原点周边
	results, err := burinClient.GeoRadius(testKey, 0, 0, 1, "km", interfaces.WithGeoDatabase(geoDBName))
	if err != nil {
		recordTest("GEO边界情况-单点查询", false, fmt.Sprintf("GeoRadius失败: %v", err), time.Since(startTime))
		return
	}

	if len(results) != 1 || results[0].Name != "origin" {
		recordTest("GEO边界情况-单点查询", false, "应该找到origin", time.Since(startTime))
		return
	}
	printSuccess("单点查询成功")

	printSubTest("GEO-EDGE.3 清理测试数据")
	if err := burinClient.Delete(testKey); err != nil {
		printWarning(fmt.Sprintf("清理失败: %v", err))
	} else {
		printSuccess("测试数据已清理")
	}

	recordTest("GEO边界情况", true, "所有边界情况测试通过", time.Since(startTime))
}
