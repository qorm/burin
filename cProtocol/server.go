package cProtocol

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
)

// ProtocolRequest 协议请求
type ProtocolRequest struct {
	Head       *HEAD
	Data       []byte
	Meta       map[string]string
	Connection net.Conn // 添加连接对象，用于认证处理器标记认证状态
}

// ProtocolResponse 协议响应
type ProtocolResponse struct {
	Head   *HEAD
	Status int
	Data   []byte
	Error  string
}

// Handler 命令处理器接口
type Handler interface {
	Handle(ctx context.Context, req *ProtocolRequest) (*ProtocolResponse, error)
}

// HandlerFunc 处理器函数类型
type HandlerFunc func(ctx context.Context, req *ProtocolRequest) (*ProtocolResponse, error)

func (f HandlerFunc) Handle(ctx context.Context, req *ProtocolRequest) (*ProtocolResponse, error) {
	return f(ctx, req)
}

// Router 统一命令路由器
type Router struct {
	mu       sync.RWMutex
	handlers map[string]Handler // 使用字符串键来统一存储所有类型的命令
	logger   *logrus.Logger
}

// ClusterRouter 集群命令路由器
type ClusterRouter struct {
	mu       sync.RWMutex
	handlers map[ClusterCommand]ClusterHandler
	logger   *logrus.Logger
}

// NewRouter 创建新的路由器
func NewRouter(logger *logrus.Logger) *Router {
	return &Router{
		handlers: make(map[string]Handler),
		logger:   logger,
	}
}

// NewClusterRouter 创建新的集群命令路由器
func NewClusterRouter(logger *logrus.Logger) *ClusterRouter {
	return &ClusterRouter{
		handlers: make(map[ClusterCommand]ClusterHandler),
		logger:   logger,
	}
}

// Register 注册命令处理器 - 支持所有命令类型
func (r *Router) Register(cmdType CommandType, cmd interface{}, handler Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// 生成统一的键
	var key string
	switch cmdType {
	case CommandTypeClient:
		if clientCmd, ok := cmd.(Command); ok {
			key = fmt.Sprintf("client_%d", uint8(clientCmd))
		} else {
			r.logger.Warnf("Failed to cast client command: %T %v", cmd, cmd)
			return
		}
	case CommandTypeCluster:
		if clusterCmd, ok := cmd.(ClusterCommand); ok {
			key = fmt.Sprintf("cluster_%d", uint32(clusterCmd))
		} else {
			r.logger.Warnf("Failed to cast cluster command: %T %v", cmd, cmd)
			return
		}
	case CommandTypeGeo:
		if geoCmd, ok := cmd.(GeoCommand); ok {
			key = fmt.Sprintf("geo_%d", uint8(geoCmd))
		} else {
			r.logger.Warnf("Failed to cast geo command: %T %v", cmd, cmd)
			return
		}
	case CommandTypeAuth:
		if authCmd, ok := cmd.(AuthCommand); ok {
			key = fmt.Sprintf("auth_%d", uint8(authCmd))
		} else {
			r.logger.Warnf("Failed to cast auth command: %T %v", cmd, cmd)
			return
		}
	default:
		r.logger.Warnf("Unknown command type: %v", cmdType)
		return
	}

	if key == "" {
		r.logger.Warnf("Empty key generated for cmdType=%s, cmd=%v", cmdType.String(), cmd)
		return
	}

	r.handlers[key] = handler
	r.logger.Infof("Registered handler for %s command %v with key: %s", cmdType.String(), cmd, key)
}

// RegisterCommand 注册普通命令处理器（兼容旧接口）
func (r *Router) RegisterCommand(cmd Command, handler Handler) {
	r.Register(CommandTypeClient, cmd, handler)
}

// RegisterCluster 注册集群命令处理器
func (cr *ClusterRouter) Register(cmd ClusterCommand, handler ClusterHandler) {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	cr.handlers[cmd] = handler
	cr.logger.Infof("Registered cluster handler for command %s", cmd.String())
}

// Route 路由请求到对应处理器
func (r *Router) Route(ctx context.Context, req *ProtocolRequest) (*ProtocolResponse, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// 根据请求头中的命令类型生成键
	cmdType := CommandType(req.Head.GetCommandType())
	cmdValue := req.Head.GetCommand() // 直接使用头中的命令值
	var key string

	switch cmdType {
	case CommandTypeClient:
		key = fmt.Sprintf("client_%d", req.Head.GetCommand())
	case CommandTypeCluster:
		key = fmt.Sprintf("cluster_%d", req.Head.GetCommand())
	case CommandTypeGeo:
		key = fmt.Sprintf("geo_%d", req.Head.GetCommand())
	case CommandTypeAuth:
		key = fmt.Sprintf("auth_%d", req.Head.GetCommand())
	default:
		return &ProtocolResponse{
			Status: 400,
			Error:  fmt.Sprintf("unknown command type: %s", cmdType.String()),
		}, nil
	}

	r.logger.Infof("Looking for handler with key: %s (cmdType=%s, cmdValue=%d)",
		key, cmdType.String(), cmdValue)

	handler, exists := r.handlers[key]
	if !exists {
		// 列出已注册的处理器以便调试
		keys := make([]string, 0, len(r.handlers))
		for k := range r.handlers {
			keys = append(keys, k)
		}
		r.logger.Infof("Available handlers: %v", keys)

		return &ProtocolResponse{
			Status: 404,
			Error:  fmt.Sprintf("unknown command: %d (type: %s)", cmdValue, cmdType.String()),
		}, nil
	}

	return handler.Handle(ctx, req)
}

// RouteCluster 路由集群命令到对应处理器
func (cr *ClusterRouter) Route(ctx context.Context, req *ClusterRequest) (*ClusterResponse, error) {
	cr.mu.RLock()
	// 从Head获取集群命令
	clusterCommand := ClusterCommand(req.Head.GetCommand())
	handler, exists := cr.handlers[clusterCommand]
	cr.mu.RUnlock()

	if !exists {
		return &ClusterResponse{
			Status: 404,
			Error:  fmt.Sprintf("unknown cluster command: %s", clusterCommand.String()),
		}, nil
	}

	return handler.HandleCluster(ctx, req)
}

// Server 协议服务器 - 支持多种命令类型的统一路由
type Server struct {
	addr     string
	router   *Router // 统一路由器，支持所有命令类型
	listener net.Listener
	running  int32
	mu       sync.RWMutex
	conns    map[net.Conn]bool
	connAuth map[net.Conn]*ConnectionAuth // 每个连接的认证状态
	logger   *logrus.Logger

	// 配置
	readTimeout  time.Duration
	writeTimeout time.Duration
	maxConns     int
	connCount    int32
	requireAuth  bool // 是否强制要求认证
}

// ConnectionAuth 连接认证状态
type ConnectionAuth struct {
	Authenticated bool                // 是否已认证
	Username      string              // 认证用户名
	Role          string              // 用户角色 (superadmin, admin, readwrite, readonly)
	Permissions   map[string][]string // 数据库权限映射: database -> []permission
	LoginTime     time.Time           // 登录时间
	CurrentDB     string              // 当前操作的数据库
	mu            sync.RWMutex
}

// ServerConfig 服务器配置
type ServerConfig struct {
	Address       string        `yaml:"address" mapstructure:"address"`
	ReadTimeout   time.Duration `yaml:"read_timeout" mapstructure:"read_timeout"`
	WriteTimeout  time.Duration `yaml:"write_timeout" mapstructure:"write_timeout"`
	MaxConns      int           `yaml:"max_conns" mapstructure:"max_conns"`
	RequireAuth   bool          `yaml:"require_auth" mapstructure:"require_auth"`     // 是否强制要求认证
	ForwardLeader bool          `yaml:"forward_leader" mapstructure:"forward_leader"` // 是否转发写操作到 leader
}

// NewServer 创建新的协议服务器 - 支持多种命令类型路由
func NewServer(config *ServerConfig, logger *logrus.Logger) *Server {
	if config.ReadTimeout == 0 {
		config.ReadTimeout = 30 * time.Second
	}
	if config.WriteTimeout == 0 {
		config.WriteTimeout = 30 * time.Second
	}
	if config.MaxConns == 0 {
		config.MaxConns = 10000
	}

	return &Server{
		addr:         config.Address,
		router:       NewRouter(logger), // 使用统一路由器
		readTimeout:  config.ReadTimeout,
		writeTimeout: config.WriteTimeout,
		maxConns:     config.MaxConns,
		requireAuth:  config.RequireAuth,
		conns:        make(map[net.Conn]bool),
		connAuth:     make(map[net.Conn]*ConnectionAuth),
		logger:       logger,
	}
}

// SetRouter 设置统一路由器
func (s *Server) SetRouter(router *Router) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.router = router
	s.logger.Info("Set unified router for all command types")
}

// RegisterRouter 向后兼容方法 - 已弃用，使用 SetRouter
func (s *Server) RegisterRouter(cmdType CommandType, router *Router) {
	s.logger.Warnf("RegisterRouter is deprecated for command type %s, use SetRouter instead", cmdType.String())
	s.SetRouter(router)
}

// NewServerWithRouter 向后兼容的构造函数 - 使用统一路由器
func NewServerWithRouter(config *ServerConfig, router *Router, logger *logrus.Logger) *Server {
	server := NewServer(config, logger)
	server.SetRouter(router)
	return server
}

// Start 启动服务器
func (s *Server) Start() error {
	if !atomic.CompareAndSwapInt32(&s.running, 0, 1) {
		return errors.New("server already running")
	}

	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		atomic.StoreInt32(&s.running, 0)
		return fmt.Errorf("failed to listen on %s: %w", s.addr, err)
	}

	s.listener = listener
	s.logger.Infof("cProtocol server listening on %s", s.addr)

	go s.acceptLoop()
	return nil
}

// Stop 停止服务器
func (s *Server) Stop() error {
	if !atomic.CompareAndSwapInt32(&s.running, 1, 0) {
		return errors.New("server not running")
	}

	if s.listener != nil {
		s.listener.Close()
	}

	// 关闭所有连接
	s.mu.Lock()
	for conn := range s.conns {
		conn.Close()
	}
	s.conns = make(map[net.Conn]bool)
	s.connAuth = make(map[net.Conn]*ConnectionAuth)
	s.mu.Unlock()

	s.logger.Info("cProtocol server stopped")
	return nil
}

// MarkConnectionAuthenticated 标记连接为已认证
func (s *Server) MarkConnectionAuthenticated(conn net.Conn, username, role string, permissions map[string][]string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if auth, exists := s.connAuth[conn]; exists {
		auth.mu.Lock()
		auth.Authenticated = true
		auth.Username = username
		auth.Role = role
		auth.Permissions = permissions
		auth.LoginTime = time.Now()
		auth.mu.Unlock()
	} else {
		s.connAuth[conn] = &ConnectionAuth{
			Authenticated: true,
			Username:      username,
			Role:          role,
			Permissions:   permissions,
			LoginTime:     time.Now(),
		}
	}
	s.logger.Infof("Connection authenticated: %s (user: %s, role: %s, databases: %d)",
		conn.RemoteAddr(), username, role, len(permissions))
}

// IsConnectionAuthenticated 检查连接是否已认证
func (s *Server) IsConnectionAuthenticated(conn net.Conn) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if auth, exists := s.connAuth[conn]; exists {
		auth.mu.RLock()
		defer auth.mu.RUnlock()
		return auth.Authenticated
	}
	return false
}

// GetConnectionUsername 获取连接的认证用户名
func (s *Server) GetConnectionUsername(conn net.Conn) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if auth, exists := s.connAuth[conn]; exists {
		auth.mu.RLock()
		defer auth.mu.RUnlock()
		return auth.Username
	}
	return ""
}

// GetConnectionRole 获取连接的用户角色
func (s *Server) GetConnectionRole(conn net.Conn) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if auth, exists := s.connAuth[conn]; exists {
		auth.mu.RLock()
		defer auth.mu.RUnlock()
		return auth.Role
	}
	return ""
}

// GetConnectionAuth 获取连接的完整认证信息
func (s *Server) GetConnectionAuth(conn net.Conn) *ConnectionAuth {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if auth, exists := s.connAuth[conn]; exists {
		auth.mu.RLock()
		defer auth.mu.RUnlock()
		// 返回副本以避免并发问题
		permCopy := make(map[string][]string)
		for db, perms := range auth.Permissions {
			permCopy[db] = append([]string{}, perms...)
		}
		return &ConnectionAuth{
			Authenticated: auth.Authenticated,
			Username:      auth.Username,
			Role:          auth.Role,
			Permissions:   permCopy,
			LoginTime:     auth.LoginTime,
		}
	}
	return nil
}

// HasPermission 检查连接是否有指定数据库的指定权限
func (s *Server) HasPermission(conn net.Conn, database, permission string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if auth, exists := s.connAuth[conn]; exists {
		auth.mu.RLock()
		defer auth.mu.RUnlock()

		// SuperAdmin 拥有所有权限
		if auth.Role == "superadmin" {
			s.logger.Debugf("Permission granted: superadmin role (user=%s, database=%s, permission=%s)",
				auth.Username, database, permission)
			return true
		}

		// 检查是否有该数据库的权限
		if perms, ok := auth.Permissions[database]; ok {
			for _, perm := range perms {
				if perm == "all" || perm == permission {
					s.logger.Debugf("Permission granted: user=%s, database=%s, permission=%s, granted=%s",
						auth.Username, database, permission, perm)
					return true
				}
			}
		}

		s.logger.Debugf("Permission denied: user=%s, role=%s, database=%s, permission=%s, available_perms=%v",
			auth.Username, auth.Role, database, permission, auth.Permissions[database])
		return false
	}

	s.logger.Warnf("Permission denied: connection not authenticated")
	return false
}

// SetConnectionCurrentDB 设置连接的当前数据库
func (s *Server) SetConnectionCurrentDB(conn net.Conn, database string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if auth, exists := s.connAuth[conn]; exists {
		auth.mu.Lock()
		auth.CurrentDB = database
		auth.mu.Unlock()
		s.logger.Debugf("Set current database for connection: database=%s, user=%s", database, auth.Username)
	}
}

// GetConnectionCurrentDB 获取连接的当前数据库
func (s *Server) GetConnectionCurrentDB(conn net.Conn) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if auth, exists := s.connAuth[conn]; exists {
		auth.mu.RLock()
		defer auth.mu.RUnlock()
		return auth.CurrentDB
	}
	return ""
}

// IsRunning 检查服务器是否运行
func (s *Server) IsRunning() bool {
	return atomic.LoadInt32(&s.running) == 1
}

// GetConnCount 获取当前连接数
func (s *Server) GetConnCount() int {
	return int(atomic.LoadInt32(&s.connCount))
}

// acceptLoop 接受连接循环
func (s *Server) acceptLoop() {
	for atomic.LoadInt32(&s.running) == 1 {
		conn, err := s.listener.Accept()
		if err != nil {
			if atomic.LoadInt32(&s.running) == 0 {
				return
			}
			s.logger.Errorf("Failed to accept connection: %v", err)
			continue
		}

		// 检查连接数限制
		if int(atomic.LoadInt32(&s.connCount)) >= s.maxConns {
			conn.Close()
			s.logger.Warn("Max connections reached, rejected new connection")
			continue
		}

		// 记录连接
		s.mu.Lock()
		s.conns[conn] = true
		// 初始化连接认证状态为未认证
		s.connAuth[conn] = &ConnectionAuth{
			Authenticated: false,
			Username:      "",
		}
		s.mu.Unlock()
		atomic.AddInt32(&s.connCount, 1)

		go s.handleConnection(conn)
	}
}

// handleConnection 处理单个连接
func (s *Server) handleConnection(conn net.Conn) {
	s.logger.Infof("New connection established from %s", conn.RemoteAddr().String())

	defer func() {
		s.logger.Infof("Closing connection from %s", conn.RemoteAddr().String())
		conn.Close()

		// 移除连接记录
		s.mu.Lock()
		delete(s.conns, conn)
		delete(s.connAuth, conn) // 清理认证状态
		s.mu.Unlock()
		atomic.AddInt32(&s.connCount, -1)
	}()

	buffer := make([]byte, 4096)

	for atomic.LoadInt32(&s.running) == 1 {
		// 设置读超时
		conn.SetReadDeadline(time.Now().Add(s.readTimeout))

		// 读取协议头部（6字节）
		n, err := conn.Read(buffer[:6])
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue // 读超时，继续循环
			}
			return // 连接错误，退出
		}

		if n != 6 {
			s.logger.Warn("Invalid protocol header size")
			return
		}

		// 解析协议头部
		head, err := HeadFromBytes(buffer[:6])
		if err != nil {
			s.logger.Errorf("Failed to parse protocol header: %v", err)
			return
		}

		// 读取数据体
		dataLen := head.GetContentLength()
		var data []byte
		if dataLen > 0 {
			if dataLen > uint32(len(buffer)) {
				data = make([]byte, dataLen)
			} else {
				data = buffer[:dataLen]
			}

			// 完整读取所有数据（处理大数据包）
			totalRead := 0
			for totalRead < int(dataLen) {
				n, err := conn.Read(data[totalRead:])
				if err != nil {
					s.logger.Errorf("Failed to read data: %v (read %d/%d bytes)", err, totalRead, dataLen)
					return
				}
				totalRead += n
			}
		}

		// 获取命令类型
		commandType := CommandType(head.GetCommandType())
		clientCommand := head.GetCommand()

		s.logger.Infof("Received request - commandType: %d (%s), clientCommand: %d",
			commandType, commandType.String(), clientCommand)

		// 检查连接认证状态
		isAuthenticated := s.IsConnectionAuthenticated(conn)
		isLoginCommand := (commandType == CommandTypeAuth && clientCommand == uint8(AuthCommandLogin))
		isClusterCommand := (commandType == CommandTypeCluster)

		// 如果连接未认证，只允许登录命令和集群命令，否则强制断开连接
		if !isAuthenticated && !isLoginCommand && !isClusterCommand {
			s.logger.Warnf("Unauthenticated connection attempted to execute non-login command: %s (command: %d), closing connection from %s",
				commandType.String(), clientCommand, conn.RemoteAddr().String())

			// 直接使用错误响应格式（避免JSON序列化）
			errorMsg := "authentication required: you must be login"
			resp := &ProtocolResponse{
				Status: 401,
				Data:   nil,
				Error:  errorMsg,
			}
			s.sendResponse(conn, resp)

			// 强制关闭连接
			conn.Close()
			return
		}

		// 使用统一路由器处理所有命令类型
		s.mu.RLock()
		router := s.router
		s.mu.RUnlock()

		if router == nil {
			s.logger.Warn("No unified router configured")
			resp := &ProtocolResponse{
				Status: 500,
				Error:  "no router configured",
			}
			s.sendResponse(conn, resp)
			continue
		}

		// 构造请求
		req := &ProtocolRequest{
			Head:       head,
			Data:       data,
			Connection: conn, // 传递连接对象
			Meta: map[string]string{
				"command_type": fmt.Sprintf("%d", commandType),
				"remote_addr":  conn.RemoteAddr().String(),
				"username":     s.GetConnectionUsername(conn),
				"role":         s.GetConnectionRole(conn),
			},
		}

		// 路由并处理请求
		ctx := context.Background()
		resp, err := router.Route(ctx, req)
		if err != nil {
			s.logger.Errorf("Router error for command type %s: %v", commandType.String(), err)
			resp = &ProtocolResponse{
				Status: 500,
				Error:  err.Error(),
			}
		}

		s.logger.Infof("Response for command type %s: status=%d, error=%s",
			commandType.String(), resp.Status, resp.Error)
		s.sendResponse(conn, resp)
	}
}

// sendResponse 发送响应
func (s *Server) sendResponse(conn net.Conn, resp *ProtocolResponse) {
	// 优化协议：所有响应统一格式
	// 前2字节：状态码（Big Endian）
	// 后续字节：业务数据或错误消息

	// 编码状态码（2字节，Big Endian）
	statusBytes := []byte{byte(resp.Status >> 8), byte(resp.Status & 0xFF)}

	var responseData []byte
	if resp.Status != 200 {
		// 错误响应：2字节状态码 + 错误消息
		errorBytes := []byte(resp.Error)
		responseData = make([]byte, 2+len(errorBytes))
		copy(responseData[0:2], statusBytes)
		copy(responseData[2:], errorBytes)
	} else {
		// 正常响应：2字节状态码(200) + 业务数据
		responseData = make([]byte, 2+len(resp.Data))
		copy(responseData[0:2], statusBytes)
		copy(responseData[2:], resp.Data)
	}

	// 创建或使用响应头部
	var head *HEAD
	if resp.Head != nil {
		// 使用响应中提供的 Head
		head = resp.Head
		head.SetContentLength(uint32(len(responseData)))
	} else {
		// 创建默认响应头部
		head = NewHead()
		head.SetCommand(uint8(CmdPong)) // 响应命令
		head.SetContentLength(uint32(len(responseData)))
		// 设置Low字节的必要字段 - 使用客户端命令类型
		head.SetConfig(REP, NoResponse, uint8(CommandTypeClient))
	}

	// 设置写超时
	conn.SetWriteDeadline(time.Now().Add(s.writeTimeout))

	// 发送头部（处理大数据包）
	headBytes := head.GetBytes()
	totalWritten := 0
	for totalWritten < len(headBytes) {
		n, err := conn.Write(headBytes[totalWritten:])
		if err != nil {
			s.logger.Errorf("Failed to send response header: %v", err)
			return
		}
		totalWritten += n
	}

	// 发送数据（处理大数据包）
	totalWritten = 0
	for totalWritten < len(responseData) {
		n, err := conn.Write(responseData[totalWritten:])
		if err != nil {
			s.logger.Errorf("Failed to send response data: %v", err)
			return
		}
		totalWritten += n
	}
}
