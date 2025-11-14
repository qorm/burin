package cProtocol

import "fmt"

// CommandType 定义不同服务的命令类型
type CommandType uint8

const (
	// 服务类型定义
	CommandTypeClient  CommandType = 13 // 客户端服务命令类型
	CommandTypeCluster CommandType = 14 // 集群服务命令类型
	CommandTypeGeo     CommandType = 3  // GEO地址位置服务命令类型
	CommandTypeAuth    CommandType = 4  // 认证和用户管理命令类型
)

var commandTypeNames = map[CommandType]string{
	CommandTypeClient:  "CLIENT",
	CommandTypeCluster: "CLUSTER",
	CommandTypeGeo:     "GEO",
	CommandTypeAuth:    "AUTH",
}

func (ct CommandType) String() string {
	if name, ok := commandTypeNames[ct]; ok {
		return name
	}
	return fmt.Sprintf("UNKNOWN_TYPE_%d", uint32(ct))
}

// Command 协议命令定义
//
// 命令分类说明:
// ===============================================
// 客户端命令 (Client Commands) - 使用 SetCommand/GetCommand
// - 命令空间: 0-127 (7位)
// - 用途: 客户端应用程序与Burin缓存系统交互
// - 协议位置: HEAD.high字节的高7位
// ===============================================
//
// 集群内部命令 (Cluster Internal Commands) - 使用 SetPriCommand/GetPriCommand
// - 命令空间: 0-31 (5位)
// - 用途: 集群节点间的内部通信和协调
// - 协议位置: HEAD.low字节的低5位
// ===============================================
type Command uint8

// 客户端命令定义 (Client Commands)
// 这些命令通过 SetCommand/GetCommand 处理
const (
	CmdNull     Command = iota // 空命令
	CmdCreateDB                // 创建 Cache 类型数据库

	CmdPing        // 客户端连接测试
	CmdPong        // 服务器响应
	CmdGet         // 客户端读取数据
	CmdSet         // 客户端写入数据
	CmdDelete      // 客户端删除数据
	CmdExists      // 客户端检查键存在性
	CmdTransaction // 客户端事务操作
	CmdHealth      // 客户端健康检查
	CmdCount       // 客户端统计计数
	CmdList        // 客户端列表操作

	// 数据库管理命令
	CmdUseDB    // 切换当前数据库
	CmdListDBs  // 列出所有数据库
	CmdDBExists // 检查数据库是否存在
	CmdDBInfo   // 获取数据库信息
	CmdDeleteDB // 删除数据库
)

var commandNames = map[Command]string{
	CmdNull:        "NULL",
	CmdPing:        "PING",
	CmdPong:        "PONG",
	CmdGet:         "GET",
	CmdSet:         "SET",
	CmdDelete:      "DELETE",
	CmdExists:      "EXISTS",
	CmdTransaction: "TRANSACTION",
	CmdHealth:      "HEALTH",
	CmdCount:       "COUNT",
	CmdList:        "LIST",
	CmdCreateDB:    "CREATEDB",
	CmdUseDB:       "USEDB",
	CmdListDBs:     "LISTDBS",
	CmdDBExists:    "DBEXISTS",
	CmdDBInfo:      "DBINFO",
	CmdDeleteDB:    "DELETEDB",
}

func (c Command) String() string {
	if name, ok := commandNames[c]; ok {
		return name
	}
	return fmt.Sprintf("UNKNOWN_%d", uint8(c))
}

// ClusterCommand 集群通信命令定义
//
// 命令分类说明:
// ===============================================
// 集群命令 (Cluster Commands)
// - 命令空间: 0-255 (8位)
// - 协议位置: HEAD.high字节
// - 用途: 节点间内部通信和协调
// ===============================================
type ClusterCommand uint8

const (
	// Raft 共识协议命令 (0-9)
	ClusterCmdNull              ClusterCommand = iota // 空命令
	ClusterCmdRaftAppendEntries                       // Raft日志复制
	ClusterCmdRaftRequestVote                         // Raft投票请求
	ClusterCmdRaftHeartbeat                           // Raft心跳
	ClusterCmdRaftSnapshot                            // Raft快照传输

	// 节点管理命令 (10-19)
	ClusterCmdNodeJoin       = 10 // 节点加入集群请求
	ClusterCmdNodeLeave      = 11 // 节点离开集群
	ClusterCmdNodeDiscovery  = 12 // 节点发现
	ClusterCmdNodeStatus     = 13 // 节点状态同步
	ClusterCmdNodeHealthPing = 14 // 内部健康检查

	// 数据同步命令 (20-29)
	ClusterCmdDataSync     = 20 // 数据同步
	ClusterCmdDataBackup   = 21 // 数据备份
	ClusterCmdDataRestore  = 22 // 数据恢复
	ClusterCmdDataValidate = 23 // 数据验证
	ClusterCmdForward      = 24 // 请求转发（Follower -> Leader）

	// 集群协调命令 (30-31)
	ClusterCmdClusterStatus = 29 // 集群状态查询
	ClusterCmdClusterConfig = 30 // 集群配置更新
	ClusterCmdEmergencyStop = 31 // 紧急停止
)

var clusterCommandNames = map[ClusterCommand]string{
	// 空命令
	ClusterCmdNull: "NULL",

	// Raft 命令
	ClusterCmdRaftAppendEntries: "RAFT_APPEND_ENTRIES",
	ClusterCmdRaftRequestVote:   "RAFT_REQUEST_VOTE",
	ClusterCmdRaftHeartbeat:     "RAFT_HEARTBEAT",
	ClusterCmdRaftSnapshot:      "RAFT_SNAPSHOT",

	// 节点管理
	ClusterCmdNodeJoin:       "NODE_JOIN",
	ClusterCmdNodeLeave:      "NODE_LEAVE",
	ClusterCmdNodeDiscovery:  "NODE_DISCOVERY",
	ClusterCmdNodeStatus:     "NODE_STATUS",
	ClusterCmdNodeHealthPing: "NODE_HEALTH_PING",

	// 数据同步
	ClusterCmdDataSync:     "DATA_SYNC",
	ClusterCmdDataBackup:   "DATA_BACKUP",
	ClusterCmdDataRestore:  "DATA_RESTORE",
	ClusterCmdDataValidate: "DATA_VALIDATE",
	ClusterCmdForward:      "FORWARD",

	// 集群协调
	ClusterCmdClusterStatus: "CLUSTER_STATUS",
	ClusterCmdClusterConfig: "CLUSTER_CONFIG",
	ClusterCmdEmergencyStop: "EMERGENCY_STOP",
}

func (c ClusterCommand) String() string {
	if name, ok := clusterCommandNames[c]; ok {
		return name
	}
	return fmt.Sprintf("CLUSTER_UNKNOWN_%d", uint32(c))
}

// GeoCommand GEO服务命令定义
//
// 命令分类说明:
// ===============================================
// GEO服务命令 (GEO Service Commands)
// - 命令空间: 0-255 (8位)
// - 用途: 地理位置服务相关操作
// - 协议位置: HEAD.high字节
// ===============================================
type GeoCommand uint8

// GEO服务命令定义
const (
	GeoCommandNull    GeoCommand = iota // 空命令
	GeoCommandAdd                       // 添加地理位置 (GEOADD)
	GeoCommandDist                      // 计算距离 (GEODIST)
	GeoCommandRadius                    // 半径查询 (GEORADIUS)
	GeoCommandHash                      // 获取地理哈希 (GEOHASH)
	GeoCommandPos                       // 获取坐标 (GEOPOS)
	GeoCommandGet                       // 获取完整信息 (GEOGET)
	GeoCommandDelete                    // 删除成员 (GEODEL)
	GeoCommandSearch                    // 搜索操作 (GEOSEARCH)
	GeoCommandCluster                   // 集群地理操作
)

var geoCommandNames = map[GeoCommand]string{
	GeoCommandNull:    "NULL",
	GeoCommandAdd:     "ADD",
	GeoCommandDist:    "DIST",
	GeoCommandRadius:  "RADIUS",
	GeoCommandHash:    "HASH",
	GeoCommandPos:     "POS",
	GeoCommandGet:     "GET",
	GeoCommandDelete:  "DELETE",
	GeoCommandSearch:  "SEARCH",
	GeoCommandCluster: "CLUSTER",
}

func (c GeoCommand) String() string {
	if name, ok := geoCommandNames[c]; ok {
		return name
	}
	return fmt.Sprintf("GEO_UNKNOWN_%d", uint8(c))
}

// AuthCommand 认证和用户管理命令定义
//
// 命令分类说明:
// ===============================================
// 认证命令 (Auth Commands)
// - 命令空间: 0-255 (8位)
// - 用途: 用户认证、用户管理、权限管理
// - 协议位置: HEAD.high字节
// - 注意: 这些命令只能访问系统数据库 __burin_system__
// ===============================================
type AuthCommand uint8

// 认证命令定义
const (
	AuthCommandNull AuthCommand = iota // 空命令

	// 认证相关 (1-9)
	AuthCommandLogin    // 用户登录
	AuthCommandLogout   // 用户登出
	AuthCommandValidate // 验证令牌
	AuthCommandRefresh  // 刷新令牌

	// 用户管理 (10-19)
	AuthCommandCreateUser // 创建用户
	AuthCommandUpdateUser // 更新用户
	AuthCommandDeleteUser // 删除用户
	AuthCommandGetUser    // 获取用户信息
	AuthCommandListUsers  // 列出用户
	AuthCommandChangePass // 修改密码
	AuthCommandResetPass  // 重置密码（管理员）

	// 权限管理 (20-29)
	AuthCommandGrantPerm  // 授予权限
	AuthCommandRevokePerm // 撤销权限
	AuthCommandListPerm   // 列出权限
	AuthCommandCheckPerm  // 检查权限
)

var authCommandNames = map[AuthCommand]string{
	AuthCommandNull:       "NULL",
	AuthCommandLogin:      "LOGIN",
	AuthCommandLogout:     "LOGOUT",
	AuthCommandValidate:   "VALIDATE",
	AuthCommandRefresh:    "REFRESH",
	AuthCommandCreateUser: "CREATE_USER",
	AuthCommandUpdateUser: "UPDATE_USER",
	AuthCommandDeleteUser: "DELETE_USER",
	AuthCommandGetUser:    "GET_USER",
	AuthCommandListUsers:  "LIST_USERS",
	AuthCommandChangePass: "CHANGE_PASSWORD",
	AuthCommandResetPass:  "RESET_PASSWORD",
	AuthCommandGrantPerm:  "GRANT_PERMISSION",
	AuthCommandRevokePerm: "REVOKE_PERMISSION",
	AuthCommandListPerm:   "LIST_PERMISSIONS",
	AuthCommandCheckPerm:  "CHECK_PERMISSION",
}

func (c AuthCommand) String() string {
	if name, ok := authCommandNames[c]; ok {
		return name
	}
	return fmt.Sprintf("AUTH_UNKNOWN_%d", uint8(c))
}

// CreateClientCommandHead 创建客户端命令的协议头
func CreateClientCommandHead(cmd Command) *HEAD {
	head := NewHead()
	head.SetCommand(uint8(cmd))                                 // 设置客户端命令到high字节
	head.SetConfig(REQ, HaveResponse, uint8(CommandTypeClient)) // 设置命令类型到low字节
	return head
}

// CreateClusterCommandHead 创建集群命令的协议头
func CreateClusterCommandHead(cmd ClusterCommand) *HEAD {
	head := NewHead()
	head.SetCommand(uint8(cmd))                                  // 集群命令也使用high字节
	head.SetConfig(REQ, HaveResponse, uint8(CommandTypeCluster)) // 设置命令类型到low字节
	return head
}

// CreateGeoCommandHead 创建GEO命令的协议头
func CreateGeoCommandHead(cmd GeoCommand) *HEAD {
	head := NewHead()
	head.SetCommand(uint8(cmd))                              // GEO命令使用high字节
	head.SetConfig(REQ, HaveResponse, uint8(CommandTypeGeo)) // 设置命令类型到low字节
	return head
}

// CreateAuthCommandHead 创建认证命令的协议头
func CreateAuthCommandHead(cmd AuthCommand) *HEAD {
	head := NewHead()
	head.SetCommand(uint8(cmd))                               // 认证命令使用high字节
	head.SetConfig(REQ, HaveResponse, uint8(CommandTypeAuth)) // 设置命令类型到low字节
	return head
}

// ParseCommandFromHead 从协议头解析命令信息
func ParseCommandFromHead(head *HEAD) (CommandType, uint8) {
	commandType := CommandType(head.GetCommandType())
	command := head.GetCommand()
	return commandType, command
}

// IsClientCommand 检查是否为客户端命令
func IsClientCommand(head *HEAD) bool {
	return CommandType(head.GetCommandType()) == CommandTypeClient
}

// IsClusterCommand 检查是否为集群命令
func IsClusterCommand(head *HEAD) bool {
	return CommandType(head.GetCommandType()) == CommandTypeCluster
}

// IsGeoCommand 检查是否为GEO命令
func IsGeoCommand(head *HEAD) bool {
	return CommandType(head.GetCommandType()) == CommandTypeGeo
}

// IsAuthCommand 检查是否为认证命令
func IsAuthCommand(head *HEAD) bool {
	return CommandType(head.GetCommandType()) == CommandTypeAuth
}
