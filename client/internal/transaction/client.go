package transaction

import (
	"context"
	"fmt"
	"sync"
	"time"

	"burin/cProtocol"
	"burin/cid"
	"burin/client/interfaces"

	"github.com/bytedance/sonic"
	"github.com/sirupsen/logrus"
)

// Client 事务客户端
type Client struct {
	protocolClient *cProtocol.Client
	config         *Config
	logger         *logrus.Logger

	// 事务管理
	activeTxs  map[string]*txImpl
	txContexts map[string]txContext // 存储每个事务的context和cancel
	mu         sync.RWMutex
}

// txContext 事务context管理
type txContext struct {
	ctx    context.Context
	cancel context.CancelFunc
}

// Config 事务客户端配置
type Config struct {
	DefaultTimeout        time.Duration
	DefaultIsolationLevel interfaces.IsolationLevel
	MaxConcurrentTxs      int
}

// NewDefaultConfig 创建默认配置
func NewDefaultConfig() *Config {
	return &Config{
		DefaultTimeout:        30 * time.Second,
		DefaultIsolationLevel: interfaces.RepeatableRead,
		MaxConcurrentTxs:      1000,
	}
}

// NewClient 创建事务客户端
func NewClient(protocolClient *cProtocol.Client, config *Config) *Client {
	if config == nil {
		config = NewDefaultConfig()
	}

	return &Client{
		protocolClient: protocolClient,
		config:         config,
		logger:         logrus.New(),
		activeTxs:      make(map[string]*txImpl),
		txContexts:     make(map[string]txContext),
	}
}

// BeginTransaction 开始一个新事务
func (c *Client) BeginTransaction(opts ...interfaces.TransactionOption) (interfaces.Transaction, error) {
	// 应用选项
	options := &interfaces.TransactionOptions{
		Timeout:        c.config.DefaultTimeout,
		IsolationLevel: c.config.DefaultIsolationLevel,
		Database:       "default",
	}
	for _, opt := range opts {
		opt(options)
	}

	// 检查并发限制
	c.mu.RLock()
	if len(c.activeTxs) >= c.config.MaxConcurrentTxs {
		c.mu.RUnlock()
		return nil, fmt.Errorf("max concurrent transactions reached: %d", c.config.MaxConcurrentTxs)
	}
	c.mu.RUnlock()

	// 生成事务ID - 使用cid生成唯一ID
	txID := "tx-" + cid.Generate()

	// 创建事务上下文 - 在Client中管理
	ctx := context.Background()
	txCtx, cancel := context.WithTimeout(ctx, options.Timeout)

	// 创建事务实现 - 不包含ctx和cancel
	tx := &txImpl{
		id:         txID,
		client:     c,
		options:    options,
		status:     interfaces.TxStatusPending,
		startTime:  time.Now(),
		operations: make([]*operation, 0),
		readSet:    make(map[string][]byte),
		writeSet:   make(map[string][]byte),
		deleteSet:  make(map[string]bool),
	}

	// 向服务端发送BEGIN请求
	req := &txRequest{
		Operation:      "begin",
		TxID:           txID,
		IsolationLevel: string(options.IsolationLevel),
		Database:       options.Database,
		Participants:   options.Participants,
		Timeout:        int64(options.Timeout.Seconds()),
	}

	reqData, err := sonic.Marshal(req)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to marshal begin request: %w", err)
	}

	protocolReq := &cProtocol.ProtocolRequest{
		Head: cProtocol.CreateClientCommandHead(cProtocol.CmdTransaction),
		Data: reqData,
		Meta: map[string]string{
			"operation": "tx_begin",
			"tx_id":     txID,
		},
	}

	resp, err := c.protocolClient.SendRequest(ctx, protocolReq)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}

	if resp.Status != 200 {
		cancel()
		return nil, fmt.Errorf("begin transaction failed: %s", resp.Error)
	}

	// 从服务器响应中获取事务ID
	var txResp struct {
		TransactionID string `json:"transaction_id"`
		Status        string `json:"status"`
		Error         string `json:"error"`
	}
	if err := sonic.Unmarshal(resp.Data, &txResp); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to parse begin response: %w", err)
	}

	// 使用服务器返回的事务ID
	serverTxID := txResp.TransactionID
	if serverTxID == "" {
		serverTxID = txID // 如果服务器没有返回，使用生成的ID
	}

	// 更新tx对象的ID
	tx.id = serverTxID

	// 注册活跃事务和context
	c.mu.Lock()
	c.activeTxs[serverTxID] = tx
	c.txContexts[serverTxID] = txContext{
		ctx:    txCtx,
		cancel: cancel,
	}
	c.mu.Unlock()

	c.logger.Infof("Transaction %s started with isolation level %s", serverTxID, options.IsolationLevel)

	return tx, nil
}

// removeTransaction 移除事务和对应的context
func (c *Client) removeTransaction(txID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.activeTxs, txID)
	// 清理context
	if txCtx, exists := c.txContexts[txID]; exists {
		txCtx.cancel()
		delete(c.txContexts, txID)
	}
}

// getTransactionContext 获取事务的context
func (c *Client) getTransactionContext(txID string) (context.Context, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	txCtx, exists := c.txContexts[txID]
	return txCtx.ctx, exists
}

// txImpl 事务实现
type txImpl struct {
	id         string
	client     *Client
	options    *interfaces.TransactionOptions
	status     interfaces.TransactionStatus
	startTime  time.Time
	operations []*operation
	readSet    map[string][]byte
	writeSet   map[string][]byte
	deleteSet  map[string]bool
	mu         sync.RWMutex
}

// operation 事务操作
type operation struct {
	opType    string // "get", "set", "delete"
	key       string
	value     []byte
	timestamp time.Time
}

// txRequest 事务请求
type txRequest struct {
	Operation      string   `json:"operation"` // begin, commit, rollback, get, set, delete
	TxID           string   `json:"transaction_id"`
	Key            string   `json:"key,omitempty"`
	Value          string   `json:"value,omitempty"` // base64编码
	IsolationLevel string   `json:"isolation_level,omitempty"`
	Database       string   `json:"database,omitempty"`
	Type           string   `json:"type,omitempty"` // read-only, read-write
	Participants   []string `json:"participants,omitempty"`
	Timeout        int64    `json:"timeout,omitempty"`
}

// ID 获取事务ID
func (t *txImpl) ID() string {
	return t.id
}

// Status 获取事务状态
func (t *txImpl) Status() interfaces.TransactionStatus {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.status
}

// Get 在事务中读取数据
func (t *txImpl) Get(key string) ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.status != interfaces.TxStatusPending {
		return nil, fmt.Errorf("transaction %s is not active", t.id)
	}

	// 检查写集
	if val, exists := t.writeSet[key]; exists {
		return val, nil
	}

	// 检查删除集
	if t.deleteSet[key] {
		return nil, fmt.Errorf("key %s has been deleted in this transaction", key)
	}

	// 检查读集（可重复读）
	if val, exists := t.readSet[key]; exists {
		return val, nil
	}

	// 获取事务的context
	txCtx, exists := t.client.getTransactionContext(t.id)
	if !exists {
		return nil, fmt.Errorf("transaction context not found for %s", t.id)
	}

	// 构建完整请求，包含 Commands
	fullReq := map[string]interface{}{
		"operation":      "execute",
		"transaction_id": t.id,
		"database":       t.options.Database,
		"commands": []map[string]interface{}{
			{
				"type":     "get",
				"key":      key,
				"database": t.options.Database,
			},
		},
	}

	reqData, err := sonic.Marshal(fullReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal get request: %w", err)
	}

	protocolReq := &cProtocol.ProtocolRequest{
		Head: cProtocol.CreateClientCommandHead(cProtocol.CmdTransaction),
		Data: reqData,
		Meta: map[string]string{
			"operation": "tx_get",
			"tx_id":     t.id,
		},
	}

	resp, err := t.client.protocolClient.SendRequest(txCtx, protocolReq)
	if err != nil {
		return nil, fmt.Errorf("failed to get in transaction: %w", err)
	}

	if resp.Status != 200 {
		return nil, fmt.Errorf("get failed: %s", resp.Error)
	}

	// 解析响应 - 服务器返回 TransactionResponse 包含 Results 数组
	var txResp struct {
		Status  string `json:"status"`
		Results []struct {
			Key   string `json:"key"`
			Value []byte `json:"value"`
			Found bool   `json:"found"`
			Error string `json:"error"`
		} `json:"results"`
		Error string `json:"error"`
	}
	if err := sonic.Unmarshal(resp.Data, &txResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if txResp.Error != "" {
		return nil, fmt.Errorf("server error: %s", txResp.Error)
	}

	if len(txResp.Results) == 0 {
		return nil, fmt.Errorf("no results returned from server")
	}

	result := txResp.Results[0]
	if result.Error != "" {
		return nil, fmt.Errorf("get error: %s", result.Error)
	}

	if !result.Found {
		return nil, fmt.Errorf("key not found: %s", key)
	}

	value := result.Value

	// 记录到读集
	t.readSet[key] = value

	// 记录操作
	t.operations = append(t.operations, &operation{
		opType:    "get",
		key:       key,
		value:     value,
		timestamp: time.Now(),
	})

	t.client.logger.Debugf("Transaction %s read key: %s", t.id, key)

	return value, nil
}

// Set 在事务中写入数据
func (t *txImpl) Set(key string, value []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.status != interfaces.TxStatusPending {
		return fmt.Errorf("transaction %s is not active", t.id)
	}

	if t.options.ReadOnly {
		return fmt.Errorf("cannot write in read-only transaction")
	}

	// 更新写集
	t.writeSet[key] = value

	// 从删除集移除
	delete(t.deleteSet, key)

	// 记录操作
	t.operations = append(t.operations, &operation{
		opType:    "set",
		key:       key,
		value:     value,
		timestamp: time.Now(),
	})

	t.client.logger.Debugf("Transaction %s wrote key: %s (size: %d bytes)", t.id, key, len(value))

	return nil
}

// Delete 在事务中删除数据
func (t *txImpl) Delete(key string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.status != interfaces.TxStatusPending {
		return fmt.Errorf("transaction %s is not active", t.id)
	}

	if t.options.ReadOnly {
		return fmt.Errorf("cannot delete in read-only transaction")
	}

	// 更新删除集
	t.deleteSet[key] = true

	// 从写集移除
	delete(t.writeSet, key)

	// 记录操作
	t.operations = append(t.operations, &operation{
		opType:    "delete",
		key:       key,
		timestamp: time.Now(),
	})

	t.client.logger.Debugf("Transaction %s deleted key: %s", t.id, key)

	return nil
}

// Commit 提交事务
func (t *txImpl) Commit() error {
	t.mu.Lock()
	if t.status != interfaces.TxStatusPending {
		t.mu.Unlock()
		return fmt.Errorf("transaction %s is not in pending state", t.id)
	}
	t.status = interfaces.TxStatusPreparing
	t.mu.Unlock()

	defer func() {
		t.client.removeTransaction(t.id)
	}()

	// 获取事务的context
	txCtx, exists := t.client.getTransactionContext(t.id)
	if !exists {
		t.mu.Lock()
		t.status = interfaces.TxStatusAborted
		t.mu.Unlock()
		return fmt.Errorf("transaction context not found for %s", t.id)
	}

	// 准备提交数据
	commands := make([]map[string]interface{}, 0, len(t.writeSet)+len(t.deleteSet))

	for key, value := range t.writeSet {
		commands = append(commands, map[string]interface{}{
			"type":     "set",
			"key":      key,
			"value":    value, // 发送为字节数组
			"database": t.options.Database,
		})
	}

	for key := range t.deleteSet {
		commands = append(commands, map[string]interface{}{
			"type":     "delete",
			"key":      key,
			"database": t.options.Database,
		})
	}

	// 发送提交请求
	req := map[string]interface{}{
		"operation":      "commit",
		"transaction_id": t.id,
		"database":       t.options.Database,
		"commands":       commands,
	}

	reqData, err := sonic.Marshal(req)
	if err != nil {
		t.mu.Lock()
		t.status = interfaces.TxStatusAborted
		t.mu.Unlock()
		return fmt.Errorf("failed to marshal commit request: %w", err)
	}

	protocolReq := &cProtocol.ProtocolRequest{
		Head: cProtocol.CreateClientCommandHead(cProtocol.CmdTransaction),
		Data: reqData,
		Meta: map[string]string{
			"operation": "tx_commit",
			"tx_id":     t.id,
		},
	}

	resp, err := t.client.protocolClient.SendRequest(txCtx, protocolReq)
	if err != nil {
		t.mu.Lock()
		t.status = interfaces.TxStatusAborted
		t.mu.Unlock()
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	if resp.Status != 200 {
		t.mu.Lock()
		t.status = interfaces.TxStatusAborted
		t.mu.Unlock()
		return fmt.Errorf("commit failed: %s", resp.Error)
	}

	t.mu.Lock()
	t.status = interfaces.TxStatusCommitted
	t.mu.Unlock()

	duration := time.Since(t.startTime)
	t.client.logger.Infof("Transaction %s committed successfully (duration: %v, ops: %d)",
		t.id, duration, len(t.operations))

	return nil
}

// Rollback 回滚事务
func (t *txImpl) Rollback() error {
	t.mu.Lock()
	if t.status != interfaces.TxStatusPending && t.status != interfaces.TxStatusPreparing {
		t.mu.Unlock()
		return fmt.Errorf("transaction %s cannot be rolled back in current state", t.id)
	}
	t.mu.Unlock()

	defer func() {
		t.client.removeTransaction(t.id)
	}()

	// 获取事务的context
	txCtx, exists := t.client.getTransactionContext(t.id)
	if !exists {
		t.mu.Lock()
		t.status = interfaces.TxStatusAborted
		t.mu.Unlock()
		return fmt.Errorf("transaction context not found for %s", t.id)
	}

	// 发送回滚请求
	req := &txRequest{
		Operation: "rollback",
		TxID:      t.id,
		Database:  t.options.Database,
	}

	reqData, err := sonic.Marshal(req)
	if err != nil {
		t.mu.Lock()
		t.status = interfaces.TxStatusAborted
		t.mu.Unlock()
		return fmt.Errorf("failed to marshal rollback request: %w", err)
	}

	protocolReq := &cProtocol.ProtocolRequest{
		Head: cProtocol.CreateClientCommandHead(cProtocol.CmdTransaction),
		Data: reqData,
		Meta: map[string]string{
			"operation": "tx_rollback",
			"tx_id":     t.id,
		},
	}

	resp, err := t.client.protocolClient.SendRequest(txCtx, protocolReq)
	if err != nil {
		t.mu.Lock()
		t.status = interfaces.TxStatusAborted
		t.mu.Unlock()
		return fmt.Errorf("failed to rollback transaction: %w", err)
	}

	if resp.Status != 200 {
		t.mu.Lock()
		t.status = interfaces.TxStatusAborted
		t.mu.Unlock()
		return fmt.Errorf("rollback failed: %s", resp.Error)
	}

	t.mu.Lock()
	t.status = interfaces.TxStatusAborted
	t.mu.Unlock()

	t.client.logger.Infof("Transaction %s rolled back successfully", t.id)

	return nil
}
