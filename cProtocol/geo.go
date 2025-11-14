package cProtocol

import (
	"math"
)

// GeoRequest GEO操作请求
type GeoRequest struct {
	Operation string                 `json:"operation"` // geoadd, geodist, georadius, geohash, geopos
	Database  string                 `json:"database,omitempty"`
	Key       string                 `json:"key"`                  // 地理索引的键
	Prefix    string                 `json:"prefix,omitempty"`     // 成员名称前缀，用于过滤搜索范围
	Members   []GeoMember            `json:"members,omitempty"`    // 成员信息
	Longitude float64                `json:"longitude,omitempty"`  // 经度
	Latitude  float64                `json:"latitude,omitempty"`   // 纬度
	Member    string                 `json:"member,omitempty"`     // 成员名称
	Unit      string                 `json:"unit,omitempty"`       // 距离单位: m, km, mi, ft
	Radius    float64                `json:"radius,omitempty"`     // 半径
	Metadata  map[string]interface{} `json:"metadata,omitempty"`   // 位置关联的附加数据
	Count     int                    `json:"count,omitempty"`      // 返回结果数量限制
	WithDist  bool                   `json:"with_dist,omitempty"`  // 是否返回距离
	WithCoord bool                   `json:"with_coord,omitempty"` // 是否返回坐标
	WithHash  bool                   `json:"with_hash,omitempty"`  // 是否返回地理位置哈希
	Sort      string                 `json:"sort,omitempty"`       // 排序方式: asc, desc
}

// GeoResponse GEO操作响应
type GeoResponse struct {
	Success bool                   `json:"success"`
	Status  int                    `json:"status"`
	Error   string                 `json:"error,omitempty"`
	Result  []GeoResult            `json:"result,omitempty"`
	Data    map[string]interface{} `json:"data,omitempty"` // 返回的地理数据
	Count   int                    `json:"count"`          // 返回结果数量
}

// GeoMember 地理成员信息
type GeoMember struct {
	Member    string                 `json:"member"`             // 成员名称/ID
	Longitude float64                `json:"longitude"`          // 经度
	Latitude  float64                `json:"latitude"`           // 纬度
	Metadata  map[string]interface{} `json:"metadata,omitempty"` // 该成员的附加数据
}

// GeoResult 地理查询结果
type GeoResult struct {
	Member    string                 `json:"member"`
	Longitude float64                `json:"longitude,omitempty"`
	Latitude  float64                `json:"latitude,omitempty"`
	Distance  float64                `json:"distance,omitempty"`
	Hash      int64                  `json:"hash,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"` // 该成员的附加数据
}

// GeoData 地理数据存储结构
type GeoData struct {
	Member    string                 `json:"member"`
	Longitude float64                `json:"longitude"`
	Latitude  float64                `json:"latitude"`
	Metadata  map[string]interface{} `json:"metadata"` // 附加的键值对数据
	Timestamp int64                  `json:"timestamp"`
	TTL       int64                  `json:"ttl,omitempty"` // 过期时间戳
}

// GeoIndexData 地理索引数据（用于内部存储）
type GeoIndexData struct {
	Members map[string]*GeoData `json:"members"` // member_id -> GeoData
	Updated int64               `json:"updated"`
}

// GeoDistanceResult 地理距离计算结果
type GeoDistanceResult struct {
	Member1  string  `json:"member1"`
	Member2  string  `json:"member2"`
	Distance float64 `json:"distance"`
	Unit     string  `json:"unit"`
}

// GeoRadiusQuery 地理半径查询
type GeoRadiusQuery struct {
	Center    *GeoMember
	Radius    float64
	Unit      string
	Count     int
	WithDist  bool
	WithCoord bool
	WithHash  bool
	Sort      string
}

// IsValidUnit 验证距离单位是否有效
func IsValidUnit(unit string) bool {
	switch unit {
	case "m", "km", "mi", "ft":
		return true
	}
	return false
}

// GetUnitMultiplier 获取单位乘数（转换为米）
func GetUnitMultiplier(unit string) float64 {
	switch unit {
	case "m":
		return 1.0
	case "km":
		return 1000.0
	case "mi":
		return 1609.34
	case "ft":
		return 0.3048
	default:
		return 1.0
	}
}

// ConvertDistance 转换距离到指定单位
func ConvertDistance(meters float64, unit string) float64 {
	multiplier := GetUnitMultiplier(unit)
	if multiplier == 0 {
		multiplier = 1.0
	}
	return meters / multiplier
}

// Haversine 计算两点之间的距离（米）
func Haversine(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371000.0 // 地球半径（米）

	phi1 := lat1 * math.Pi / 180.0
	phi2 := lat2 * math.Pi / 180.0
	deltaPhi := (lat2 - lat1) * math.Pi / 180.0
	deltaLambda := (lon2 - lon1) * math.Pi / 180.0

	a := math.Sin(deltaPhi/2)*math.Sin(deltaPhi/2) +
		math.Cos(phi1)*math.Cos(phi2)*math.Sin(deltaLambda/2)*math.Sin(deltaLambda/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return R * c
}

func toRad(deg float64) float64 {
	return deg * 3.141592653589793 / 180.0
}

func sin(x float64) float64 {
	return sinApprox(x)
}

func cos(x float64) float64 {
	return cosApprox(x)
}

func sqrt(x float64) float64 {
	if x < 0 {
		return 0
	}
	return sqrtApprox(x)
}

func atan2(y, x float64) float64 {
	return atan2Approx(y, x)
}

// sinApprox 正弦函数近似
func sinApprox(x float64) float64 {
	// 标准化到 [-π, π]
	const pi = 3.141592653589793
	const twoPi = 6.283185307179586

	x = x - (x/twoPi)*twoPi
	if x > pi {
		x = x - twoPi
	} else if x < -pi {
		x = x + twoPi
	}

	// 使用泰勒级数
	x2 := x * x
	return x * (1 - x2/6*(1-x2/20*(1-x2/42)))
}

// cosApprox 余弦函数近似
func cosApprox(x float64) float64 {
	const pi = 3.141592653589793
	const halfPi = 1.5707963267948966
	return sinApprox(halfPi - x)
}

// sqrtApprox 平方根近似（牛顿法）
func sqrtApprox(x float64) float64 {
	if x == 0 {
		return 0
	}
	z := x
	for i := 0; i < 10; i++ {
		z = (z + x/z) / 2
	}
	return z
}

// atan2Approx 反正切函数近似
func atan2Approx(y, x float64) float64 {
	const pi = 3.141592653589793
	const halfPi = 1.5707963267948966

	if x > 0 {
		return atanApprox(y / x)
	} else if x < 0 && y >= 0 {
		return atanApprox(y/x) + pi
	} else if x < 0 && y < 0 {
		return atanApprox(y/x) - pi
	} else if x == 0 && y > 0 {
		return halfPi
	} else if x == 0 && y < 0 {
		return -halfPi
	}
	return 0
}

// atanApprox 反正切函数近似
func atanApprox(x float64) float64 {
	return x / (1 + 0.28*x*x)
}
