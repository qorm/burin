package business

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/bytedance/sonic"

	"github.com/qorm/burin/auth"
	"github.com/qorm/burin/cProtocol"
	"github.com/qorm/burin/cid"
	"github.com/qorm/burin/consensus"
	"github.com/qorm/burin/store"
	"github.com/qorm/burin/transaction"

	"github.com/sirupsen/logrus"
)

// Engine 业务引擎
type Engine struct {
	// 核心组件
	cacheEngine    *store.ShardedCacheEngine
	consensusNode  *consensus.Node
	transactionMgr *transaction.TransactionManager

	// 认证管理
	authManager *auth.AuthManager
	authHandler *auth.AuthHandler
	authConfig  *auth.AuthConfig // 认证配置

	// Burin客户端服务层
	burinServer *cProtocol.Server
	burinRouter *cProtocol.Router
	burinConfig *cProtocol.ServerConfig // Burin服务器配置引用

	// 集群命令服务层
	clusterServer *cProtocol.Server
	clusterRouter *cProtocol.Router
	clusterConfig *cProtocol.ServerConfig // 集群服务器配置引用

	// 数据复制
	replicator *DataReplicator

	// 配置和状态
	nodeID  string
	logger  *logrus.Logger
	mu      sync.RWMutex
	running bool

	// 生命周期管理
	ctx    context.Context
	cancel context.CancelFunc

	// 统计信息
	stats *EngineStats
}

// EngineStats 引擎统计信息
type EngineStats struct {
	mu           sync.RWMutex
	StartTime    time.Time `json:"start_time"`
	RequestCount int64     `json:"request_count"`
	ErrorCount   int64     `json:"error_count"`
	CacheHits    int64     `json:"cache_hits"`
	CacheMisses  int64     `json:"cache_misses"`
	Transactions int64     `json:"transactions"`
	LastActivity time.Time `json:"last_activity"`
}

// EngineConfig 引擎配置
type EngineConfig struct {
	NodeID            string
	CacheConfig       *store.Config
	ConsensusConfig   *consensus.NodeConfig
	TransactionConfig *transaction.TransactionConfig
	BurinConfig       *cProtocol.ServerConfig // 改名为BurinConfig
	AuthConfig        *auth.AuthConfig        // 认证配置
}

// NewEngine 创建新的业务引擎
func NewEngine(config *EngineConfig, logger *logrus.Logger) (*Engine, error) {
	nodeID := config.NodeID
	if nodeID == "" {
		nodeID = cid.Generate()
	}

	// 创建统计信息
	stats := &EngineStats{
		StartTime: time.Now(),
	}

	// 创建context用于生命周期管理
	ctx, cancel := context.WithCancel(context.Background())

	engine := &Engine{
		nodeID:     nodeID,
		logger:     logger,
		stats:      stats,
		replicator: NewDataReplicator(nodeID, logger),
		ctx:        ctx,
		cancel:     cancel,
	}

	// 初始化各个组件
	if err := engine.initializeComponents(config); err != nil {
		return nil, fmt.Errorf("failed to initialize components: %w", err)
	}

	// 设置协议路由
	engine.setupBurinRoutes()
	engine.setupClusterRoutes()
	engine.setupGeoRoutes()
	engine.setupAuthRoutes()

	logger.Infof("Business engine created with node ID: %s", nodeID)
	return engine, nil
}

// initializeComponents 初始化所有组件
func (e *Engine) initializeComponents(config *EngineConfig) error {
	var err error

	// 1. 初始化缓存引擎
	if config.CacheConfig != nil {
		e.cacheEngine, err = store.NewShardedCacheEngine(config.CacheConfig)
		if err != nil {
			return fmt.Errorf("failed to create cache engine: %w", err)
		}
	}

	// 2. 初始化共识节点
	if config.ConsensusConfig != nil {
		// 直接设置协议地址，无需端口映射
		if config.BurinConfig != nil {
			config.ConsensusConfig.ProtocolAddr = config.BurinConfig.Address
			config.ConsensusConfig.ClientAddr = config.BurinConfig.Address // 设置客户端地址
			e.logger.Infof("Set protocol address: %s", config.ConsensusConfig.ProtocolAddr)
			e.logger.Infof("Set client address: %s", config.ConsensusConfig.ClientAddr)
		} else {
			e.logger.Warn("Burin server configuration not provided")
		}

		e.consensusNode, err = consensus.NewNode(config.ConsensusConfig, e.logger)
		if err != nil {
			return fmt.Errorf("failed to create consensus node: %w", err)
		}
	}

	// 初始化事务管理器
	if config.TransactionConfig != nil {
		// 传递 consensus 节点给事务管理器
		e.transactionMgr, err = transaction.NewTransactionManager(config.TransactionConfig, e.cacheEngine, e.consensusNode)
		if err != nil {
			return fmt.Errorf("failed to create transaction manager: %w", err)
		}
	}

	// 5. 初始化统一协议服务层
	if config.BurinConfig != nil {
		e.burinConfig = config.BurinConfig // 保存Burin服务器配置引用
		e.burinRouter = cProtocol.NewRouter(e.logger)
		e.burinServer = cProtocol.NewServer(config.BurinConfig, e.logger)

		// 设置统一路由器
		e.burinServer.SetRouter(e.burinRouter)
	}

	// 6. 初始化认证系统
	if e.cacheEngine != nil {
		// 创建系统数据库用于存储认证信息
		if err := e.cacheEngine.CreateDatabase(auth.SystemDatabase); err != nil {
			// 如果数据库已存在，忽略错误
			e.logger.Debugf("System database might already exist: %v", err)
		}

		// 创建存储适配器，传入 consensus node 以支持系统数据库的 Raft 同步
		storageBackend := auth.NewStoreAdapter(e.cacheEngine, e.consensusNode, e.logger)

		// 使用配置中的认证设置，如果没有则使用默认值
		authConfig := config.AuthConfig
		if authConfig == nil {
			authConfig = &auth.AuthConfig{
				Enabled:        true,
				SuperAdminUser: "burin",
				SuperAdminPass: "burin@secret",
				RequireAuth:    false,
			}
		}

		// 保存认证配置到 Engine
		e.authConfig = authConfig

		e.authManager = auth.NewAuthManager(storageBackend, e.logger, authConfig)

		// 初始化认证系统（创建默认超级管理员）
		if err := e.authManager.Initialize(e.ctx); err != nil {
			e.logger.Errorf("Failed to initialize auth system: %v", err)
			// 不返回错误，允许系统继续运行
		} // 创建认证处理器
		e.authHandler = auth.NewAuthHandler(e.authManager, e.logger)

		e.logger.Info("Auth system initialized successfully")
	}

	return nil
} // setupBurinRoutes 设置Burin客户端服务路由
func (e *Engine) setupBurinRoutes() {
	if e.burinRouter == nil {
		return
	}

	// 注册客户端命令处理器（仅客户端相关命令）
	clientCommands := []cProtocol.Command{
		cProtocol.CmdPing,
		cProtocol.CmdGet,
		cProtocol.CmdSet,
		cProtocol.CmdDelete,
		cProtocol.CmdExists,
		cProtocol.CmdTransaction,
		cProtocol.CmdHealth,
		cProtocol.CmdCount,
		cProtocol.CmdList,
		cProtocol.CmdCreateDB,
		cProtocol.CmdUseDB,
		cProtocol.CmdListDBs,
		cProtocol.CmdDBExists,
		cProtocol.CmdDBInfo,
		cProtocol.CmdDeleteDB,
	}

	for _, cmd := range clientCommands {
		e.logger.Infof("Registering client command: %s (%d)", cmd.String(), cmd)
		e.burinRouter.Register(cProtocol.CommandTypeClient, cmd, cProtocol.HandlerFunc(e.handleClientCommands))
	}

	e.logger.Infof("All client commands registered to unified router")

}

// setupClusterRoutes 设置集群命令路由（使用统一服务器）
func (e *Engine) setupClusterRoutes() {
	if e.consensusNode == nil {
		e.logger.Warn("Consensus node not available, skipping cluster route setup")
		return
	}

	// 测试每个命令的注册
	commands := []cProtocol.ClusterCommand{
		cProtocol.ClusterCmdNull,
		cProtocol.ClusterCmdNodeJoin,
		cProtocol.ClusterCmdNodeLeave,
		cProtocol.ClusterCmdNodeStatus,
		cProtocol.ClusterCmdDataSync,
		cProtocol.ClusterCmdNodeDiscovery,
		cProtocol.ClusterCmdNodeHealthPing,
		cProtocol.ClusterCmdClusterStatus,
		cProtocol.ClusterCmdForward, // 请求转发命令
	}

	for _, cmd := range commands {
		e.logger.Infof("Registering cluster command: %s (%d)", cmd.String(), cmd)
		e.burinRouter.Register(cProtocol.CommandTypeCluster, cmd, cProtocol.HandlerFunc(e.handleClusterCommands))
	}

	e.logger.Infof("All cluster commands registered to unified router")

} // setupGeoRoutes 设置地理位置命令路由
func (e *Engine) setupGeoRoutes() {
	// 注册各种 GEO 命令到统一的 GEO 处理器

	commands := []cProtocol.GeoCommand{
		cProtocol.GeoCommandAdd,
		cProtocol.GeoCommandDist,
		cProtocol.GeoCommandRadius,
		cProtocol.GeoCommandHash,
		cProtocol.GeoCommandPos,
		cProtocol.GeoCommandGet,
		cProtocol.GeoCommandDelete,
		cProtocol.GeoCommandSearch,
		cProtocol.GeoCommandCluster,
	}

	for _, cmd := range commands {
		e.logger.Infof("Registering geo command: %v (%d)", cmd, cmd)
		e.burinRouter.Register(cProtocol.CommandTypeGeo, cmd, cProtocol.HandlerFunc(e.handleGeoCommands))
	}

	e.logger.Infof("All geo commands registered to unified router")

}

// setupAuthRoutes 设置认证命令路由
func (e *Engine) setupAuthRoutes() {
	if e.burinRouter == nil || e.authHandler == nil {
		e.logger.Warn("Router or auth handler not available, skipping auth route setup")
		return
	}

	// 注册认证命令
	commands := []cProtocol.AuthCommand{
		cProtocol.AuthCommandLogin,
		cProtocol.AuthCommandLogout,
		cProtocol.AuthCommandValidate,
		cProtocol.AuthCommandRefresh,
		cProtocol.AuthCommandCreateUser,
		cProtocol.AuthCommandUpdateUser,
		cProtocol.AuthCommandDeleteUser,
		cProtocol.AuthCommandGetUser,
		cProtocol.AuthCommandListUsers,
		cProtocol.AuthCommandChangePass,
		cProtocol.AuthCommandResetPass,
		cProtocol.AuthCommandGrantPerm,
		cProtocol.AuthCommandRevokePerm,
		cProtocol.AuthCommandListPerm,
		cProtocol.AuthCommandCheckPerm,
	}

	for _, cmd := range commands {
		e.logger.Infof("Registering auth command: %s (%d)", cmd.String(), cmd)
		e.burinRouter.Register(cProtocol.CommandTypeAuth, cmd, cProtocol.HandlerFunc(e.handleAuthCommands))
	}

	e.logger.Infof("All auth commands registered to unified router")
}

// Start 启动引擎
func (e *Engine) Start() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.running {
		return fmt.Errorf("engine already running")
	}

	// 启动统一协议服务器（处理所有类型的命令）
	if e.burinServer != nil {
		if err := e.burinServer.Start(); err != nil {
			return fmt.Errorf("failed to start unified protocol server: %w", err)
		}
		e.logger.Info("Unified protocol server started (supports client, cluster, and queue commands)")
	}

	// 启动consensus节点
	if e.consensusNode != nil {
		if err := e.consensusNode.Start(); err != nil {
			e.logger.Errorf("Failed to start consensus node: %v", err)
		} else {
			e.logger.Info("Consensus node started")

			// 设置FSM回调，当Raft apply时同步数据到本地缓存引擎
			e.consensusNode.SetFSMCallback(func(database, key string, value []byte, ttl time.Duration) {
				err := e.cacheEngine.SetWithDatabase(database, key, value, ttl)
				if err != nil {
					e.logger.Errorf("Failed to sync data from FSM to cache engine: db=%s, key=%s, err=%v", database, key, err)
				} else {
					e.logger.Debugf("Synced data from FSM to cache engine: db=%s, key=%s, valueLen=%d", database, key, len(value))
				}
			})

			// 设置DELETE FSM回调，当Raft apply DELETE时同步删除本地缓存引擎数据
			e.consensusNode.SetFSMDeleteCallback(func(database, key string) {
				err := e.cacheEngine.DeleteWithDatabase(database, key)
				if err != nil {
					e.logger.Errorf("Failed to delete data from FSM to cache engine: db=%s, key=%s, err=%v", database, key, err)
				} else {
					e.logger.Debugf("Deleted data from FSM to cache engine: db=%s, key=%s", database, key)
				}
			})

			// 设置CREATE DATABASE FSM回调，当Raft apply CREATE_DATABASE时同步创建数据库到本地存储引擎
			e.consensusNode.SetFSMCreateDatabaseCallback(func(database, dataType string) error {
				err := e.cacheEngine.CreateDatabase(database)
				if err != nil {
					e.logger.Errorf("Failed to create database from FSM to cache engine: db=%s, err=%v", database, err)
					return err
				} else {
					e.logger.Infof("Created database from FSM to cache engine: db=%s", database)
				}
				return nil
			})

			// 设置DELETE DATABASE FSM回调，当Raft apply DELETE_DATABASE时同步删除数据库到本地存储引擎
			e.consensusNode.SetFSMDeleteDatabaseCallback(func(database string) error {
				err := e.cacheEngine.DeleteDatabase(database)
				if err != nil {
					e.logger.Errorf("Failed to delete database from FSM to cache engine: db=%s, err=%v", database, err)
					return err
				} else {
					e.logger.Infof("Deleted database from FSM to cache engine: db=%s", database)
				}
				return nil
			})

			// 设置GEOADD FSM回调，当Raft apply GEOADD时同步GEO数据到本地存储引擎
			e.consensusNode.SetFSMGeoAddCallback(func(database, key string, members interface{}) error {
				// 将interface{}转换为[]cProtocol.GeoMember
				membersBytes, err := sonic.Marshal(members)
				if err != nil {
					e.logger.Errorf("Failed to marshal members: %v", err)
					return err
				}

				var geoMembers []cProtocol.GeoMember
				if err := sonic.Unmarshal(membersBytes, &geoMembers); err != nil {
					e.logger.Errorf("Failed to unmarshal members: %v", err)
					return err
				}

				err = e.cacheEngine.SetGeo(database, key, geoMembers)
				if err != nil {
					e.logger.Errorf("Failed to add geo data from FSM to cache engine: db=%s, key=%s, err=%v", database, key, err)
					return err
				} else {
					e.logger.Infof("Added geo data from FSM to cache engine: db=%s, key=%s, members=%d", database, key, len(geoMembers))
				}
				return nil
			})
		}
	}

	// 启动后立即设置集群信息
	e.setupClusterNodesFromConsensus()

	// 启动定期更新集群信息的goroutine
	go e.startClusterMonitor()

	e.running = true
	e.logger.Info("Business engine started successfully")

	return nil
}

// Stop 停止引擎
func (e *Engine) Stop() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.running {
		return fmt.Errorf("engine not running")
	}

	e.logger.Info("Stopping business engine...")

	// 取消context，停止所有goroutines
	e.cancel()

	// 停止统一协议服务器
	if e.burinServer != nil {
		if err := e.burinServer.Stop(); err != nil {
			e.logger.Errorf("Failed to stop unified protocol server: %v", err)
		}
	}

	// 停止consensus节点
	if e.consensusNode != nil {
		if err := e.consensusNode.Stop(); err != nil {
			e.logger.Errorf("Failed to stop consensus node: %v", err)
		}
	}

	e.running = false
	e.logger.Info("Business engine stopped")

	return nil
}

// IsRunning 检查引擎是否运行
func (e *Engine) IsRunning() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.running
}

// SetupCluster 配置集群节点信息
func (e *Engine) SetupCluster(nodes []NodeInfo) {
	if e.replicator == nil {
		e.logger.Warn("Replicator not initialized, skipping cluster setup")
		return
	}

	for _, node := range nodes {
		e.replicator.AddNode(node)
	}

	e.logger.Infof("Configured cluster with %d nodes", len(nodes))
}

// UpdateLeaderStatus 更新领导者状态
func (e *Engine) UpdateLeaderStatus(isLeader bool) {
	if e.replicator != nil {
		e.replicator.SetLeaderStatus(isLeader)
	}

	if e.consensusNode != nil {
		// 这里可以添加从consensus模块获取leader状态的逻辑
		// 暂时手动设置
	}
}

// GetClusterNodes 获取集群节点信息
func (e *Engine) GetClusterNodes() []NodeInfo {
	if e.replicator == nil {
		return []NodeInfo{}
	}
	return e.replicator.GetNodes()
}

// setupClusterNodesFromConsensus 从共识模块获取集群节点并配置到数据复制器
func (e *Engine) setupClusterNodesFromConsensus() {
	if e.consensusNode == nil || e.replicator == nil {
		e.logger.Warn("Consensus node or replicator not available, skipping cluster setup")
		return
	}

	// 从Raft获取集群节点信息
	peers := e.consensusNode.GetPeers()
	if len(peers) == 0 {
		e.logger.Warn("No peers found in consensus module")
		return
	}

	// 清除现有节点并重新添加
	e.replicator.ClearNodes()

	// 转换为NodeInfo并添加到复制器
	nodes := make([]NodeInfo, 0, len(peers))
	for id, peer := range peers {
		// 判断是否为领导者
		isLeader := e.consensusNode.IsLeader() && id == e.nodeID

		// 使用客户端地址进行数据复制（如果有的话），否则使用Raft地址
		replicationAddress := peer.ClientAddr
		if replicationAddress == "" {
			e.logger.Warnf("Node %s missing client address, using raft address: %s", id, peer.Address)
			replicationAddress = peer.Address
		}

		node := NodeInfo{
			ID:       id,
			Address:  replicationAddress,
			IsLeader: isLeader,
		}
		nodes = append(nodes, node)
		e.replicator.AddNode(node)
	}

	// 设置当前节点的领导者状态
	isCurrentLeader := e.consensusNode.IsLeader()
	e.replicator.SetLeaderStatus(isCurrentLeader)

	e.logger.Infof("Configured cluster with %d nodes from consensus module", len(nodes))

	// 打印节点信息用于调试
	for _, node := range nodes {
		e.logger.Infof("Node: ID=%s, Address=%s, IsLeader=%v",
			node.ID, node.Address, node.IsLeader)
	}

	if isCurrentLeader {
		e.logger.Info("This node is the leader, will replicate data to followers")
	} else {
		e.logger.Info("This node is a follower, will receive replicated data")
	}
}

// startClusterMonitor 启动集群监控，定期更新节点信息
func (e *Engine) startClusterMonitor() {
	ticker := time.NewTicker(10 * time.Second) // 每10秒检查一次
	defer ticker.Stop()

	for {
		select {
		case <-e.ctx.Done():
			// 引擎停止，退出监控循环
			e.logger.Info("Cluster monitor stopped")
			return
		case <-ticker.C:
			// 检查集群状态变化
			if e.consensusNode != nil && e.replicator != nil {
				currentPeers := e.consensusNode.GetPeers()
				currentNodes := e.replicator.GetNodes()

				// 如果节点数量或leader状态发生变化，重新配置
				if len(currentPeers) != len(currentNodes) || e.hasLeaderStatusChanged() {
					e.logger.Info("Detected cluster changes, updating node configuration")
					e.setupClusterNodesFromConsensus()
				}
			}
		}
	}
}

// hasLeaderStatusChanged 检查leader状态是否发生变化
func (e *Engine) hasLeaderStatusChanged() bool {
	if e.consensusNode == nil || e.replicator == nil {
		return false
	}

	currentIsLeader := e.consensusNode.IsLeader()
	nodes := e.replicator.GetNodes()

	// 检查当前节点的leader状态是否与复制器中记录的一致
	for _, node := range nodes {
		if node.ID == e.nodeID {
			return node.IsLeader != currentIsLeader
		}
	}

	return true // 如果没找到当前节点，说明需要更新
}

func (e *Engine) GetNodeID() string {
	return e.nodeID
}

// GetStats 获取引擎统计信息
func (e *Engine) GetStats() *EngineStats {
	e.stats.mu.RLock()
	defer e.stats.mu.RUnlock()

	// 返回副本，避免复制锁
	stats := &EngineStats{
		StartTime:    e.stats.StartTime,
		RequestCount: e.stats.RequestCount,
		ErrorCount:   e.stats.ErrorCount,
		CacheHits:    e.stats.CacheHits,
		CacheMisses:  e.stats.CacheMisses,
		Transactions: e.stats.Transactions,
		LastActivity: e.stats.LastActivity,
	}
	return stats
}

// updateStats 更新统计信息
func (e *Engine) updateStats() {
	e.stats.mu.Lock()
	defer e.stats.mu.Unlock()

	e.stats.RequestCount++
	e.stats.LastActivity = time.Now()
}

// checkAuth 检查认证和权限
// 返回: (username, error)
func (e *Engine) checkAuth(ctx context.Context, req *cProtocol.ProtocolRequest, database string, requiredPerm auth.Permission) (string, error) {
	// 如果认证未启用，直接放行
	if e.authManager == nil {
		return "", nil
	}

	// 从请求的 Meta 中获取 token
	username, ok := req.Meta["username"]
	if !ok || username == "" {
		// 如果没有 username，检查是否强制要求认证
		if e.authConfig != nil && !e.authConfig.RequireAuth {
			// 开发模式：不强制要求认证，允许匿名访问
			e.logger.Debug("No username provided, but RequireAuth=false, allowing anonymous access")
			return "", nil
		}

		// 生产模式：必须提供 username
		e.logger.Warn("No authentication username provided and RequireAuth=true")
		return "", &AuthError{
			Code:    401,
			Message: "authentication required: no username provided",
		}
	}

	// 检查数据库权限
	hasPermission, err := e.authManager.CheckPermission(ctx, username, database, requiredPerm)
	if err != nil {
		e.logger.Errorf("Permission check failed: %v", err)
		return "", &AuthError{
			Code:    500,
			Message: fmt.Sprintf("permission check failed: %v", err),
		}
	}

	if !hasPermission {
		e.logger.Warnf("Permission denied: user=%s, database=%s, permission=%s", username, database, requiredPerm)
		return "", &AuthError{
			Code: 403,
			Message: fmt.Sprintf("permission denied: user '%s' does not have '%s' permission on database '%s'",
				username, requiredPerm, database),
		}
	}

	e.logger.Debugf("Authentication successful: user=%s, database=%s, permission=%s", username, database, requiredPerm)
	return username, nil
}

// 集群内部命令处理器
// ===============================================
// 以下方法处理集群内部命令 (使用 SetPriCommand/GetPriCommand)

// handleClusterCommands 处理所有集群命令的主入口
func (e *Engine) handleClusterCommands(ctx context.Context, req *cProtocol.ProtocolRequest) (*cProtocol.ProtocolResponse, error) {
	// 从Meta中获取命令类型信息，或者直接从Head获取
	commandTypeStr := req.Meta["command_type"]
	if commandTypeStr == "" {
		// 如果Meta中没有，从Head获取
		commandTypeStr = fmt.Sprintf("%d", req.Head.GetCommandType())
	}

	// 检查是否是集群命令类型
	if commandTypeStr != fmt.Sprintf("%d", cProtocol.CommandTypeCluster) {
		return &cProtocol.ProtocolResponse{
			Status: 400,
			Error:  "Invalid cluster command",
		}, nil
	}

	// 构造集群请求
	clusterReq := &cProtocol.ClusterRequest{
		Head: req.Head,
		Data: req.Data,
		Meta: req.Meta,
	}

	// 根据命令类型分发到具体处理器
	var clusterResp *cProtocol.ClusterResponse
	var err error

	// 从Head获取集群命令
	clusterCommand := cProtocol.ClusterCommand(req.Head.GetCommand())
	switch clusterCommand {
	case cProtocol.ClusterCmdNull:
		clusterResp, err = e.handleClusterNull(ctx, clusterReq)
	case cProtocol.ClusterCmdNodeJoin:
		clusterResp, err = e.handleClusterNodeJoin(ctx, clusterReq)
	case cProtocol.ClusterCmdNodeLeave:
		clusterResp, err = e.handleClusterNodeLeave(ctx, clusterReq)
	case cProtocol.ClusterCmdNodeStatus:
		clusterResp, err = e.handleClusterNodeStatus(ctx, clusterReq)
	case cProtocol.ClusterCmdRaftHeartbeat:
		clusterResp, err = e.handleClusterRaftHeartbeat(ctx, clusterReq)
	case cProtocol.ClusterCmdDataSync:
		clusterResp, err = e.handleClusterDataSync(ctx, clusterReq)
	case cProtocol.ClusterCmdRaftAppendEntries:
		clusterResp, err = e.handleClusterRaftAppendEntries(ctx, clusterReq)
	case cProtocol.ClusterCmdRaftRequestVote:
		clusterResp, err = e.handleClusterRaftRequestVote(ctx, clusterReq)
	case cProtocol.ClusterCmdRaftSnapshot:
		clusterResp, err = e.handleClusterRaftSnapshot(ctx, clusterReq)
	case cProtocol.ClusterCmdNodeDiscovery:
		clusterResp, err = e.handleClusterNodeDiscovery(ctx, clusterReq)
	case cProtocol.ClusterCmdNodeHealthPing:
		clusterResp, err = e.handleClusterNodeHealthPing(ctx, clusterReq)
	case cProtocol.ClusterCmdDataBackup:
		clusterResp, err = e.handleClusterDataBackup(ctx, clusterReq)
	case cProtocol.ClusterCmdDataRestore:
		clusterResp, err = e.handleClusterDataRestore(ctx, clusterReq)
	case cProtocol.ClusterCmdDataValidate:
		clusterResp, err = e.handleClusterDataValidate(ctx, clusterReq)
	case cProtocol.ClusterCmdClusterStatus:
		clusterResp, err = e.handleClusterClusterStatus(ctx, clusterReq)
	case cProtocol.ClusterCmdForward:
		// 处理从 follower 转发来的请求
		clusterResp, err = e.handleClusterForward(ctx, clusterReq)
	case cProtocol.ClusterCmdClusterConfig:
		clusterResp, err = e.handleClusterClusterConfig(ctx, clusterReq)
	case cProtocol.ClusterCmdEmergencyStop:
		clusterResp, err = e.handleClusterEmergencyStop(ctx, clusterReq)
	default:
		return &cProtocol.ProtocolResponse{
			Status: 400,
			Error:  fmt.Sprintf("Unknown cluster command: %d", clusterCommand),
		}, nil
	}

	if err != nil {
		return &cProtocol.ProtocolResponse{
			Status: 500,
			Error:  fmt.Sprintf("Cluster command failed: %v", err),
		}, nil
	}

	// 转换集群响应为协议响应
	// 使用集群响应中的 Head（如果有）
	protocolResp := &cProtocol.ProtocolResponse{
		Status: clusterResp.Status,
		Data:   clusterResp.Data,
		Error:  clusterResp.Error,
	}

	// 如果集群响应包含 Head，使用它；否则创建一个响应头
	if clusterResp.Head != nil {
		protocolResp.Head = clusterResp.Head
	} else {
		// 为集群命令创建响应头
		respHead := cProtocol.CreateClusterCommandHead(clusterCommand)
		respHead.SetConfig(cProtocol.REP, cProtocol.NoResponse, uint8(cProtocol.CommandTypeCluster))
		protocolResp.Head = respHead
	}

	return protocolResp, nil
}

// ============================================
// 客户端命令处理器
// ============================================

// handleClientCommands 处理所有客户端命令的主入口
func (e *Engine) handleClientCommands(ctx context.Context, req *cProtocol.ProtocolRequest) (*cProtocol.ProtocolResponse, error) {
	// 记录统计信息
	e.updateStats()

	// 从Header获取客户端命令
	commandType, command := cProtocol.ParseCommandFromHead(req.Head)
	if commandType != cProtocol.CommandTypeClient {
		return &cProtocol.ProtocolResponse{
			Status: 400,
			Error:  "Invalid command type for client service",
		}, nil
	}

	clientCmd := cProtocol.Command(command)
	e.logger.Debugf("Processing client command: %s (%d)", clientCmd.String(), clientCmd)

	// 根据命令类型分发到具体处理器
	switch clientCmd {
	case cProtocol.CmdPing:
		return e.handlePing(ctx, req)
	case cProtocol.CmdGet:
		return e.handleGet(ctx, req)
	case cProtocol.CmdSet:
		return e.handleSet(ctx, req)
	case cProtocol.CmdDelete:
		return e.handleDelete(ctx, req)
	case cProtocol.CmdExists:
		return e.handleExists(ctx, req)
	case cProtocol.CmdTransaction:
		return e.handleTransaction(ctx, req)
	case cProtocol.CmdHealth:
		return e.handleHealth(ctx, req)
	case cProtocol.CmdCount:
		return e.handleCount(ctx, req)
	case cProtocol.CmdList:
		return e.handleList(ctx, req)
	case cProtocol.CmdCreateDB:
		return e.handleCreateCacheDB(ctx, req)
	case cProtocol.CmdUseDB:
		return e.handleUseDB(ctx, req)
	case cProtocol.CmdListDBs:
		return e.handleListDBs(ctx, req)
	case cProtocol.CmdDBExists:
		return e.handleDBExists(ctx, req)
	case cProtocol.CmdDBInfo:
		return e.handleDBInfo(ctx, req)
	case cProtocol.CmdDeleteDB:
		return e.handleDeleteDB(ctx, req)
	default:
		return &cProtocol.ProtocolResponse{
			Status: 400,
			Error:  fmt.Sprintf("Unknown client command: %v", clientCmd),
		}, nil
	}
}

// ============================================
// 队列命令处理器
// ============================================

// ============================================
// GEO 命令处理器
// ============================================

// handleGeoCommands 处理 GEO 相关命令
func (e *Engine) handleGeoCommands(ctx context.Context, req *cProtocol.ProtocolRequest) (*cProtocol.ProtocolResponse, error) {
	// 记录统计信息
	e.updateStats()

	// 从Header获取 GEO 命令
	commandType, command := cProtocol.ParseCommandFromHead(req.Head)
	if commandType != cProtocol.CommandTypeGeo {
		return &cProtocol.ProtocolResponse{
			Status: 400,
			Error:  "Invalid command type for GEO service",
		}, nil
	}

	geoCmd := cProtocol.GeoCommand(command)
	e.logger.Debugf("Processing GEO command: %v (%d)", geoCmd, geoCmd)

	// 解析 GEO 请求
	geoReq, err := parseGeoRequest(req.Data)
	if err != nil {
		return &cProtocol.ProtocolResponse{
			Status: 400,
			Error:  fmt.Sprintf("Failed to parse GEO request: %v", err),
		}, nil
	}

	// GEO 命令强制使用 __burin_geo__ 数据库
	geoReq.Database = "__burin_geo__"

	// 对于写操作（GEOADD, GEODEL），如果不是 leader 且未被转发，则转发到 leader
	isWriteOperation := geoCmd == cProtocol.GeoCommandAdd || geoCmd == cProtocol.GeoCommandDelete
	if isWriteOperation && !e.isForwardedRequest(req.Data) {
		if e.consensusNode != nil && !e.consensusNode.IsLeader() {
			e.logger.Infof("Forwarding GEO write operation %v to leader", geoCmd)
			return e.forwardToLeader(ctx, req)
		}
	}

	var geoResp *cProtocol.GeoResponse

	// 根据命令类型分发到具体处理器
	switch geoCmd {
	case cProtocol.GeoCommandAdd:
		geoResp = e.handleGeoAdd(ctx, geoReq)
	case cProtocol.GeoCommandDist:
		geoResp = e.handleGeoDist(ctx, geoReq)
	case cProtocol.GeoCommandRadius:
		geoResp = e.handleGeoRadius(ctx, geoReq)
	case cProtocol.GeoCommandHash:
		geoResp = e.handleGeoHash(ctx, geoReq)
	case cProtocol.GeoCommandPos:
		geoResp = e.handleGeoPos(ctx, geoReq)
	case cProtocol.GeoCommandGet:
		geoResp = e.handleGeoGet(ctx, geoReq)
	case cProtocol.GeoCommandDelete:
		geoResp = e.handleGeoDel(ctx, geoReq)
	case cProtocol.GeoCommandSearch:
		geoResp = e.handleGeoRadius(ctx, geoReq) // 使用 radius 处理搜索
	case cProtocol.GeoCommandCluster:
		// 集群 GEO 命令处理
		return e.handleGeoCluster(ctx, geoReq)
	default:
		return &cProtocol.ProtocolResponse{
			Status: 400,
			Error:  fmt.Sprintf("Unknown GEO command: %v", geoCmd),
		}, nil
	}

	// 将 GEO 响应转换为协议响应
	responseData, err := sonic.Marshal(geoResp)
	if err != nil {
		e.logger.Errorf("Failed to marshal GEO response: %v", err)
		return &cProtocol.ProtocolResponse{
			Status: 500,
			Error:  fmt.Sprintf("Failed to marshal response: %v", err),
		}, nil
	}

	return &cProtocol.ProtocolResponse{
		Status: geoResp.Status,
		Data:   responseData,
	}, nil
}

// handleGeoCluster 处理集群 GEO 命令
func (e *Engine) handleGeoCluster(ctx context.Context, req *cProtocol.GeoRequest) (*cProtocol.ProtocolResponse, error) {
	if e.consensusNode == nil {
		return &cProtocol.ProtocolResponse{
			Status: 503,
			Error:  "Cluster not available",
		}, nil
	}

	e.logger.Debugf("Handling cluster GEO command: operation=%s, key=%s, database=%s",
		req.Operation, req.Key, req.Database)

	// 通过共识层应用 GEO 命令（确保一致性）
	// 这里可以根据需要添加集群协调逻辑
	// 例如：通过 Raft 日志复制到其他节点

	// 对于读取操作（如 geodist, georadius, geohash, geopos, geoget），
	// 可以直接从本地缓存读取而不需要通过 Raft

	// 对于写入操作（如 geoadd, geodel），
	// 应该通过 Raft 日志进行复制

	switch req.Operation {
	case "geoadd", "geodel":
		// 这些是写入操作，需要通过共识层
		e.logger.Infof("GEO write operation via cluster: %s on key %s", req.Operation, req.Key)
		// 可以在这里添加 Raft 日志提交逻辑
	default:
		// 读取操作，直接处理
		e.logger.Infof("GEO read operation: %s on key %s", req.Operation, req.Key)
	}

	// 根据操作类型转发到相应的处理器
	var geoResp *cProtocol.GeoResponse
	switch req.Operation {
	case "geoadd":
		geoResp = e.handleGeoAdd(ctx, req)
	case "geodist":
		geoResp = e.handleGeoDist(ctx, req)
	case "georadius", "geosearch":
		geoResp = e.handleGeoRadius(ctx, req)
	case "geohash":
		geoResp = e.handleGeoHash(ctx, req)
	case "geopos":
		geoResp = e.handleGeoPos(ctx, req)
	case "geoget":
		geoResp = e.handleGeoGet(ctx, req)
	case "geodel":
		geoResp = e.handleGeoDel(ctx, req)
	default:
		geoResp = &cProtocol.GeoResponse{
			Success: false,
			Status:  400,
			Error:   fmt.Sprintf("Unknown cluster GEO operation: %s", req.Operation),
		}
	}

	// 将 GEO 响应转换为协议响应
	responseData, err := sonic.Marshal(geoResp)
	if err != nil {
		e.logger.Errorf("Failed to marshal GEO response: %v", err)
		return &cProtocol.ProtocolResponse{
			Status: 500,
			Error:  fmt.Sprintf("Failed to marshal response: %v", err),
		}, nil
	}

	return &cProtocol.ProtocolResponse{
		Status: geoResp.Status,
		Data:   responseData,
	}, nil
}

// ============================================
// Leader 转发功能
// ============================================

// forwardToLeader 将请求转发到 Leader 节点
func (e *Engine) forwardToLeader(ctx context.Context, req *cProtocol.ProtocolRequest) (*cProtocol.ProtocolResponse, error) {
	// 获取 leader 信息
	if e.consensusNode == nil {
		return &cProtocol.ProtocolResponse{
			Status: 503,
			Error:  "consensus node not available",
		}, nil
	}

	leaderID := e.consensusNode.GetLeader()
	if leaderID == "" {
		return &cProtocol.ProtocolResponse{
			Status: 503,
			Error:  "no leader available in cluster",
		}, nil
	}

	// 如果当前节点就是 leader，不应该转发
	if leaderID == e.nodeID {
		return &cProtocol.ProtocolResponse{
			Status: 500,
			Error:  "internal error: attempt to forward to self",
		}, nil
	}

	// 获取 leader 的客户端地址
	leaderClientAddr := e.getLeaderClientAddr()
	if leaderClientAddr == "" {
		return &cProtocol.ProtocolResponse{
			Status: 503,
			Error:  fmt.Sprintf("leader client address not available for node %s", leaderID),
		}, nil
	}

	e.logger.Infof("Forwarding request to leader %s at %s", leaderID, leaderClientAddr)

	// 创建到 leader 的客户端连接（用于集群内部通信，不需要认证）
	clientConfig := &cProtocol.ClientConfig{
		Endpoint:            leaderClientAddr,
		MaxConnsPerEndpoint: 1,
		DialTimeout:         5 * time.Second,
		ReadTimeout:         30 * time.Second,
		WriteTimeout:        30 * time.Second,
	}

	client := cProtocol.NewClient(clientConfig, e.logger)

	if err := client.Start(); err != nil {
		return &cProtocol.ProtocolResponse{
			Status: 503,
			Error:  fmt.Sprintf("failed to connect to leader: %v", err),
		}, nil
	}
	defer client.Stop()

	// 构造集群转发请求
	// 将原始请求数据包装在集群转发命令中
	// 重要：权限已在 follower 节点验证，这里传递权限验证结果
	forwardData := map[string]interface{}{
		"original_data": req.Data,            // 原始请求数据（已序列化的字节）
		"original_head": req.Head.GetBytes(), // 原始请求头
		"from_node":     e.nodeID,
		"meta":          req.Meta, // 传递元数据（包含用户名、权限等信息）
	}

	// 如果有连接信息，提取用户名和权限信息
	if req.Connection != nil {
		if username, ok := req.Meta["username"]; ok && username != "" {
			forwardData["username"] = username
			e.logger.Debugf("Forwarding request from user: %s", username)
		}
		// 标记权限已在 follower 端验证
		// Leader 需要信任 follower 的验证结果
		forwardData["permission_verified"] = true
	}

	forwardedData, err := sonic.Marshal(forwardData)
	if err != nil {
		return &cProtocol.ProtocolResponse{
			Status: 500,
			Error:  fmt.Sprintf("failed to marshal forward request: %v", err),
		}, nil
	}

	// 创建集群转发请求
	forwardHead := cProtocol.CreateClusterCommandHead(cProtocol.ClusterCmdForward)
	forwardReq := &cProtocol.ProtocolRequest{
		Head: forwardHead,
		Data: forwardedData,
		Meta: req.Meta, // 保留原始元数据
	}
	forwardReq.Head.SetContentLength(uint32(len(forwardedData)))

	// 发送请求到 leader
	forwardCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	resp, err := client.SendRequest(forwardCtx, forwardReq)
	if err != nil {
		return &cProtocol.ProtocolResponse{
			Status: 502,
			Error:  fmt.Sprintf("failed to forward request to leader: %v", err),
		}, nil
	}

	e.logger.Infof("Successfully forwarded request to leader, status: %d", resp.Status)
	return resp, nil
} // authenticateConnection 在指定连接上执行认证
func (e *Engine) authenticateConnection(conn net.Conn, username, password string) error {
	// 构造登录请求
	head := cProtocol.CreateAuthCommandHead(cProtocol.AuthCommandLogin)

	loginReq := map[string]string{
		"username": username,
		"password": password,
	}

	reqData, err := sonic.Marshal(loginReq)
	if err != nil {
		return fmt.Errorf("failed to marshal login data: %w", err)
	}

	head.SetContentLength(uint32(len(reqData)))

	// 发送协议头
	if _, err := conn.Write(head.GetBytes()); err != nil {
		return fmt.Errorf("failed to send auth head: %w", err)
	}

	// 发送请求数据
	if _, err := conn.Write(reqData); err != nil {
		return fmt.Errorf("failed to send auth data: %w", err)
	}

	// 读取响应头
	headBytes := make([]byte, 6) // HEAD 大小为 6 字节
	if _, err := conn.Read(headBytes); err != nil {
		return fmt.Errorf("failed to read auth response head: %w", err)
	}

	responseHead, err := cProtocol.HeadFromBytes(headBytes)
	if err != nil {
		return fmt.Errorf("failed to parse auth response head: %w", err)
	}

	// 读取响应体
	contentLength := responseHead.GetContentLength()
	if contentLength > 0 {
		responseData := make([]byte, contentLength)
		if _, err := conn.Read(responseData); err != nil {
			return fmt.Errorf("failed to read auth response data: %w", err)
		}

		// 解析响应数据查看认证是否成功
		var authResp map[string]interface{}
		if err := sonic.Unmarshal(responseData, &authResp); err != nil {
			e.logger.Warnf("Failed to parse auth response: %v", err)
		} else {
			if status, ok := authResp["status"].(string); ok && status != "success" {
				return fmt.Errorf("authentication failed: %s", status)
			}
		}
	}

	e.logger.Infof("Connection authenticated as user: %s", username)
	return nil
}

// getLeaderClientAddr 获取 Leader 的客户端服务地址
func (e *Engine) getLeaderClientAddr() string {
	if e.consensusNode == nil {
		return ""
	}

	leaderID := e.consensusNode.GetLeader()
	if leaderID == "" {
		e.logger.Warn("No leader elected yet")
		return ""
	}

	e.logger.Debugf("Getting client address for leader: %s", leaderID)

	// 如果是当前节点，返回配置的客户端地址
	if leaderID == e.nodeID {
		config := e.consensusNode.GetConfig()
		if config != nil {
			e.logger.Debugf("Current node is leader, client addr: %s", config.ClientAddr)
			return config.ClientAddr
		}
		e.logger.Warn("Current node is leader but config is nil")
		return ""
	}

	// 从 peers 中获取 leader 的客户端地址
	peers := e.consensusNode.GetPeers()
	e.logger.Debugf("Checking peers for leader %s, total peers: %d", leaderID, len(peers))

	if peer, exists := peers[leaderID]; exists {
		if peer.ClientAddr != "" {
			e.logger.Infof("Found leader client address: %s", peer.ClientAddr)
			return peer.ClientAddr
		}
		e.logger.Warnf("Leader %s found but ClientAddr is empty, will use Raft address as fallback", leaderID)
		// 尝试使用 Raft 地址作为后备（假设客户端和 Raft 端口可能在同一主机）
		// 这是一个临时解决方案，生产环境应该确保所有节点都正确配置 ClientAddr
		return peer.Address
	} else {
		e.logger.Warnf("Leader %s not found in peers map", leaderID)
	}

	e.logger.Warnf("Leader client address not found for node %s", leaderID)
	return ""
}

// handleClusterForward 处理从 follower 转发来的请求
// 这个方法在 leader 节点上执行，接收 follower 转发的客户端请求
func (e *Engine) handleClusterForward(ctx context.Context, req *cProtocol.ClusterRequest) (*cProtocol.ClusterResponse, error) {
	e.logger.Info("Handling forwarded request from follower")

	// 解析转发数据
	var forwardData struct {
		OriginalData       []byte            `json:"original_data"`
		OriginalHead       []byte            `json:"original_head"`
		FromNode           string            `json:"from_node"`
		Meta               map[string]string `json:"meta"`
		Username           string            `json:"username"`
		PermissionVerified bool              `json:"permission_verified"`
	}

	if err := sonic.Unmarshal(req.Data, &forwardData); err != nil {
		e.logger.Errorf("Failed to unmarshal forward data: %v", err)
		return &cProtocol.ClusterResponse{
			Status: 400,
			Error:  fmt.Sprintf("invalid forward data: %v", err),
		}, nil
	}

	e.logger.Infof("Processing forwarded request from node %s, user: %s, permission_verified: %v",
		forwardData.FromNode, forwardData.Username, forwardData.PermissionVerified)

	// 解析原始请求头
	originalHead, err := cProtocol.HeadFromBytes(forwardData.OriginalHead)
	if err != nil {
		e.logger.Errorf("Failed to parse original head: %v", err)
		return &cProtocol.ClusterResponse{
			Status: 400,
			Error:  fmt.Sprintf("invalid original head: %v", err),
		}, nil
	}

	// 重构原始请求
	originalReq := &cProtocol.ProtocolRequest{
		Head:       originalHead,
		Data:       forwardData.OriginalData,
		Meta:       forwardData.Meta,
		Connection: nil, // 转发请求没有直接的连接对象
	}

	// 在 Meta 中添加标记，表明这是已验证权限的转发请求
	if originalReq.Meta == nil {
		originalReq.Meta = make(map[string]string)
	}
	originalReq.Meta["_forwarded"] = "true"
	originalReq.Meta["_from_node"] = forwardData.FromNode
	originalReq.Meta["_permission_verified"] = fmt.Sprintf("%v", forwardData.PermissionVerified)

	// 根据原始命令类型分发到相应的处理器
	commandType, command := cProtocol.ParseCommandFromHead(originalHead)

	e.logger.Debugf("Forwarded request: type=%s, command=%d", commandType.String(), command)

	var resp *cProtocol.ProtocolResponse
	var handleErr error

	switch commandType {
	case cProtocol.CommandTypeClient:
		resp, handleErr = e.handleClientCommands(ctx, originalReq)
	case cProtocol.CommandTypeGeo:
		resp, handleErr = e.handleGeoCommands(ctx, originalReq)
	case cProtocol.CommandTypeAuth:
		resp, handleErr = e.handleAuthCommands(ctx, originalReq)
	default:
		return &cProtocol.ClusterResponse{
			Status: 400,
			Error:  fmt.Sprintf("unsupported command type for forwarding: %s", commandType.String()),
		}, nil
	}

	if handleErr != nil {
		e.logger.Errorf("Error handling forwarded request: %v", handleErr)
		return &cProtocol.ClusterResponse{
			Status: 500,
			Error:  fmt.Sprintf("failed to handle forwarded request: %v", handleErr),
		}, nil
	}

	e.logger.Infof("Successfully handled forwarded request, status: %d", resp.Status)

	// 将 ProtocolResponse 转换为 ClusterResponse
	// 创建响应头
	respHead := cProtocol.CreateClusterCommandHead(cProtocol.ClusterCmdForward)

	return &cProtocol.ClusterResponse{
		Head:   respHead,
		Status: resp.Status,
		Data:   resp.Data,
		Error:  resp.Error,
	}, nil
}

// isForwardedRequest 检查请求是否已经被转发过
func (e *Engine) isForwardedRequest(data []byte) bool {
	if len(data) == 0 {
		return false
	}

	var reqData map[string]interface{}
	if err := sonic.Unmarshal(data, &reqData); err != nil {
		return false
	}

	forwarded, exists := reqData["_forwarded"]
	if !exists {
		return false
	}

	if forwardedBool, ok := forwarded.(bool); ok {
		return forwardedBool
	}

	return false
}

// ============================================
