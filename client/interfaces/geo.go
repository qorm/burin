package interfaces

import (
	"time"
)

// GeoInterface 地理位置操作接口
type GeoInterface interface {
	// GeoAdd 添加地理位置信息
	// 参数：
	//   - key: 存储键
	//   - members: 地理位置成员列表
	//   - opts: 操作选项（数据库、元数据等）
	GeoAdd(key string, members []GeoMember, opts ...GeoOption) error

	// GeoDist 计算两个地理位置之间的距离
	// 参数：
	//   - key: 存储键
	//   - member1, member2: 要比较的成员名称
	//   - unit: 距离单位 ("m", "km", "mi", "ft")
	// 返回：距离值
	GeoDist(key, member1, member2, unit string, opts ...GeoOption) (float64, error)

	// GeoRadius 查询指定范围内的地理位置
	// 参数：
	//   - key: 存储键
	//   - lon, lat: 中心点经纬度
	//   - radius: 半径
	//   - unit: 距离单位 ("m", "km", "mi", "ft")
	// 返回：查询结果列表
	GeoRadius(key string, lon, lat, radius float64, unit string, opts ...GeoOption) ([]GeoResult, error)

	// GeoHash 获取地理位置的哈希值
	// 参数：
	//   - key: 存储键
	//   - members: 成员名称列表
	// 返回：哈希值列表
	GeoHash(key string, members []string, opts ...GeoOption) ([]string, error)

	// GeoPos 获取地理位置的经纬度
	// 参数：
	//   - key: 存储键
	//   - members: 成员名称列表
	// 返回：位置信息列表
	GeoPos(key string, members []string, opts ...GeoOption) ([]GeoPosition, error)

	// GeoGet 获取地理位置信息
	// 参数：
	//   - key: 存储键
	//   - member: 成员名称
	// 返回：完整的地理位置信息
	GeoGet(key, member string, opts ...GeoOption) (*GeoData, error)

	// GeoDel 删除地理位置
	// 参数：
	//   - key: 存储键
	//   - members: 要删除的成员名称列表
	GeoDel(key string, members []string, opts ...GeoOption) (int64, error)

	// GeoSearch 高级地理位置搜索
	// 参数：
	//   - key: 存储键
	//   - searchParams: 搜索参数
	// 返回：搜索结果列表
	GeoSearch(key string, searchParams *GeoSearchParams, opts ...GeoOption) ([]GeoResult, error)
}

// GeoMember 地理位置成员
type GeoMember struct {
	Name      string            `json:"name"`      // 成员名称
	Longitude float64           `json:"longitude"` // 经度 (-180 ~ 180)
	Latitude  float64           `json:"latitude"`  // 纬度 (-90 ~ 90)
	Metadata  map[string]string `json:"metadata"`  // 附加元数据
}

// GeoPosition 地理位置坐标
type GeoPosition struct {
	Name      string    `json:"name"`
	Longitude float64   `json:"longitude"`
	Latitude  float64   `json:"latitude"`
	Timestamp time.Time `json:"timestamp"`
}

// GeoData 地理位置数据
type GeoData struct {
	Name      string            `json:"name"`
	Longitude float64           `json:"longitude"`
	Latitude  float64           `json:"latitude"`
	Metadata  map[string]string `json:"metadata"`
	Timestamp time.Time         `json:"timestamp"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// GeoResult 地理位置查询结果
type GeoResult struct {
	Name      string            `json:"name"`
	Distance  float64           `json:"distance"`
	Unit      string            `json:"unit"`
	Longitude float64           `json:"longitude"`
	Latitude  float64           `json:"latitude"`
	Metadata  map[string]string `json:"metadata"`
	Timestamp time.Time         `json:"timestamp"`
}

// GeoSearchParams 地理位置搜索参数
type GeoSearchParams struct {
	Longitude float64 `json:"longitude"`  // 中心点经度
	Latitude  float64 `json:"latitude"`   // 中心点纬度
	Radius    float64 `json:"radius"`     // 搜索半径
	Unit      string  `json:"unit"`       // 距离单位
	Count     int32   `json:"count"`      // 返回结果数量
	Sort      string  `json:"sort"`       // 排序方式 ("ASC", "DESC")
	WithCoord bool    `json:"with_coord"` // 是否返回坐标
	WithDist  bool    `json:"with_dist"`  // 是否返回距离
}

// GeoOption Geo操作选项函数
type GeoOption func(*GeoOptions)

// GeoOptions Geo操作选项结构
type GeoOptions struct {
	Database   string
	Metadata   map[string]string
	Consistent bool
	Timeout    time.Duration
	Prefix     string // 成员名称前缀过滤
}

// WithGeoDatabase 设置数据库
func WithGeoDatabase(db string) GeoOption {
	return func(opts *GeoOptions) {
		opts.Database = db
	}
}

// WithGeoMetadata 设置元数据
func WithGeoMetadata(metadata map[string]string) GeoOption {
	return func(opts *GeoOptions) {
		opts.Metadata = metadata
	}
}

// WithGeoConsistent 设置强一致性
func WithGeoConsistent(consistent bool) GeoOption {
	return func(opts *GeoOptions) {
		opts.Consistent = consistent
	}
}

// WithGeoTimeout 设置超时时间
func WithGeoTimeout(timeout time.Duration) GeoOption {
	return func(opts *GeoOptions) {
		opts.Timeout = timeout
	}
}

// WithGeoPrefix 设置成员名称前缀过滤
func WithGeoPrefix(prefix string) GeoOption {
	return func(opts *GeoOptions) {
		opts.Prefix = prefix
	}
}
