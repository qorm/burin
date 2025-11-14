package store

import (
	"fmt"
	"hash/crc32"
	"strings"
	"time"

	"github.com/qorm/burin/cProtocol"

	"github.com/bytedance/sonic"
	"github.com/dgraph-io/badger/v4"
)

// GeoIndexMeta GEO 索引元数据
type GeoIndexMeta struct {
	Key     string `json:"key"`
	Created int64  `json:"created"`
	Updated int64  `json:"updated"`
	Count   int    `json:"count"`
}

// GeoCoordData GEO 坐标数据
type GeoCoordData struct {
	Longitude float64 `json:"longitude"`
	Latitude  float64 `json:"latitude"`
	Timestamp int64   `json:"timestamp"`
}

// buildGeoIndexKey 构建 GEO 索引键
func buildGeoIndexKey(key string) string {
	return "geo::index::" + key
}

// buildGeoCoordKey 构建 GEO 坐标键
func buildGeoCoordKey(key string, member string) string {
	return "geo::coord::" + key + "::" + member
}

// buildGeoMetaKey 构建 GEO 元数据键
func buildGeoMetaKey(key string, member string) string {
	return "geo::meta::" + key + "::" + member
}

// buildGeoCoordPrefix 构建 GEO 坐标前缀（用于扫描所有成员）
func buildGeoCoordPrefix(key string) string {
	return "geo::coord::" + key + "::"
}

// buildGeoCoordPrefixWithFilter 构建带过滤的 GEO 坐标前缀
func buildGeoCoordPrefixWithFilter(key string, memberPrefix string) string {
	return "geo::coord::" + key + "::" + memberPrefix
}

// parseMemberNameFromKey 从完整键中解析成员名称
func parseMemberNameFromKey(fullKey string, keyPrefix string) string {
	if !strings.HasPrefix(fullKey, keyPrefix) {
		return ""
	}
	return fullKey[len(keyPrefix):]
}

// getShardForDatabase 获取指定数据库的分片
func (e *ShardedCacheEngine) getShardForDatabase(database string, key string) (*CacheShard, error) {
	e.mu.RLock()
	shards, ok := e.databases[database]
	e.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("database not found: %s", database)
	}

	if len(shards) == 0 {
		return nil, fmt.Errorf("no shards available for database: %s", database)
	}

	hash := crc32.ChecksumIEEE([]byte(key)) % uint32(len(shards))
	return shards[hash], nil
}

// SetGeo 存储地理数据（使用新的分离存储结构）
func (e *ShardedCacheEngine) SetGeo(database string, key string, members []cProtocol.GeoMember) error {
	if database == "" {
		database = "default"
	}

	indexKey := buildGeoIndexKey(key)
	indexShard, err := e.getShardForDatabase(database, indexKey)
	if err != nil {
		return err
	}

	// 读取或创建索引元数据
	indexMeta := &GeoIndexMeta{
		Key:     key,
		Created: time.Now().Unix(),
		Updated: time.Now().Unix(),
		Count:   0,
	}

	existingData, found, err := indexShard.Get(indexKey)
	if err == nil && found {
		if unmarshalErr := sonic.Unmarshal(existingData, indexMeta); unmarshalErr == nil {
			indexMeta.Updated = time.Now().Unix()
		}
	}

	// 存储每个成员的坐标和元数据
	for _, member := range members {
		// 存储坐标数据
		coordKey := buildGeoCoordKey(key, member.Member)
		coordShard, err := e.getShardForDatabase(database, coordKey)
		if err != nil {
			return fmt.Errorf("failed to get shard for coord %s: %w", member.Member, err)
		}

		coordData := &GeoCoordData{
			Longitude: member.Longitude,
			Latitude:  member.Latitude,
			Timestamp: time.Now().Unix(),
		}

		coordBytes, err := sonic.Marshal(coordData)
		if err != nil {
			return fmt.Errorf("failed to marshal coord data: %w", err)
		}

		if err := coordShard.Set(coordKey, coordBytes, 0); err != nil {
			return fmt.Errorf("failed to set coord: %w", err)
		}

		// 存储元数据（如果有）
		if len(member.Metadata) > 0 {
			metaKey := buildGeoMetaKey(key, member.Member)
			metaShard, err := e.getShardForDatabase(database, metaKey)
			if err != nil {
				return fmt.Errorf("failed to get shard for meta %s: %w", member.Member, err)
			}

			metaBytes, err := sonic.Marshal(member.Metadata)
			if err != nil {
				return fmt.Errorf("failed to marshal metadata: %w", err)
			}

			if err := metaShard.Set(metaKey, metaBytes, 0); err != nil {
				return fmt.Errorf("failed to set metadata: %w", err)
			}
		}
	}

	// 更新成员计数
	memberCount, err := e.scanGeoMembers(database, key, "", true)
	if err == nil {
		indexMeta.Count = int(memberCount)
	}

	// 保存索引元数据
	metaData, err := sonic.Marshal(indexMeta)
	if err != nil {
		return fmt.Errorf("failed to marshal index meta: %w", err)
	}

	return indexShard.Set(indexKey, metaData, 0)
}

// GetGeo 获取地理数据
func (e *ShardedCacheEngine) GetGeo(database string, key string, memberNames []string) ([]cProtocol.GeoData, error) {
	if database == "" {
		database = "default"
	}

	var results []cProtocol.GeoData

	for _, memberName := range memberNames {
		// 获取坐标
		coordKey := buildGeoCoordKey(key, memberName)
		coordShard, err := e.getShardForDatabase(database, coordKey)
		if err != nil {
			continue
		}

		coordBytes, found, err := coordShard.Get(coordKey)
		if err != nil || !found {
			continue
		}

		var coordData GeoCoordData
		if err := sonic.Unmarshal(coordBytes, &coordData); err != nil {
			continue
		}

		// 获取元数据（可选）
		var metadata map[string]interface{}
		metaKey := buildGeoMetaKey(key, memberName)
		metaShard, err := e.getShardForDatabase(database, metaKey)
		if err == nil {
			metaBytes, found, err := metaShard.Get(metaKey)
			if err == nil && found {
				// 反序列化为 map[string]interface{}
				sonic.Unmarshal(metaBytes, &metadata)
			}
		}

		geoData := cProtocol.GeoData{
			Member:    memberName,
			Longitude: coordData.Longitude,
			Latitude:  coordData.Latitude,
			Metadata:  metadata,
			Timestamp: coordData.Timestamp,
		}

		results = append(results, geoData)
	}

	return results, nil
}

// DeleteGeo 删除地理成员
func (e *ShardedCacheEngine) DeleteGeo(database string, key string, memberNames []string) (int, error) {
	if database == "" {
		database = "default"
	}

	deleted := 0

	for _, memberName := range memberNames {
		// 删除坐标
		coordKey := buildGeoCoordKey(key, memberName)
		coordShard, err := e.getShardForDatabase(database, coordKey)
		if err == nil {
			if err := coordShard.Delete(coordKey); err == nil {
				deleted++
			}
		}

		// 删除元数据
		metaKey := buildGeoMetaKey(key, memberName)
		metaShard, err := e.getShardForDatabase(database, metaKey)
		if err == nil {
			metaShard.Delete(metaKey)
		}
	}

	// 更新索引元数据
	indexKey := buildGeoIndexKey(key)
	indexShard, err := e.getShardForDatabase(database, indexKey)
	if err == nil {
		memberCount, err := e.scanGeoMembers(database, key, "", true)
		if err == nil && memberCount == 0 {
			// 没有成员了，删除索引
			indexShard.Delete(indexKey)
		} else if err == nil {
			// 更新计数
			existingData, found, err := indexShard.Get(indexKey)
			if err == nil && found {
				var indexMeta GeoIndexMeta
				if sonic.Unmarshal(existingData, &indexMeta) == nil {
					indexMeta.Count = int(memberCount)
					indexMeta.Updated = time.Now().Unix()
					metaData, _ := sonic.Marshal(indexMeta)
					indexShard.Set(indexKey, metaData, 0)
				}
			}
		}
	}

	return deleted, nil
}

// scanGeoMembers 扫描 GEO 成员（使用 BadgerDB 前缀扫描）
func (e *ShardedCacheEngine) scanGeoMembers(database string, key string, memberPrefix string, countOnly bool) (int64, error) {
	if database == "" {
		database = "default"
	}

	// 构建扫描前缀
	var scanPrefix string
	if memberPrefix == "" {
		scanPrefix = buildGeoCoordPrefix(key)
	} else {
		scanPrefix = buildGeoCoordPrefixWithFilter(key, memberPrefix)
	}

	e.mu.RLock()
	shards, exists := e.databases[database]
	e.mu.RUnlock()

	if !exists {
		return 0, fmt.Errorf("database %s not found", database)
	}

	var totalCount int64

	// 扫描所有分片
	for _, shard := range shards {
		count, err := shard.scanKeysWithPrefix(scanPrefix, countOnly)
		if err != nil {
			return 0, err
		}
		totalCount += count
	}

	return totalCount, nil
}

// getAllGeoMembers 获取所有地理成员（使用 BadgerDB 前缀扫描）
func (e *ShardedCacheEngine) getAllGeoMembers(database string, key string, memberPrefix string) ([]cProtocol.GeoData, error) {
	if database == "" {
		database = "default"
	}

	// 构建扫描前缀
	var scanPrefix string
	var coordPrefix string
	if memberPrefix == "" {
		scanPrefix = buildGeoCoordPrefix(key)
		coordPrefix = scanPrefix
	} else {
		scanPrefix = buildGeoCoordPrefixWithFilter(key, memberPrefix)
		coordPrefix = buildGeoCoordPrefix(key)
	}

	e.mu.RLock()
	shards, exists := e.databases[database]
	e.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("database %s not found", database)
	}

	var results []cProtocol.GeoData

	// 扫描所有分片
	for _, shard := range shards {
		members, err := shard.scanGeoMembersWithPrefix(scanPrefix, coordPrefix)
		if err != nil {
			continue
		}

		// 为每个成员加载元数据
		for _, member := range members {
			metaKey := buildGeoMetaKey(key, member.Member)
			metaShard, err := e.getShardForDatabase(database, metaKey)
			if err == nil {
				metaBytes, found, err := metaShard.Get(metaKey)
				if err == nil && found {
					var metadata map[string]interface{}
					if sonic.Unmarshal(metaBytes, &metadata) == nil {
						member.Metadata = metadata
					}
				}
			}
			results = append(results, member)
		}
	}

	return results, nil
}

// scanKeysWithPrefix 在分片中扫描匹配前缀的键（只统计）
func (s *CacheShard) scanKeysWithPrefix(prefix string, countOnly bool) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var count int64

	err := s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := txn.NewIterator(opts)
		defer it.Close()

		prefixBytes := []byte(prefix)

		for it.Seek(prefixBytes); it.ValidForPrefix(prefixBytes); it.Next() {
			count++
		}

		return nil
	})

	return count, err
}

// scanGeoMembersWithPrefix 在分片中扫描 GEO 成员
func (s *CacheShard) scanGeoMembersWithPrefix(scanPrefix string, coordPrefix string) ([]cProtocol.GeoData, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []cProtocol.GeoData

	err := s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = true
		it := txn.NewIterator(opts)
		defer it.Close()

		scanPrefixBytes := []byte(scanPrefix)

		for it.Seek(scanPrefixBytes); it.ValidForPrefix(scanPrefixBytes); it.Next() {
			item := it.Item()
			key := string(item.Key())

			// 解析成员名称
			memberName := parseMemberNameFromKey(key, coordPrefix)
			if memberName == "" {
				continue
			}

			// 读取坐标数据
			err := item.Value(func(val []byte) error {
				var coordData GeoCoordData
				if err := sonic.Unmarshal(val, &coordData); err != nil {
					return err
				}

				geoData := cProtocol.GeoData{
					Member:    memberName,
					Longitude: coordData.Longitude,
					Latitude:  coordData.Latitude,
					Timestamp: coordData.Timestamp,
				}

				results = append(results, geoData)
				return nil
			})

			if err != nil {
				continue
			}
		}

		return nil
	})

	return results, err
}

// GeoRadius 半径查询（以成员为中心）
func (e *ShardedCacheEngine) GeoRadius(database string, key string, centerMember string, radius float64, unit string, count int, prefix string) ([]cProtocol.GeoResult, error) {
	if database == "" {
		database = "default"
	}

	// 获取中心成员坐标
	members, err := e.GetGeo(database, key, []string{centerMember})
	if err != nil || len(members) == 0 {
		return nil, fmt.Errorf("center member not found: %s", centerMember)
	}

	centerData := members[0]
	radiusM := radius * cProtocol.GetUnitMultiplier(unit)

	// 使用前缀扫描获取所有成员
	allMembers, err := e.getAllGeoMembers(database, key, prefix)
	if err != nil {
		return nil, err
	}

	var results []cProtocol.GeoResult
	for _, member := range allMembers {
		distM := cProtocol.Haversine(centerData.Latitude, centerData.Longitude, member.Latitude, member.Longitude)
		if distM <= radiusM {
			result := cProtocol.GeoResult{
				Member:    member.Member,
				Longitude: member.Longitude,
				Latitude:  member.Latitude,
				Distance:  cProtocol.ConvertDistance(distM, unit),
				Metadata:  member.Metadata,
			}
			results = append(results, result)
		}
	}

	if count > 0 && len(results) > count {
		results = results[:count]
	}

	return results, nil
}

// GeoRadiusByCoord 查询指定坐标半径内的成员（使用 BadgerDB 前缀扫描）
func (e *ShardedCacheEngine) GeoRadiusByCoord(database string, key string, centerLon float64, centerLat float64, radius float64, unit string, count int, prefix string) ([]cProtocol.GeoResult, error) {
	if database == "" {
		database = "default"
	}

	radiusM := radius * cProtocol.GetUnitMultiplier(unit)

	// 使用前缀扫描获取所有成员
	allMembers, err := e.getAllGeoMembers(database, key, prefix)
	if err != nil {
		return nil, err
	}

	var results []cProtocol.GeoResult
	for _, member := range allMembers {
		distM := cProtocol.Haversine(centerLat, centerLon, member.Latitude, member.Longitude)
		if distM <= radiusM {
			result := cProtocol.GeoResult{
				Member:    member.Member,
				Longitude: member.Longitude,
				Latitude:  member.Latitude,
				Distance:  cProtocol.ConvertDistance(distM, unit),
				Metadata:  member.Metadata,
			}
			results = append(results, result)
		}
	}

	if count > 0 && len(results) > count {
		results = results[:count]
	}

	return results, nil
}

// GeoHash 获取地理哈希
func (e *ShardedCacheEngine) GeoHash(database string, key string, memberNames []string) (map[string]interface{}, error) {
	members, err := e.GetGeo(database, key, memberNames)
	if err != nil {
		return nil, err
	}

	result := make(map[string]interface{})
	for _, member := range members {
		hash := generateGeoHashInt64(member.Latitude, member.Longitude)
		result[member.Member] = hash
	}

	return result, nil
}

// generateGeoHashInt64 生成 GeoHash (int64 格式)
func generateGeoHashInt64(lat, lon float64) int64 {
	latInt := int64((lat + 90.0) * 10000000)
	lonInt := int64((lon + 180.0) * 10000000)
	return (latInt << 32) | lonInt
}
