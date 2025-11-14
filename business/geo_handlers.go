package business

import (
	"context"
	"fmt"
	"time"

	"github.com/bytedance/sonic"

	"github.com/qorm/burin/cProtocol"
)

// ============================================
// GEO 地址位置功能处理器
// ============================================

// parseGeoRequest 解析GEO请求
func parseGeoRequest(data []byte) (*cProtocol.GeoRequest, error) {
	var req cProtocol.GeoRequest
	if err := sonic.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("failed to parse geo request: %w", err)
	}
	return &req, nil
}

// handleGeoAdd 处理 GEOADD 命令 - 添加地理位置
func (e *Engine) handleGeoAdd(ctx context.Context, req *cProtocol.GeoRequest) *cProtocol.GeoResponse {
	if req.Key == "" {
		return &cProtocol.GeoResponse{
			Success: false,
			Status:  400,
			Error:   "key is required",
		}
	}

	if len(req.Members) == 0 {
		return &cProtocol.GeoResponse{
			Success: false,
			Status:  400,
			Error:   "members are required",
		}
	}

	// 数据库在 handleGeoCommands 中已经确定，这里直接使用
	database := req.Database

	if !e.cacheEngine.DatabaseExists(database) {
		return &cProtocol.GeoResponse{
			Success: false,
			Status:  404,
			Error:   fmt.Sprintf("database %s does not exist", database),
		}
	}

	// 验证所有成员的坐标
	for _, member := range req.Members {
		if member.Member == "" {
			return &cProtocol.GeoResponse{
				Success: false,
				Status:  400,
				Error:   "member name is required",
			}
		}
		if member.Longitude < -180 || member.Longitude > 180 {
			return &cProtocol.GeoResponse{
				Success: false,
				Status:  400,
				Error:   fmt.Sprintf("invalid longitude for member %s: %f", member.Member, member.Longitude),
			}
		}
		if member.Latitude < -90 || member.Latitude > 90 {
			return &cProtocol.GeoResponse{
				Success: false,
				Status:  400,
				Error:   fmt.Sprintf("invalid latitude for member %s: %f", member.Member, member.Latitude),
			}
		}
	}

	// 存储地理数据
	if err := e.cacheEngine.SetGeo(req.Database, req.Key, req.Members); err != nil {
		e.logger.Errorf("Failed to add geo data: %v", err)
		return &cProtocol.GeoResponse{
			Success: false,
			Status:  500,
			Error:   fmt.Sprintf("failed to add geo data: %v", err),
		}
	}

	e.logger.Infof("Successfully added %d members to geo index %s in database %s", len(req.Members), req.Key, req.Database)

	// 通过Raft协议将数据同步到其他节点（如果是leader）
	if e.consensusNode != nil && e.consensusNode.IsLeader() {
		e.logger.Infof("Starting Raft replication for geo key=%s to cluster nodes", req.Key)

		// 创建Raft命令
		raftCommand := map[string]interface{}{
			"operation": "geoadd",
			"database":  req.Database,
			"key":       req.Key,
			"members":   req.Members,
			"timestamp": time.Now().UnixNano(),
		}

		commandData, err := sonic.Marshal(raftCommand)
		if err != nil {
			e.logger.Errorf("Failed to marshal Raft command for geo key=%s: %v", req.Key, err)
		} else {
			// 通过Raft Apply命令到集群
			err = e.consensusNode.ApplyCommand(commandData)
			if err != nil {
				e.logger.Warnf("Raft replication failed for geo key=%s: %v", req.Key, err)
			} else {
				e.logger.Infof("Geo data replicated via Raft successfully for key=%s", req.Key)
			}
		}
	}

	// 构建结果
	results := make([]cProtocol.GeoResult, len(req.Members))
	for i, member := range req.Members {
		results[i] = cProtocol.GeoResult{
			Member:    member.Member,
			Longitude: member.Longitude,
			Latitude:  member.Latitude,
			Metadata:  member.Metadata,
		}
	}

	return &cProtocol.GeoResponse{
		Success: true,
		Status:  200,
		Result:  results,
		Count:   len(req.Members),
	}
}

// handleGeoDist 处理 GEODIST 命令 - 计算两个成员之间的距离
func (e *Engine) handleGeoDist(ctx context.Context, req *cProtocol.GeoRequest) *cProtocol.GeoResponse {
	if req.Key == "" {
		return &cProtocol.GeoResponse{
			Success: false,
			Status:  400,
			Error:   "key is required",
		}
	}

	if len(req.Members) < 2 {
		return &cProtocol.GeoResponse{
			Success: false,
			Status:  400,
			Error:   "two members are required to calculate distance",
		}
	}

	// 数据库在 handleGeoCommands 中已经确定，这里直接使用
	database := req.Database

	if !e.cacheEngine.DatabaseExists(database) {
		return &cProtocol.GeoResponse{
			Success: false,
			Status:  404,
			Error:   fmt.Sprintf("database %s does not exist", database),
		}
	}

	unit := req.Unit
	if unit == "" {
		unit = "m"
	}
	if !cProtocol.IsValidUnit(unit) {
		return &cProtocol.GeoResponse{
			Success: false,
			Status:  400,
			Error:   fmt.Sprintf("invalid distance unit: %s", unit),
		}
	}

	// 获取两个成员的坐标
	member1Name := req.Members[0].Member
	member2Name := req.Members[1].Member

	members, err := e.cacheEngine.GetGeo(req.Database, req.Key, []string{member1Name, member2Name})
	if err != nil {
		e.logger.Errorf("Failed to get geo data: %v", err)
		return &cProtocol.GeoResponse{
			Success: false,
			Status:  500,
			Error:   fmt.Sprintf("failed to get geo data: %v", err),
		}
	}

	if len(members) < 2 {
		return &cProtocol.GeoResponse{
			Success: false,
			Status:  404,
			Error:   "one or both members not found",
		}
	}

	member1 := members[0]
	member2 := members[1]

	// 计算距离
	distanceM := cProtocol.Haversine(member1.Latitude, member1.Longitude, member2.Latitude, member2.Longitude)
	distance := cProtocol.ConvertDistance(distanceM, unit)

	e.logger.Infof("Calculated distance between %s and %s: %.2f %s", member1Name, member2Name, distance, unit)

	return &cProtocol.GeoResponse{
		Success: true,
		Status:  200,
		Result: []cProtocol.GeoResult{
			{
				Member:   member1Name,
				Distance: distance,
			},
			{
				Member:   member2Name,
				Distance: distance,
			},
		},
		Count: 1,
	}
}

// handleGeoRadius 处理 GEORADIUS 命令 - 查询半径内的成员
func (e *Engine) handleGeoRadius(ctx context.Context, req *cProtocol.GeoRequest) *cProtocol.GeoResponse {
	if req.Key == "" {
		return &cProtocol.GeoResponse{
			Success: false,
			Status:  400,
			Error:   "key is required",
		}
	}

	// 数据库在 handleGeoCommands 中已经确定，这里直接使用
	database := req.Database

	if !e.cacheEngine.DatabaseExists(database) {
		return &cProtocol.GeoResponse{
			Success: false,
			Status:  404,
			Error:   fmt.Sprintf("database %s does not exist", database),
		}
	}

	// 支持两种模式：
	// 1. 以成员为中心：Members[0] 指定中心成员
	// 2. 以坐标为中心：Longitude 和 Latitude 指定中心坐标
	var centerLon, centerLat float64
	var hasCenterCoords bool

	if len(req.Members) > 0 && req.Members[0].Member != "" {
		// 模式 1：以成员为中心
		centerMember := req.Members[0].Member
		members, err := e.cacheEngine.GetGeo(req.Database, req.Key, []string{centerMember})
		if err != nil || len(members) == 0 {
			return &cProtocol.GeoResponse{
				Success: false,
				Status:  404,
				Error:   fmt.Sprintf("center member %s not found", centerMember),
			}
		}
		centerLon = members[0].Longitude
		centerLat = members[0].Latitude
		hasCenterCoords = true
	} else if req.Operation == "georadius" {
		// 模式 2：以坐标为中心（georadius操作）
		centerLon = req.Longitude
		centerLat = req.Latitude
		hasCenterCoords = true
	}

	if !hasCenterCoords {
		return &cProtocol.GeoResponse{
			Success: false,
			Status:  400,
			Error:   "center member or coordinates are required",
		}
	}

	if req.Radius <= 0 {
		return &cProtocol.GeoResponse{
			Success: false,
			Status:  400,
			Error:   "radius must be positive",
		}
	}

	unit := req.Unit
	if unit == "" {
		unit = "m"
	}
	if !cProtocol.IsValidUnit(unit) {
		return &cProtocol.GeoResponse{
			Success: false,
			Status:  400,
			Error:   fmt.Sprintf("invalid distance unit: %s", unit),
		}
	}

	count := req.Count
	if count <= 0 {
		count = -1 // 无限制
	}

	// 查询半径内的成员，使用 prefix 过滤
	results, err := e.cacheEngine.GeoRadiusByCoord(req.Database, req.Key, centerLon, centerLat, req.Radius, unit, count, req.Prefix)
	if err != nil {
		e.logger.Errorf("Failed to query geo radius: %v", err)
		return &cProtocol.GeoResponse{
			Success: false,
			Status:  500,
			Error:   fmt.Sprintf("failed to query geo radius: %v", err),
		}
	}

	if req.Prefix != "" {
		e.logger.Infof("Found %d members (prefix: %s) within radius %.2f %s from center (%.4f, %.4f)", len(results), req.Prefix, req.Radius, unit, centerLon, centerLat)
	} else {
		e.logger.Infof("Found %d members within radius %.2f %s from center (%.4f, %.4f)", len(results), req.Radius, unit, centerLon, centerLat)
	}

	return &cProtocol.GeoResponse{
		Success: true,
		Status:  200,
		Result:  results,
		Count:   len(results),
	}
}

// handleGeoHash 处理 GEOHASH 命令 - 获取地理位置哈希
func (e *Engine) handleGeoHash(ctx context.Context, req *cProtocol.GeoRequest) *cProtocol.GeoResponse {
	if req.Key == "" {
		return &cProtocol.GeoResponse{
			Success: false,
			Status:  400,
			Error:   "key is required",
		}
	}

	if len(req.Members) == 0 {
		return &cProtocol.GeoResponse{
			Success: false,
			Status:  400,
			Error:   "members are required",
		}
	}

	memberNames := make([]string, len(req.Members))
	for i, m := range req.Members {
		memberNames[i] = m.Member
	}

	// 获取地理位置哈希
	hashes, err := e.cacheEngine.GeoHash(req.Database, req.Key, memberNames)
	if err != nil {
		e.logger.Errorf("Failed to get geo hash: %v", err)
		return &cProtocol.GeoResponse{
			Success: false,
			Status:  500,
			Error:   fmt.Sprintf("failed to get geo hash: %v", err),
		}
	}

	// 构建 Result
	results := make([]cProtocol.GeoResult, 0)
	for member, hashValue := range hashes {
		result := cProtocol.GeoResult{
			Member: member,
		}

		// 处理不同的哈希值类型
		if hash, ok := hashValue.(int64); ok {
			result.Hash = hash
		} else if hashStr, ok := hashValue.(string); ok {
			// 如果是字符串，需要转换为某种整数表示
			// 可以计算字符串的哈希值或者保存为原始字符串
			// 这里我们简单地将字符串转为 int64（如果可能）
			var h int64
			fmt.Sscanf(hashStr, "%d", &h)
			result.Hash = h
		}

		results = append(results, result)
	}

	return &cProtocol.GeoResponse{
		Success: true,
		Status:  200,
		Result:  results,
		Count:   len(results),
	}
}

// handleGeoPos 处理 GEOPOS 命令 - 获取成员的坐标
func (e *Engine) handleGeoPos(ctx context.Context, req *cProtocol.GeoRequest) *cProtocol.GeoResponse {
	if req.Key == "" {
		return &cProtocol.GeoResponse{
			Success: false,
			Status:  400,
			Error:   "key is required",
		}
	}

	if len(req.Members) == 0 {
		return &cProtocol.GeoResponse{
			Success: false,
			Status:  400,
			Error:   "members are required",
		}
	}

	memberNames := make([]string, len(req.Members))
	for i, m := range req.Members {
		memberNames[i] = m.Member
	}

	// 获取成员坐标
	members, err := e.cacheEngine.GetGeo(req.Database, req.Key, memberNames)
	if err != nil {
		e.logger.Errorf("Failed to get geo position: %v", err)
		return &cProtocol.GeoResponse{
			Success: false,
			Status:  500,
			Error:   fmt.Sprintf("failed to get geo position: %v", err),
		}
	}

	result := make([]cProtocol.GeoResult, len(members))
	for i, member := range members {
		result[i] = cProtocol.GeoResult{
			Member:    member.Member,
			Longitude: member.Longitude,
			Latitude:  member.Latitude,
			Metadata:  member.Metadata,
		}
	}

	return &cProtocol.GeoResponse{
		Success: true,
		Status:  200,
		Result:  result,
		Count:   len(result),
	}
}

// handleGeoGet 处理 GEOGET 命令 - 获取成员的完整信息包括附加数据
func (e *Engine) handleGeoGet(ctx context.Context, req *cProtocol.GeoRequest) *cProtocol.GeoResponse {
	if req.Key == "" {
		return &cProtocol.GeoResponse{
			Success: false,
			Status:  400,
			Error:   "key is required",
		}
	}

	if len(req.Members) == 0 {
		return &cProtocol.GeoResponse{
			Success: false,
			Status:  400,
			Error:   "members are required",
		}
	}

	memberNames := make([]string, len(req.Members))
	for i, m := range req.Members {
		memberNames[i] = m.Member
	}

	// 获取成员信息
	members, err := e.cacheEngine.GetGeo(req.Database, req.Key, memberNames)
	if err != nil {
		e.logger.Errorf("Failed to get geo info: %v", err)
		return &cProtocol.GeoResponse{
			Success: false,
			Status:  500,
			Error:   fmt.Sprintf("failed to get geo info: %v", err),
		}
	}

	result := make([]cProtocol.GeoResult, len(members))
	for i, member := range members {
		result[i] = cProtocol.GeoResult{
			Member:    member.Member,
			Longitude: member.Longitude,
			Latitude:  member.Latitude,
			Metadata:  member.Metadata,
		}
	}

	return &cProtocol.GeoResponse{
		Success: true,
		Status:  200,
		Result:  result,
		Count:   len(result),
	}
}

// handleGeoDel 处理 GEODEL 命令 - 删除地理成员
func (e *Engine) handleGeoDel(ctx context.Context, req *cProtocol.GeoRequest) *cProtocol.GeoResponse {
	if req.Key == "" {
		return &cProtocol.GeoResponse{
			Success: false,
			Status:  400,
			Error:   "key is required",
		}
	}

	if len(req.Members) == 0 {
		return &cProtocol.GeoResponse{
			Success: false,
			Status:  400,
			Error:   "members are required",
		}
	}

	memberNames := make([]string, len(req.Members))
	for i, m := range req.Members {
		memberNames[i] = m.Member
	}

	// 删除成员
	deleted, err := e.cacheEngine.DeleteGeo(req.Database, req.Key, memberNames)
	if err != nil {
		e.logger.Errorf("Failed to delete geo members: %v", err)
		return &cProtocol.GeoResponse{
			Success: false,
			Status:  500,
			Error:   fmt.Sprintf("failed to delete geo members: %v", err),
		}
	}

	e.logger.Infof("Deleted %d members from geo index %s", deleted, req.Key)

	// 构建包含被删除成员名称的 Result
	results := make([]cProtocol.GeoResult, 0)
	for _, member := range req.Members {
		results = append(results, cProtocol.GeoResult{
			Member: member.Member,
		})
	}

	return &cProtocol.GeoResponse{
		Success: true,
		Status:  200,
		Result:  results,
		Count:   deleted,
	}
}
