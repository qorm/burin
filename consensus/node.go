package consensus

import (
	"github.com/qorm/burin/cProtocol"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bytedance/sonic"

	"github.com/hashicorp/raft"
	raftboltdb "github.com/hashicorp/raft-boltdb"
	"github.com/sirupsen/logrus"
)

// Node 代表集群中的一个节点 - 纯粹的Raft实现
type Node struct {
	raft     *raft.Raft
	fsm      *FSM
	config   *NodeConfig
	logger   *logrus.Logger
	shutdown chan struct{}
	mu       sync.RWMutex

	// 节点状态
	isLeader bool
	peers    map[string]*Peer
	metadata map[string]interface{}

	// 客户端地址映射（持久化存储）
	clientAddrs map[string]string

	// 快照管理器
	snapshotManager *SnapshotManager

	// 数据同步管理器
	syncManager *DataSyncManager
}

// NodeConfig 节点配置
type NodeConfig struct {
	NodeID            string        `json:"node_id" yaml:"id" mapstructure:"id"`
	BindAddr          string        `json:"bind_addr" yaml:"address" mapstructure:"address"`
	ClientAddr        string        `json:"client_addr" yaml:"client_addr" mapstructure:"client_addr"` // 客户端服务地址
	DataDir           string        `json:"data_dir" yaml:"data_dir" mapstructure:"data_dir"`
	Bootstrap         bool          `json:"bootstrap" yaml:"bootstrap" mapstructure:"bootstrap"`
	JoinAddr          string        `json:"join_addr" yaml:"join_addr" mapstructure:"join_addr"`
	ProtocolAddr      string        `json:"protocol_addr" yaml:"protocol_addr" mapstructure:"protocol_addr"`
	MaxPool           int           `json:"max_pool" yaml:"max_pool" mapstructure:"max_pool"`
	Timeout           time.Duration `json:"timeout" yaml:"timeout" mapstructure:"timeout"`
	HeartbeatTimeout  time.Duration `json:"heartbeat_timeout" yaml:"heartbeat_timeout" mapstructure:"heartbeat_timeout"`
	ElectionTimeout   time.Duration `json:"election_timeout" yaml:"election_timeout" mapstructure:"election_timeout"`
	CommitTimeout     time.Duration `json:"commit_timeout" yaml:"commit_timeout" mapstructure:"commit_timeout"`
	MaxAppendEntries  int           `json:"max_append_entries" yaml:"max_append_entries" mapstructure:"max_append_entries"`
	TrailingLogs      uint64        `json:"trailing_logs" yaml:"trailing_logs" mapstructure:"trailing_logs"`
	SnapshotInterval  time.Duration `json:"snapshot_interval" yaml:"snapshot_interval" mapstructure:"snapshot_interval"`
	SnapshotThreshold uint64        `json:"snapshot_threshold" yaml:"snapshot_threshold" mapstructure:"snapshot_threshold"`
}

// Peer 节点信息
type Peer struct {
	ID         string `json:"id"`
	Address    string `json:"address"`     // Raft 共识地址
	ClientAddr string `json:"client_addr"` // 客户端服务地址
	IsActive   bool   `json:"is_active"`
	Role       string `json:"role"`
}

// NewNode 创建新节点
func NewNode(config *NodeConfig, logger *logrus.Logger) (*Node, error) {
	if logger == nil {
		logger = logrus.New()
	}

	node := &Node{
		config:      config,
		logger:      logger,
		shutdown:    make(chan struct{}),
		peers:       make(map[string]*Peer),
		metadata:    make(map[string]interface{}),
		clientAddrs: make(map[string]string),
	}

	node.snapshotManager = NewSnapshotManager(node)
	node.syncManager = NewDataSyncManager(node)

	if err := node.setupRaft(); err != nil {
		return nil, fmt.Errorf("failed to setup raft: %v", err)
	}

	return node, nil
}

// Start 启动节点
func (n *Node) Start() error {
	n.logger.Infof("Starting consensus node %s", n.config.NodeID)

	// 启动监控
	go n.monitorLeadership()

	// 如果配置了自动加入集群，则尝试加入
	if !n.config.Bootstrap && n.config.JoinAddr != "" {
		go func() {
			time.Sleep(2 * time.Second) // 等待Raft初始化完成
			if err := n.JoinCluster(n.config.JoinAddr); err != nil {
				n.logger.Errorf("Failed to join cluster: %v", err)
			}
		}()
	}

	return nil
}

// Stop 停止节点
func (n *Node) Stop() error {
	n.logger.Info("Stopping consensus node")

	close(n.shutdown)

	// 停止Raft
	if n.raft != nil {
		future := n.raft.Shutdown()
		if err := future.Error(); err != nil {
			n.logger.Errorf("Failed to shutdown raft: %v", err)
			return err
		}
	}

	return nil
}

// GetConfig 获取节点配置
func (n *Node) GetConfig() *NodeConfig {
	return n.config
}

// IsLeader 检查是否为leader
func (n *Node) IsLeader() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.isLeader
}

// GetLeader 获取当前leader地址
func (n *Node) GetLeader() string {
	if n.raft == nil {
		return ""
	}
	_, leaderID := n.raft.LeaderWithID()
	return string(leaderID)
}

// GetPeers 获取集群节点列表
func (n *Node) GetPeers() map[string]*Peer {
	n.mu.RLock()
	defer n.mu.RUnlock()

	peers := make(map[string]*Peer)
	for k, v := range n.peers {
		peers[k] = v
	}
	return peers
}

// ApplyCommand 应用命令到状态机
func (n *Node) ApplyCommand(command []byte) error {
	if !n.IsLeader() {
		return fmt.Errorf("only leader can apply commands")
	}

	future := n.raft.Apply(command, n.config.Timeout)
	if err := future.Error(); err != nil {
		return fmt.Errorf("failed to apply command: %v", err)
	}

	return nil
}

// JoinCluster 加入集群
func (n *Node) JoinCluster(leaderAddr string) error {

	n.logger.Infof("Attempting to join cluster via leader at %s", leaderAddr)

	// 使用配置中的JoinAddr而不是硬编码端口替换
	protocolAddr := n.config.JoinAddr
	if protocolAddr == "" {
		// 如果没有配置JoinAddr，则使用leaderAddr（假设传入的就是协议地址）
		protocolAddr = leaderAddr
	}

	n.logger.Infof("First, getting Raft address from protocol address: %s", protocolAddr)

	// 步骤1: 从Burin协议端口获取Raft端口信息
	raftAddr, err := n.getRaftAddressFromProtocol(protocolAddr)
	if err != nil {
		return fmt.Errorf("failed to get Raft address from %s: %v", protocolAddr, err)
	}

	n.logger.Infof("Got Raft address: %s, now joining cluster", raftAddr)

	// 步骤2: 使用获取到的Raft地址进行集群加入
	return n.joinClusterWithRaftAddr(raftAddr, protocolAddr)
}

// getRaftAddressFromProtocol 从协议端口获取Raft地址
func (n *Node) getRaftAddressFromProtocol(protocolAddr string) (string, error) {
	// 创建cProtocol客户端配置
	config := &cProtocol.ClientConfig{
		Endpoint:            protocolAddr,
		MaxConnsPerEndpoint: 1,
		DialTimeout:         time.Second * 5,
		ReadTimeout:         time.Second * 10,
		WriteTimeout:        time.Second * 10,
	}

	// 创建客户端
	client := cProtocol.NewClient(config, n.logger)
	err := client.Start()
	if err != nil {
		return "", fmt.Errorf("failed to start protocol client: %v", err)
	}
	defer client.Stop()

	// 构建获取Raft地址的请求数据
	requestData := map[string]interface{}{
		"action":       "node_discovery",
		"node_id":      n.config.NodeID,
		"request_type": "get_raft_address",
	}

	data, err := sonic.Marshal(requestData)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %v", err)
	}

	// 创建集群协议请求
	req := &cProtocol.ProtocolRequest{
		Head: cProtocol.CreateClusterCommandHead(cProtocol.ClusterCmdNodeDiscovery),
		Data: data,
	}

	// 设置数据长度
	req.Head.SetContentLength(uint32(len(data))) // 发送请求
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	resp, err := client.SendRequest(ctx, req)
	if err != nil {
		return "", fmt.Errorf("failed to send discovery request: %v", err)
	}

	// 解析响应
	var response map[string]interface{}
	err = sonic.Unmarshal(resp.Data, &response)
	if err != nil {
		return "", fmt.Errorf("failed to parse response: %v", err)
	}

	raftAddr, ok := response["raft_address"].(string)
	if !ok {
		return "", fmt.Errorf("raft_address not found in response")
	}

	return raftAddr, nil
}

// joinClusterWithRaftAddr 使用Raft地址进行集群加入
func (n *Node) joinClusterWithRaftAddr(raftAddr, protocolAddr string) error {

	conn, err := net.Dial("tcp", protocolAddr)
	if err != nil {
		return fmt.Errorf("failed to connect to leader: %v", err)
	}
	defer conn.Close()

	// 构建加入请求数据
	request := map[string]interface{}{
		"node_id":     n.config.NodeID,
		"address":     n.config.BindAddr,   // Raft 协议地址
		"client_addr": n.config.ClientAddr, // 客户端服务地址
	}

	requestData, err := sonic.Marshal(request)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %v", err)
	}

	// 使用cProtocol格式发送请求
	// 创建集群命令头
	head := cProtocol.NewHead()
	head.SetCommand(uint8(cProtocol.ClusterCmdNodeJoin))                                       // 设置节点加入命令
	head.SetConfig(cProtocol.REQ, cProtocol.HaveResponse, uint8(cProtocol.CommandTypeCluster)) // 设置集群命令类型
	head.SetContentLength(uint32(len(requestData)))                                            // 设置内容长度

	// 发送协议头
	if _, err := conn.Write(head.GetBytes()); err != nil {
		return fmt.Errorf("failed to send protocol head: %v", err)
	}

	// 发送请求数据
	if _, err := conn.Write(requestData); err != nil {
		return fmt.Errorf("failed to send request data: %v", err)
	}

	// 读取响应
	response := make([]byte, 1024)
	respLen, err := conn.Read(response)
	if err != nil {
		return fmt.Errorf("failed to read response: %v", err)
	}

	responseStr := string(response[:respLen])
	n.logger.Infof("Join request response: %s", responseStr)

	// 检查响应状态
	if strings.Contains(responseStr, "\"status\":200") {
		n.logger.Infof("Successfully sent join request to leader at %s", protocolAddr)
		return nil
	}

	return fmt.Errorf("join request failed: %s", responseStr)
}

// AddNodeToCluster 添加节点到集群（仅领导者可调用）
func (n *Node) AddNodeToCluster(nodeID, address string) error {
	return n.AddNodeToClusterWithClientAddr(nodeID, address, "")
}

// AddNodeToClusterWithClientAddr 添加节点到集群（包含客户端地址）
func (n *Node) AddNodeToClusterWithClientAddr(nodeID, raftAddr, clientAddr string) error {
	if !n.IsLeader() {
		return fmt.Errorf("only leader can add nodes to cluster")
	}

	n.logger.Infof("Adding node %s (raft=%s, client=%s) to cluster", nodeID, raftAddr, clientAddr)

	// 添加节点到Raft集群
	future := n.raft.AddVoter(
		raft.ServerID(nodeID),
		raft.ServerAddress(raftAddr),
		0,
		0,
	)

	if err := future.Error(); err != nil {
		n.logger.Errorf("Failed to add node %s to cluster: %v", nodeID, err)
		return err
	}

	// 为新节点同步数据
	if err := n.syncManager.SyncNewNode(nodeID, raftAddr); err != nil {
		n.logger.Warnf("Failed to sync data to new node %s: %v", nodeID, err)
	}

	// 保存客户端地址到持久化映射
	n.mu.Lock()
	n.clientAddrs[nodeID] = clientAddr
	n.peers[nodeID] = &Peer{
		ID:         nodeID,
		Address:    raftAddr,
		ClientAddr: clientAddr,
		IsActive:   true,
		Role:       "follower",
	}
	n.mu.Unlock()

	// 通过 Raft 日志同步节点元数据到所有节点
	if clientAddr != "" {
		cmd := map[string]interface{}{
			"operation":   "sync_node_metadata",
			"node_id":     nodeID,
			"client_addr": clientAddr,
		}

		cmdData, err := sonic.Marshal(cmd)
		if err != nil {
			n.logger.Warnf("Failed to marshal sync metadata command: %v", err)
		} else {
			applyFuture := n.raft.Apply(cmdData, n.config.Timeout)
			if err := applyFuture.Error(); err != nil {
				n.logger.Warnf("Failed to apply sync metadata command: %v", err)
			} else {
				n.logger.Infof("Node metadata synced via Raft for node %s", nodeID)
			}
		}
	}

	n.logger.Infof("Node %s successfully added to cluster (client addr: %s)", nodeID, clientAddr)
	return nil
}

// RemoveNodeFromCluster 从集群移除节点
func (n *Node) RemoveNodeFromCluster(nodeID string) error {
	if !n.IsLeader() {
		return fmt.Errorf("only leader can remove nodes from cluster")
	}

	n.logger.Infof("Removing node %s from cluster", nodeID)

	future := n.raft.RemoveServer(raft.ServerID(nodeID), 0, 0)
	if err := future.Error(); err != nil {
		n.logger.Errorf("Failed to remove node %s from cluster: %v", nodeID, err)
		return err
	}

	n.mu.Lock()
	delete(n.peers, nodeID)
	n.mu.Unlock()

	n.logger.Infof("Node %s successfully removed from cluster", nodeID)
	return nil
}

// GetStats 获取节点统计信息
func (n *Node) GetStats() map[string]interface{} {
	stats := make(map[string]interface{})

	if n.raft != nil {
		stats["raft_stats"] = n.raft.Stats()
		stats["state"] = n.raft.State().String()
		_, leaderID := n.raft.LeaderWithID()
		stats["leader"] = string(leaderID)
	}

	stats["node_id"] = n.config.NodeID
	stats["bind_addr"] = n.config.BindAddr
	stats["is_leader"] = n.IsLeader()
	stats["peers"] = n.GetPeers()

	return stats
}

// GetState 获取Raft状态字符串
func (n *Node) GetState() string {
	if n.raft != nil {
		return n.raft.State().String()
	}
	return "Unknown"
}

// AddPeer 添加节点（别名方法，兼容旧接口）
func (n *Node) AddPeer(nodeID, address string) error {
	return n.AddNodeToCluster(nodeID, address)
}

// CreateSnapshot 创建快照（别名方法）
func (n *Node) CreateSnapshot() error {
	return n.Snapshot()
}

// ForceSnapshot 强制创建快照（别名方法）
func (n *Node) ForceSnapshot() error {
	return n.Snapshot()
}

// SyncDataToNode 同步数据到节点（简化实现）
func (n *Node) SyncDataToNode(nodeID, address string) error {
	if n.syncManager != nil {
		return n.syncManager.SyncNewNode(nodeID, address)
	}
	return fmt.Errorf("sync manager not available")
}

// GetSnapshotStats 获取快照统计（简化实现）
func (n *Node) GetSnapshotStats() map[string]interface{} {
	stats := make(map[string]interface{})
	if n.raft != nil {
		raftStats := n.raft.Stats()
		stats["last_snapshot_index"] = raftStats["last_snapshot_index"]
		stats["last_snapshot_term"] = raftStats["last_snapshot_term"]
	}
	return stats
}

// GetSyncStatus 获取同步状态（简化实现）
func (n *Node) GetSyncStatus() map[string]interface{} {
	return map[string]interface{}{
		"sync_enabled": true,
		"status":       "ready",
	}
}

// setupRaft 设置Raft
func (n *Node) setupRaft() error {
	// 创建数据目录
	if err := os.MkdirAll(n.config.DataDir, 0755); err != nil {
		return fmt.Errorf("failed to create data directory: %v", err)
	}

	// 创建FSM
	n.fsm = NewFSM(n.logger)

	// 首先保存当前节点的 ClientAddr 到映射（无需等待 Raft 同步）
	if n.config.ClientAddr != "" {
		n.clientAddrs[n.config.NodeID] = n.config.ClientAddr
		n.logger.Infof("Initialized local clientAddr: %s -> %s", n.config.NodeID, n.config.ClientAddr)
	}

	// 设置节点元数据同步回调
	n.fsm.SetSyncNodeMetadataCallback(func(nodeID, clientAddr string) {
		n.mu.Lock()
		defer n.mu.Unlock()

		n.logger.Infof("Updating client address for node %s: %s", nodeID, clientAddr)
		n.clientAddrs[nodeID] = clientAddr

		// 同时更新 peers 映射
		if peer, exists := n.peers[nodeID]; exists {
			peer.ClientAddr = clientAddr
		}
	})

	// 配置Raft
	config := raft.DefaultConfig()
	config.LocalID = raft.ServerID(n.config.NodeID)
	config.HeartbeatTimeout = n.config.HeartbeatTimeout
	config.ElectionTimeout = n.config.ElectionTimeout
	config.CommitTimeout = n.config.CommitTimeout
	config.MaxAppendEntries = n.config.MaxAppendEntries
	config.TrailingLogs = n.config.TrailingLogs
	config.SnapshotInterval = n.config.SnapshotInterval
	config.SnapshotThreshold = n.config.SnapshotThreshold
	config.LogOutput = io.Discard

	// 创建传输层
	addr, err := net.ResolveTCPAddr("tcp", n.config.BindAddr)
	if err != nil {
		return fmt.Errorf("failed to resolve address: %v", err)
	}

	transport, err := raft.NewTCPTransport(n.config.BindAddr, addr, n.config.MaxPool, n.config.Timeout, os.Stderr)
	if err != nil {
		return fmt.Errorf("failed to create transport: %v", err)
	}

	// 创建日志存储
	logStore, err := raftboltdb.NewBoltStore(filepath.Join(n.config.DataDir, "raft-log.bolt"))
	if err != nil {
		return fmt.Errorf("failed to create log store: %v", err)
	}

	// 创建稳定存储
	stableStore, err := raftboltdb.NewBoltStore(filepath.Join(n.config.DataDir, "raft-stable.bolt"))
	if err != nil {
		return fmt.Errorf("failed to create stable store: %v", err)
	}

	// 创建快照存储
	snapshotStore, err := raft.NewFileSnapshotStore(n.config.DataDir, 3, os.Stderr)
	if err != nil {
		return fmt.Errorf("failed to create snapshot store: %v", err)
	}

	// 创建Raft实例
	r, err := raft.NewRaft(config, n.fsm, logStore, stableStore, snapshotStore, transport)
	if err != nil {
		return fmt.Errorf("failed to create raft: %v", err)
	}

	n.raft = r

	// 如果是引导节点，进行引导
	if n.config.Bootstrap {
		configuration := raft.Configuration{
			Servers: []raft.Server{
				{
					ID:      config.LocalID,
					Address: transport.LocalAddr(),
				},
			},
		}

		future := n.raft.BootstrapCluster(configuration)
		if err := future.Error(); err != nil {
			n.logger.Warnf("Bootstrap failed (may be already bootstrapped): %v", err)
		} else {
			n.logger.Info("Cluster bootstrapped successfully")

			// Bootstrap 节点也需要同步自己的 ClientAddr
			if n.config.ClientAddr != "" {
				// 等待一小段时间确保集群ready
				time.Sleep(500 * time.Millisecond)

				cmd := map[string]interface{}{
					"operation":   "sync_node_metadata",
					"node_id":     n.config.NodeID,
					"client_addr": n.config.ClientAddr,
				}

				cmdData, err := sonic.Marshal(cmd)
				if err != nil {
					n.logger.Warnf("Failed to marshal bootstrap node metadata: %v", err)
				} else {
					applyFuture := n.raft.Apply(cmdData, n.config.Timeout)
					if err := applyFuture.Error(); err != nil {
						n.logger.Warnf("Failed to apply bootstrap node metadata: %v", err)
					} else {
						n.logger.Infof("Bootstrap node metadata synced via Raft")
					}
				}
			}
		}
	}

	return nil
}

// monitorLeadership 监控领导者状态变化
func (n *Node) monitorLeadership() {
	for {
		select {
		case <-n.shutdown:
			return
		case <-time.After(time.Second):
			if n.raft != nil {
				isLeader := n.raft.State() == raft.Leader

				n.mu.Lock()
				wasLeader := n.isLeader
				if isLeader != n.isLeader {
					n.isLeader = isLeader
					if isLeader {
						n.logger.Info("Became leader")
					} else {
						n.logger.Info("No longer leader")
					}
				}
				n.mu.Unlock()

				// 如果刚成为 leader，同步自己的客户端地址
				if isLeader && !wasLeader && n.config.ClientAddr != "" {
					go func() {
						time.Sleep(500 * time.Millisecond) // 等待 leader 稳定
						cmd := map[string]interface{}{
							"operation":   "sync_node_metadata",
							"node_id":     n.config.NodeID,
							"client_addr": n.config.ClientAddr,
						}

						cmdData, err := sonic.Marshal(cmd)
						if err != nil {
							n.logger.Warnf("Failed to marshal leader metadata: %v", err)
							return
						}

						applyFuture := n.raft.Apply(cmdData, n.config.Timeout)
						if err := applyFuture.Error(); err != nil {
							n.logger.Warnf("Failed to sync leader metadata: %v", err)
						} else {
							n.logger.Infof("Leader metadata synced to cluster")
						}
					}()
				}

				// 更新节点信息
				n.updatePeerInfo()
			}
		}
	}
}

// updatePeerInfo 更新节点信息
func (n *Node) updatePeerInfo() {
	if n.raft == nil {
		return
	}

	future := n.raft.GetConfiguration()
	if err := future.Error(); err != nil {
		n.logger.Errorf("Failed to get configuration: %v", err)
		return
	}

	configuration := future.Configuration()

	n.mu.Lock()
	defer n.mu.Unlock()

	// 首先确保当前节点的 ClientAddr 被保存到持久化映射
	if n.config.ClientAddr != "" {
		n.clientAddrs[n.config.NodeID] = n.config.ClientAddr
		n.logger.Debugf("Saved current node ClientAddr: id=%s, clientAddr=%s",
			n.config.NodeID, n.config.ClientAddr)
	}

	// 清空现有节点信息
	n.peers = make(map[string]*Peer)

	// 获取当前的 leader ID
	_, leaderID := n.raft.LeaderWithID()

	// 添加当前配置中的节点
	for _, server := range configuration.Servers {
		role := "follower"
		// 如果是 leader 节点，设置角色为 leader
		if server.ID == leaderID {
			role = "leader"
		}

		nodeID := string(server.ID)

		// 从持久化映射中获取客户端地址
		clientAddr := n.clientAddrs[nodeID]

		// 如果是当前节点且clientAddr为空，使用配置中的ClientAddr
		if nodeID == n.config.NodeID && clientAddr == "" {
			clientAddr = n.config.ClientAddr
			// 更新映射以便下次使用
			if clientAddr != "" {
				n.clientAddrs[nodeID] = clientAddr
			}
		}

		n.peers[nodeID] = &Peer{
			ID:         nodeID,
			Address:    string(server.Address),
			ClientAddr: clientAddr,
			IsActive:   true,
			Role:       role,
		}

		n.logger.Debugf("Updated peer info: id=%s, addr=%s, clientAddr=%s, role=%s",
			nodeID, string(server.Address), clientAddr, role)
	}
} // Snapshot 创建快照
func (n *Node) Snapshot() error {
	if n.raft == nil {
		return fmt.Errorf("raft not initialized")
	}

	future := n.raft.Snapshot()
	return future.Error()
}

// GetSnapshot 获取快照信息（简化版本）
func (n *Node) GetSnapshot() (map[string]interface{}, error) {
	if n.raft == nil {
		return nil, fmt.Errorf("raft not initialized")
	}

	stats := n.raft.Stats()
	return map[string]interface{}{
		"last_snapshot_index": stats["last_snapshot_index"],
		"last_snapshot_term":  stats["last_snapshot_term"],
	}, nil
}

// RestoreSnapshot 恢复快照（简化版本）
func (n *Node) RestoreSnapshot(snapshotPath string) error {
	return fmt.Errorf("snapshot restore not implemented in simplified version")
}

// =================================
// transaction.ConsensusEngine 接口实现
// =================================

// SetValueWithDatabase 在指定数据库中设置键值对
func (n *Node) SetValueWithDatabase(database, key string, value []byte, ttl time.Duration) error {
	if n.raft == nil {
		return fmt.Errorf("raft not initialized")
	}

	cmd := map[string]interface{}{
		"operation": "set",
		"database":  database,
		"key":       key,
		"value":     string(value), // 转换为string避免JSON序列化时base64编码
	}

	// 如果指定了TTL，计算过期时间
	if ttl > 0 {
		cmd["ttl"] = time.Now().Add(ttl)
	}

	cmdData, err := sonic.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("failed to marshal command: %w", err)
	}

	future := n.raft.Apply(cmdData, time.Second*10)
	if err := future.Error(); err != nil {
		return fmt.Errorf("failed to apply command: %w", err)
	}

	return nil
}

// GetValueWithDatabase 从指定数据库中获取值
func (n *Node) GetValueWithDatabase(database, key string) ([]byte, bool, error) {
	if n.fsm == nil {
		return nil, false, fmt.Errorf("fsm not initialized")
	}

	n.fsm.mu.RLock()
	defer n.fsm.mu.RUnlock()

	// 检查数据库是否存在
	dbStore, dbExists := n.fsm.store[database]
	if !dbExists {
		return nil, false, nil
	}

	// 检查键是否存在
	value, exists := dbStore[key]
	if !exists {
		return nil, false, nil
	}

	// 检查是否过期
	if dbTTL, ttlExists := n.fsm.ttl[database]; ttlExists {
		if expiry, keyHasTTL := dbTTL[key]; keyHasTTL {
			if time.Now().After(expiry) {
				// 键已过期，异步删除
				go n.DeleteValueWithDatabase(database, key)
				return nil, false, nil
			}
		}
	}

	// 返回数据副本
	result := make([]byte, len(value))
	copy(result, value)
	return result, true, nil
}

// DeleteValueWithDatabase 从指定数据库中删除键
func (n *Node) DeleteValueWithDatabase(database, key string) error {
	if n.raft == nil {
		return fmt.Errorf("raft not initialized")
	}

	cmd := map[string]interface{}{
		"operation": "delete",
		"database":  database,
		"key":       key,
	}

	cmdData, err := sonic.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("failed to marshal command: %w", err)
	}

	future := n.raft.Apply(cmdData, time.Second*10)
	if err := future.Error(); err != nil {
		return fmt.Errorf("failed to apply command: %w", err)
	}

	return nil
}

// SetFSMCallback 设置FSM的Apply回调函数
func (n *Node) SetFSMCallback(callback func(database, key string, value []byte, ttl time.Duration)) {
	if n.fsm != nil {
		n.fsm.SetApplyCallback(callback)
	}
}

// SetFSMDeleteCallback 设置FSM删除回调
func (n *Node) SetFSMDeleteCallback(callback func(database, key string)) {
	if n.fsm != nil {
		n.fsm.SetDeleteCallback(callback)
	}
}

// SetFSMCreateDatabaseCallback 设置FSM创建数据库回调
func (n *Node) SetFSMCreateDatabaseCallback(callback func(database, dataType string) error) {
	if n.fsm != nil {
		n.fsm.SetCreateDatabaseCallback(callback)
	}
}

// SetFSMDeleteDatabaseCallback 设置FSM删除数据库回调
func (n *Node) SetFSMDeleteDatabaseCallback(callback func(database string) error) {
	if n.fsm != nil {
		n.fsm.SetDeleteDatabaseCallback(callback)
	}
}

// SetFSMGeoAddCallback 设置FSM GeoAdd回调
func (n *Node) SetFSMGeoAddCallback(callback func(database, key string, members interface{}) error) {
	if n.fsm != nil {
		n.fsm.SetGeoAddCallback(callback)
	}
}
