package main

import (
	"fmt"

	"github.com/qorm/burin/client"
	"github.com/qorm/burin/client/interfaces"
)

// main 演示 GEO 客户端的基本用法
func main() {
	// 创建客户端
	clientConfig := client.NewDefaultConfig()
	clientConfig.Connection.Endpoint = "127.0.0.1:8099"

	c, err := client.NewClient(clientConfig)
	if err != nil {
		fmt.Printf("Failed to create client: %v\n", err)
		return
	}

	if err := c.Connect(); err != nil {
		fmt.Printf("Failed to connect: %v\n", err)
		return
	}
	defer c.Disconnect()

	// 使用 GEO 客户端
	geoClient := c.Geo()

	// ===== GEO 基本操作 =====

	// 1. 添加地理位置
	fmt.Println("\n=== GEO Add ===")
	members := []interfaces.GeoMember{
		{
			Name:      "Beijing",
			Longitude: 116.3972,
			Latitude:  39.9075,
			Metadata: map[string]string{
				"city":     "Beijing",
				"country":  "China",
				"timezone": "UTC+8",
			},
		},
		{
			Name:      "Shanghai",
			Longitude: 121.4737,
			Latitude:  31.2304,
			Metadata: map[string]string{
				"city":     "Shanghai",
				"country":  "China",
				"timezone": "UTC+8",
			},
		},
		{
			Name:      "Hangzhou",
			Longitude: 120.1551,
			Latitude:  30.2741,
			Metadata: map[string]string{
				"city":     "Hangzhou",
				"country":  "China",
				"timezone": "UTC+8",
			},
		},
	}

	err = geoClient.GeoAdd("cities", members, interfaces.WithGeoDatabase("geo_db"))
	if err != nil {
		fmt.Printf("GeoAdd failed: %v\n", err)
		return
	}
	fmt.Println("✓ Added 3 cities")

	// 2. 计算距离
	fmt.Println("\n=== GEO Distance ===")
	dist, err := geoClient.GeoDist("cities", "Beijing", "Shanghai", "km", interfaces.WithGeoDatabase("geo_db"))
	if err != nil {
		fmt.Printf("GeoDist failed: %v\n", err)
		return
	}
	fmt.Printf("✓ Distance Beijing to Shanghai: %.2f km\n", dist)

	// 3. 范围查询
	fmt.Println("\n=== GEO Radius ===")
	results, err := geoClient.GeoRadius("cities", 121.4737, 31.2304, 500, "km")
	if err != nil {
		fmt.Printf("GeoRadius failed: %v\n", err)
		return
	}
	fmt.Printf("✓ Found %d cities within 500km of Shanghai:\n", len(results))
	for _, r := range results {
		fmt.Printf("  - %s: distance=%.2f %s\n", r.Name, r.Distance, r.Unit)
	}

	// 4. 获取地理哈希
	fmt.Println("\n=== GEO Hash ===")
	hashes, err := geoClient.GeoHash("cities", []string{"Beijing", "Shanghai"})
	if err != nil {
		fmt.Printf("GeoHash failed: %v\n", err)
		return
	}
	fmt.Printf("✓ Hashes for Beijing and Shanghai: %v\n", hashes)

	// 5. 获取位置坐标
	fmt.Println("\n=== GEO Pos ===")
	positions, err := geoClient.GeoPos("cities", []string{"Beijing", "Shanghai"})
	if err != nil {
		fmt.Printf("GeoPos failed: %v\n", err)
		return
	}
	fmt.Printf("✓ Positions of cities:\n")
	for _, p := range positions {
		fmt.Printf("  - %s: (%.4f, %.4f)\n", p.Name, p.Longitude, p.Latitude)
	}

	// 6. 获取完整位置信息
	fmt.Println("\n=== GEO Get ===")
	geoData, err := geoClient.GeoGet("cities", "Beijing")
	if err != nil {
		fmt.Printf("GeoGet failed: %v\n", err)
		return
	}
	fmt.Printf("✓ Beijing info:\n")
	fmt.Printf("  - Coordinates: (%.4f, %.4f)\n", geoData.Longitude, geoData.Latitude)
	fmt.Printf("  - Metadata: %v\n", geoData.Metadata)

	// 7. 删除位置
	fmt.Println("\n=== GEO Delete ===")
	count, err := geoClient.GeoDel("cities", []string{"Hangzhou"})
	if err != nil {
		fmt.Printf("GeoDel failed: %v\n", err)
		return
	}
	fmt.Printf("✓ Deleted %d city\n", count)

	// 8. 高级搜索
	fmt.Println("\n=== GEO Search ===")
	searchParams := &interfaces.GeoSearchParams{
		Longitude: 121.4737,
		Latitude:  31.2304,
		Radius:    1000,
		Unit:      "km",
		Count:     10,
	}
	searchResults, err := geoClient.GeoSearch("cities", searchParams)
	if err != nil {
		fmt.Printf("GeoSearch failed: %v\n", err)
		return
	}
	fmt.Printf("✓ Search results: %d results found\n", len(searchResults))

	// ===== 也可以直接通过 BurinClient 访问 =====
	fmt.Println("\n=== Using BurinClient Convenience Methods ===")

	// 直接调用便捷方法
	err = c.GeoAdd("stations", []interfaces.GeoMember{
		{
			Name:      "Station1",
			Longitude: 116.3972,
			Latitude:  39.9075,
			Metadata:  map[string]string{"type": "train"},
		},
	})
	if err != nil {
		fmt.Printf("GeoAdd via BurinClient failed: %v\n", err)
		return
	}
	fmt.Println("✓ Added station via BurinClient convenience method")
}
