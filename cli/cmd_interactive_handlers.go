// 数据库管理函数补充
package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"burin/cProtocol"
	"burin/client"
	"burin/client/interfaces"

	"github.com/bytedance/sonic"
)

// ============ 数据库管理函数 ============

func handleDBUse(c *client.BurinClient, database string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 构造 USE 数据库请求
	head := cProtocol.CreateClientCommandHead(cProtocol.CmdUseDB)
	reqData := map[string]string{"database": database}
	data, _ := sonic.Marshal(reqData)

	resp, err := c.SendAuthRequest(ctx, head, data)

	if err != nil {
		fmt.Printf("错误: %v\n", err)
		return false
	}

	if resp.Status != 200 {
		fmt.Printf("错误: %s\n", resp.Error)
		return false
	}

	return true
}

func handleDBCreate(c *client.BurinClient, database string) {
	if strings.HasPrefix(database, "__burin_") {
		fmt.Println("错误: database '__burin_*' is reserved for system use")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 构造创建数据库请求
	head := cProtocol.CreateClientCommandHead(cProtocol.CmdCreateDB)
	reqData := map[string]string{"database": database}
	data, _ := sonic.Marshal(reqData)

	resp, err := c.SendAuthRequest(ctx, head, data)

	if err != nil {
		fmt.Printf("错误: %v\n", err)
		return
	}

	if resp.Status != 200 {
		fmt.Printf("错误: %s\n", resp.Error)
		return
	}

	fmt.Printf("数据库 '%s' 创建成功\n", database)
}

func handleDBList(c *client.BurinClient) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	head := cProtocol.CreateClientCommandHead(cProtocol.CmdListDBs)
	resp, err := c.SendAuthRequest(ctx, head, nil)
	if err != nil {
		fmt.Printf("错误: %v\n", err)
		return
	}

	if resp.Status != 200 {
		fmt.Printf("错误: %s\n", resp.Error)
		return
	}

	var result struct {
		Databases []string `json:"databases"`
	}
	if err := sonic.Unmarshal(resp.Data, &result); err != nil {
		fmt.Printf("解析错误: %v\n", err)
		return
	}

	fmt.Printf("共 %d 个数据库:\n", len(result.Databases))
	for _, db := range result.Databases {
		fmt.Printf("  %s\n", db)
	}
}

func handleDBDelete(c *client.BurinClient, database string) {
	if strings.HasPrefix(database, "__burin_") {
		fmt.Println("错误: database '__burin_*' is reserved for system use")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	head := cProtocol.CreateClientCommandHead(cProtocol.CmdDeleteDB)
	reqData := map[string]string{"database": database}
	data, _ := sonic.Marshal(reqData)

	resp, err := c.SendAuthRequest(ctx, head, data)
	if err != nil {
		fmt.Printf("错误: %v\n", err)
		return
	}

	if resp.Status != 200 {
		fmt.Printf("错误: %s\n", resp.Error)
		return
	}

	fmt.Printf("数据库 '%s' 删除成功\n", database)
}

func handleDBExists(c *client.BurinClient, database string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	head := cProtocol.CreateClientCommandHead(cProtocol.CmdDBExists)
	reqData := map[string]string{"database": database}
	data, _ := sonic.Marshal(reqData)

	resp, err := c.SendAuthRequest(ctx, head, data)
	if err != nil {
		fmt.Printf("错误: %v\n", err)
		return
	}

	if resp.Status != 200 {
		fmt.Printf("错误: %s\n", resp.Error)
		return
	}

	var result struct {
		Exists bool `json:"exists"`
	}
	if err := sonic.Unmarshal(resp.Data, &result); err != nil {
		fmt.Printf("解析错误: %v\n", err)
		return
	}

	if result.Exists {
		fmt.Printf("数据库 '%s' 存在\n", database)
	} else {
		fmt.Printf("数据库 '%s' 不存在\n", database)
	}
}

func handleDBInfo(c *client.BurinClient, database string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	head := cProtocol.CreateClientCommandHead(cProtocol.CmdDBInfo)
	reqData := map[string]string{"database": database}
	data, _ := sonic.Marshal(reqData)

	resp, err := c.SendAuthRequest(ctx, head, data)
	if err != nil {
		fmt.Printf("错误: %v\n", err)
		return
	}

	if resp.Status != 200 {
		fmt.Printf("错误: %s\n", resp.Error)
		return
	}

	var result map[string]interface{}
	if err := sonic.Unmarshal(resp.Data, &result); err != nil {
		fmt.Printf("解析错误: %v\n", err)
		return
	}

	fmt.Printf("数据库 '%s' 信息:\n", database)
	for k, v := range result {
		fmt.Printf("  %s: %v\n", k, v)
	}
}

// ============ GEO地理位置函数 ============

func handleGeoAdd(c *client.BurinClient, parts []string, database string) {
	// 解析: geoadd <key> <lon> <lat> <member> [metadata...]
	if len(parts) < 5 {
		fmt.Println("用法: geoadd <key> <lon> <lat> <member> [metadata_key:value ...]")
		return
	}

	key := parts[1]
	lon, err1 := strconv.ParseFloat(parts[2], 64)
	lat, err2 := strconv.ParseFloat(parts[3], 64)
	member := parts[4]

	if err1 != nil || err2 != nil {
		fmt.Println("错误: 经纬度必须是数字")
		return
	}

	// 解析元数据 (可选)
	metadata := make(map[string]string)
	for i := 5; i < len(parts); i++ {
		kv := strings.Split(parts[i], ":")
		if len(kv) == 2 {
			metadata[kv[0]] = kv[1]
		}
	}

	members := []interfaces.GeoMember{
		{
			Name:      member,
			Longitude: lon,
			Latitude:  lat,
			Metadata:  metadata,
		},
	}

	err := c.GeoAdd(key, members, interfaces.WithGeoDatabase(database))
	if err != nil {
		fmt.Printf("错误: %v\n", err)
		return
	}

	fmt.Println("OK")
}

func handleGeoDist(c *client.BurinClient, key, member1, member2, unit, database string) {
	dist, err := c.GeoDist(key, member1, member2, unit, interfaces.WithGeoDatabase(database))
	if err != nil {
		fmt.Printf("错误: %v\n", err)
		return
	}

	fmt.Printf("距离: %.2f %s\n", dist, unit)
}

func handleGeoRadius(c *client.BurinClient, parts []string, database string) {
	// georadius <key> <lon> <lat> <radius> <unit> [prefix]
	if len(parts) < 6 {
		fmt.Println("用法: geo radius <key> <lon> <lat> <radius> <unit> [prefix]")
		return
	}

	key := parts[1]
	lon, err1 := strconv.ParseFloat(parts[2], 64)
	lat, err2 := strconv.ParseFloat(parts[3], 64)
	radius, err3 := strconv.ParseFloat(parts[4], 64)
	unit := parts[5]

	if err1 != nil || err2 != nil || err3 != nil {
		fmt.Println("错误: 经纬度和半径必须是数字")
		return
	}

	// 可选的 prefix 参数
	opts := []interfaces.GeoOption{interfaces.WithGeoDatabase(database)}
	if len(parts) >= 7 {
		prefix := parts[6]
		opts = append(opts, interfaces.WithGeoPrefix(prefix))
	}

	results, err := c.GeoRadius(key, lon, lat, radius, unit, opts...)
	if err != nil {
		fmt.Printf("错误: %v\n", err)
		return
	}

	fmt.Printf("找到 %d 个结果:\n", len(results))
	for _, r := range results {
		fmt.Printf("  %s - 距离: %.2f %s\n", r.Name, r.Distance, unit)
	}
}

func handleGeoHash(c *client.BurinClient, key string, members []string, database string) {
	hashes, err := c.GeoHash(key, members, interfaces.WithGeoDatabase(database))
	if err != nil {
		fmt.Printf("错误: %v\n", err)
		return
	}

	for i, hash := range hashes {
		fmt.Printf("%s: %s\n", members[i], hash)
	}
}

func handleGeoPos(c *client.BurinClient, key string, members []string, database string) {
	positions, err := c.GeoPos(key, members, interfaces.WithGeoDatabase(database))
	if err != nil {
		fmt.Printf("错误: %v\n", err)
		return
	}

	for i, pos := range positions {
		fmt.Printf("%s: [%.6f, %.6f]\n", members[i], pos.Longitude, pos.Latitude)
	}
}

func handleGeoGet(c *client.BurinClient, key, member, database string) {
	data, err := c.GeoGet(key, member, interfaces.WithGeoDatabase(database))
	if err != nil {
		fmt.Printf("错误: %v\n", err)
		return
	}

	fmt.Printf("成员: %s\n", data.Name)
	fmt.Printf("经度: %.6f\n", data.Longitude)
	fmt.Printf("纬度: %.6f\n", data.Latitude)
	if len(data.Metadata) > 0 {
		fmt.Println("元数据:")
		for k, v := range data.Metadata {
			fmt.Printf("  %s: %s\n", k, v)
		}
	}
}

func handleGeoDel(c *client.BurinClient, key string, members []string, database string) {
	count, err := c.GeoDel(key, members, interfaces.WithGeoDatabase(database))
	if err != nil {
		fmt.Printf("错误: %v\n", err)
		return
	}

	fmt.Printf("删除了 %d 个成员\n", count)
}

// ============ 用户管理函数 ============

func handleUserCreate(c *client.BurinClient, username, password, role string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	head := cProtocol.CreateAuthCommandHead(cProtocol.AuthCommandCreateUser)
	reqData := map[string]string{
		"username": username,
		"password": password,
		"role":     role,
	}
	data, _ := sonic.Marshal(reqData)

	resp, err := c.SendAuthRequest(ctx, head, data)
	if err != nil {
		fmt.Printf("错误: %v\n", err)
		return
	}

	if resp.Status != 200 {
		fmt.Printf("错误: %s\n", resp.Error)
		return
	}

	fmt.Printf("用户 '%s' 创建成功\n", username)
}

func handleUserDelete(c *client.BurinClient, username string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	head := cProtocol.CreateAuthCommandHead(cProtocol.AuthCommandDeleteUser)
	reqData := map[string]string{"username": username}
	data, _ := sonic.Marshal(reqData)

	resp, err := c.SendAuthRequest(ctx, head, data)
	if err != nil {
		fmt.Printf("错误: %v\n", err)
		return
	}

	if resp.Status != 200 {
		fmt.Printf("错误: %s\n", resp.Error)
		return
	}

	fmt.Printf("用户 '%s' 删除成功\n", username)
}

func handleUserList(c *client.BurinClient) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	head := cProtocol.CreateAuthCommandHead(cProtocol.AuthCommandListUsers)
	resp, err := c.SendAuthRequest(ctx, head, nil)
	if err != nil {
		fmt.Printf("错误: %v\n", err)
		return
	}

	if resp.Status != 200 {
		fmt.Printf("错误: %s\n", resp.Error)
		return
	}

	var result struct {
		Users []struct {
			Username    string `json:"username"`
			Role        string `json:"role"`
			Description string `json:"description"`
		} `json:"users"`
	}

	if err := sonic.Unmarshal(resp.Data, &result); err != nil {
		fmt.Printf("解析错误: %v\n", err)
		return
	}

	fmt.Printf("共 %d 个用户:\n", len(result.Users))
	fmt.Printf("%-20s %-15s %s\n", "用户名", "角色", "描述")
	fmt.Println(strings.Repeat("-", 70))
	for _, user := range result.Users {
		fmt.Printf("%-20s %-15s %s\n", user.Username, user.Role, user.Description)
	}
}

func handleUserInfo(c *client.BurinClient, username string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	head := cProtocol.CreateAuthCommandHead(cProtocol.AuthCommandGetUser)
	reqData := map[string]string{"username": username}
	data, _ := sonic.Marshal(reqData)

	resp, err := c.SendAuthRequest(ctx, head, data)
	if err != nil {
		fmt.Printf("错误: %v\n", err)
		return
	}

	if resp.Status != 200 {
		fmt.Printf("错误: %s\n", resp.Error)
		return
	}

	var user map[string]interface{}
	if err := sonic.Unmarshal(resp.Data, &user); err != nil {
		fmt.Printf("解析错误: %v\n", err)
		return
	}

	fmt.Printf("用户信息:\n")
	for k, v := range user {
		fmt.Printf("  %s: %v\n", k, v)
	}
}

func handleUserGrant(c *client.BurinClient, username, database, permissions string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 解析权限列表
	permList := strings.Split(permissions, ",")
	for i := range permList {
		permList[i] = strings.TrimSpace(permList[i])
	}

	head := cProtocol.CreateAuthCommandHead(cProtocol.AuthCommandGrantPerm)
	reqData := map[string]interface{}{
		"username":    username,
		"database":    database,
		"permissions": permList,
	}
	data, _ := sonic.Marshal(reqData)

	resp, err := c.SendAuthRequest(ctx, head, data)
	if err != nil {
		fmt.Printf("错误: %v\n", err)
		return
	}

	if resp.Status != 200 {
		fmt.Printf("错误: %s\n", resp.Error)
		return
	}

	fmt.Printf("已授予用户 '%s' 对数据库 '%s' 的权限: %s\n", username, database, permissions)
}

func handleUserRevoke(c *client.BurinClient, username, database string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	head := cProtocol.CreateAuthCommandHead(cProtocol.AuthCommandRevokePerm)
	reqData := map[string]string{
		"username": username,
		"database": database,
	}
	data, _ := sonic.Marshal(reqData)

	resp, err := c.SendAuthRequest(ctx, head, data)
	if err != nil {
		fmt.Printf("错误: %v\n", err)
		return
	}

	if resp.Status != 200 {
		fmt.Printf("错误: %s\n", resp.Error)
		return
	}

	fmt.Printf("已撤销用户 '%s' 对数据库 '%s' 的权限\n", username, database)
}

func handleUserPasswd(c *client.BurinClient, username, newPassword string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	head := cProtocol.CreateAuthCommandHead(cProtocol.AuthCommandChangePass)
	reqData := map[string]string{
		"username":     username,
		"new_password": newPassword,
	}
	data, _ := sonic.Marshal(reqData)

	resp, err := c.SendAuthRequest(ctx, head, data)
	if err != nil {
		fmt.Printf("错误: %v\n", err)
		return
	}

	if resp.Status != 200 {
		fmt.Printf("错误: %s\n", resp.Error)
		return
	}

	fmt.Printf("用户 '%s' 密码修改成功\n", username)
}
