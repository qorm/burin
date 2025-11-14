package business

import (
	"context"
	"fmt"
	"time"

	"github.com/bytedance/sonic"

	"burin/cProtocol"
	"burin/transaction"
)

// ============================================
// 事务处理器
// ============================================

// handleTransaction 处理事务命令
func (e *Engine) handleTransaction(ctx context.Context, req *cProtocol.ProtocolRequest) (*cProtocol.ProtocolResponse, error) {
	e.updateStats()

	// 检查是否是复制请求（通过检查是否包含_replicated字段）
	var testReq map[string]interface{}
	if err := sonic.Unmarshal(req.Data, &testReq); err == nil {
		if replicated, exists := testReq["_replicated"]; exists && replicated == true {
			// 这是一个复制请求，转发给复制处理器
			return e.handleReplication(ctx, req)
		}
	}

	// 检查事务管理器是否可用
	if e.transactionMgr == nil {
		return &cProtocol.ProtocolResponse{
			Status: 503,
			Error:  "transaction manager not available",
		}, nil
	}

	// 解析事务请求
	var txnReq TransactionRequest
	if err := sonic.Unmarshal(req.Data, &txnReq); err != nil {
		return &cProtocol.ProtocolResponse{
			Status: 400,
			Error:  "invalid transaction request format: " + err.Error(),
		}, nil
	}

	// 只有leader节点可以处理事务，如果不是且未被转发，则转发到leader
	if !e.isForwardedRequest(req.Data) {
		if e.consensusNode != nil && !e.consensusNode.IsLeader() {
			e.logger.Infof("Forwarding transaction operation to leader")
			return e.forwardToLeader(ctx, req)
		}
	}

	// 根据操作类型处理事务
	switch txnReq.Operation {
	case "begin":
		return e.handleTransactionBegin(ctx, &txnReq)
	case "commit":
		return e.handleTransactionCommit(ctx, &txnReq)
	case "rollback":
		return e.handleTransactionRollback(ctx, &txnReq)
	case "execute":
		return e.handleTransactionExecute(ctx, &txnReq)
	case "status":
		return e.handleTransactionStatus(ctx, &txnReq)
	case "cluster_setup":
		// TODO: 实现集群设置功能
		return &cProtocol.ProtocolResponse{
			Status: 501,
			Error:  "cluster_setup not implemented yet",
		}, nil
	default:
		return &cProtocol.ProtocolResponse{
			Status: 400,
			Error:  "unknown transaction operation: " + txnReq.Operation,
		}, nil
	}
}

// handleTransactionBegin 开始事务
func (e *Engine) handleTransactionBegin(ctx context.Context, req *TransactionRequest) (*cProtocol.ProtocolResponse, error) {
	// 确定事务类型
	txnType := transaction.ReadWriteTransaction
	if req.Type == "read-only" {
		txnType = transaction.ReadOnlyTransaction
	}

	// 开始事务
	txn, err := e.transactionMgr.BeginTransaction(ctx, txnType)
	if err != nil {
		resp := TransactionResponse{
			Status: "error",
			Error:  fmt.Sprintf("failed to begin transaction: %v", err),
		}
		respData, _ := sonic.Marshal(resp)
		return &cProtocol.ProtocolResponse{
			Status: 500,
			Data:   respData,
			Error:  "failed to begin transaction",
		}, nil
	}

	// 如果客户端指定了超时，更新事务的超时时间
	if req.Timeout > 0 {
		txn.Timeout = time.Duration(req.Timeout) * time.Second
		e.logger.Infof("Set transaction %s timeout to %v", txn.ID, txn.Timeout)
	}

	e.logger.Infof("Started transaction: %s", txn.ID)

	resp := TransactionResponse{
		TransactionID: txn.ID,
		Status:        "pending",
	}
	respData, _ := sonic.Marshal(resp)

	return &cProtocol.ProtocolResponse{
		Status: 200,
		Data:   respData,
	}, nil
}

// handleTransactionCommit 提交事务
func (e *Engine) handleTransactionCommit(ctx context.Context, req *TransactionRequest) (*cProtocol.ProtocolResponse, error) {
	if req.TransactionID == "" {
		resp := TransactionResponse{
			Status: "error",
			Error:  "transaction_id is required",
		}
		respData, _ := sonic.Marshal(resp)
		return &cProtocol.ProtocolResponse{
			Status: 400,
			Data:   respData,
			Error:  "missing transaction_id",
		}, nil
	}

	// 如果请求包含commands，先将这些命令添加到事务中
	if len(req.Commands) > 0 {
		e.logger.Infof("Adding %d commands to transaction %s before commit", len(req.Commands), req.TransactionID)

		for i, cmd := range req.Commands {
			// 将命令转换为事务操作
			var op *transaction.TransactionOp

			switch cmd.Type {
			case "set":
				op = &transaction.TransactionOp{
					Type:      transaction.OpSet,
					Database:  cmd.Database,
					Key:       cmd.Key,
					Value:     cmd.Value,
					Timestamp: time.Now(),
				}

			case "delete":
				op = &transaction.TransactionOp{
					Type:      transaction.OpDelete,
					Database:  cmd.Database,
					Key:       cmd.Key,
					Timestamp: time.Now(),
				}

			default:
				e.logger.Warnf("Unsupported command type: %s", cmd.Type)
				continue
			}

			// 添加操作到事务
			if err := e.transactionMgr.AddOperation(req.TransactionID, op); err != nil {
				e.logger.Errorf("Failed to add operation %d to transaction %s: %v", i+1, req.TransactionID, err)
				resp := TransactionResponse{
					TransactionID: req.TransactionID,
					Status:        "error",
					Error:         fmt.Sprintf("failed to add operation %d: %v", i+1, err),
				}
				respData, _ := sonic.Marshal(resp)
				return &cProtocol.ProtocolResponse{
					Status: 500,
					Data:   respData,
					Error:  "add operation failed",
				}, nil
			}

			e.logger.Infof("Added operation %d to transaction %s: %s %s", i+1, req.TransactionID, cmd.Type, cmd.Key)
		}

		e.logger.Infof("Successfully added %d operations to transaction %s", len(req.Commands), req.TransactionID)
	}

	err := e.transactionMgr.CommitTransaction(ctx, req.TransactionID)
	if err != nil {
		resp := TransactionResponse{
			TransactionID: req.TransactionID,
			Status:        "error",
			Error:         fmt.Sprintf("failed to commit transaction: %v", err),
		}
		respData, _ := sonic.Marshal(resp)
		return &cProtocol.ProtocolResponse{
			Status: 500,
			Data:   respData,
			Error:  err.Error(), // 使用原始错误信息
		}, nil
	}

	e.logger.Infof("Committed transaction: %s", req.TransactionID)

	resp := TransactionResponse{
		TransactionID: req.TransactionID,
		Status:        "committed",
	}
	respData, _ := sonic.Marshal(resp)

	return &cProtocol.ProtocolResponse{
		Status: 200,
		Data:   respData,
	}, nil
}

// handleTransactionRollback 回滚事务
func (e *Engine) handleTransactionRollback(ctx context.Context, req *TransactionRequest) (*cProtocol.ProtocolResponse, error) {
	if req.TransactionID == "" {
		resp := TransactionResponse{
			Status: "error",
			Error:  "transaction_id is required",
		}
		respData, _ := sonic.Marshal(resp)
		return &cProtocol.ProtocolResponse{
			Status: 400,
			Data:   respData,
			Error:  "missing transaction_id",
		}, nil
	}

	err := e.transactionMgr.AbortTransaction(ctx, req.TransactionID)
	if err != nil {
		resp := TransactionResponse{
			TransactionID: req.TransactionID,
			Status:        "error",
			Error:         fmt.Sprintf("failed to rollback transaction: %v", err),
		}
		respData, _ := sonic.Marshal(resp)
		return &cProtocol.ProtocolResponse{
			Status: 500,
			Data:   respData,
			Error:  "rollback failed",
		}, nil
	}

	e.logger.Infof("Rolled back transaction: %s", req.TransactionID)

	resp := TransactionResponse{
		TransactionID: req.TransactionID,
		Status:        "aborted",
	}
	respData, _ := sonic.Marshal(resp)

	return &cProtocol.ProtocolResponse{
		Status: 200,
		Data:   respData,
	}, nil
}

// handleTransactionExecute 在事务中执行命令
func (e *Engine) handleTransactionExecute(ctx context.Context, req *TransactionRequest) (*cProtocol.ProtocolResponse, error) {
	if req.TransactionID == "" {
		resp := TransactionResponse{
			Status: "error",
			Error:  "transaction_id is required",
		}
		respData, _ := sonic.Marshal(resp)
		return &cProtocol.ProtocolResponse{
			Status: 400,
			Data:   respData,
			Error:  "missing transaction_id",
		}, nil
	}

	if len(req.Commands) == 0 {
		resp := TransactionResponse{
			TransactionID: req.TransactionID,
			Status:        "error",
			Error:         "no commands to execute",
		}
		respData, _ := sonic.Marshal(resp)
		return &cProtocol.ProtocolResponse{
			Status: 400,
			Data:   respData,
			Error:  "missing commands",
		}, nil
	}

	// 执行事务中的命令
	var results []TransactionResult
	for _, cmd := range req.Commands {
		result := e.executeTransactionCommand(ctx, req.TransactionID, cmd)
		results = append(results, result)
	}

	e.logger.Infof("Executed %d commands in transaction: %s", len(req.Commands), req.TransactionID)

	resp := TransactionResponse{
		TransactionID: req.TransactionID,
		Status:        "success",
		Results:       results,
	}
	respData, _ := sonic.Marshal(resp)

	return &cProtocol.ProtocolResponse{
		Status: 200,
		Data:   respData,
	}, nil
}

// handleTransactionStatus 查询事务状态
func (e *Engine) handleTransactionStatus(ctx context.Context, req *TransactionRequest) (*cProtocol.ProtocolResponse, error) {
	if req.TransactionID == "" {
		resp := TransactionResponse{
			Status: "error",
			Error:  "transaction_id is required",
		}
		respData, _ := sonic.Marshal(resp)
		return &cProtocol.ProtocolResponse{
			Status: 400,
			Data:   respData,
			Error:  "missing transaction_id",
		}, nil
	}

	// 获取事务状态
	txn, err := e.transactionMgr.GetTransaction(req.TransactionID)
	if err != nil {
		resp := TransactionResponse{
			TransactionID: req.TransactionID,
			Status:        "error",
			Error:         fmt.Sprintf("transaction not found: %v", err),
		}
		respData, _ := sonic.Marshal(resp)
		return &cProtocol.ProtocolResponse{
			Status: 404,
			Data:   respData,
			Error:  "transaction not found",
		}, nil
	}

	status := "unknown"
	switch txn.Status {
	case transaction.TxnPending:
		status = "pending"
	case transaction.TxnCommitted:
		status = "committed"
	case transaction.TxnAborted:
		status = "aborted"
	case transaction.TxnPreparing:
		status = "preparing"
	case transaction.TxnPrepared:
		status = "prepared"
	}

	resp := TransactionResponse{
		TransactionID: req.TransactionID,
		Status:        status,
		Metadata: map[string]interface{}{
			"start_time":   txn.StartTime,
			"participants": txn.Participants,
			"type":         txn.Type,
		},
	}
	respData, _ := sonic.Marshal(resp)

	return &cProtocol.ProtocolResponse{
		Status: 200,
		Data:   respData,
	}, nil
}

// executeTransactionCommand 执行事务命令
func (e *Engine) executeTransactionCommand(ctx context.Context, txnID string, cmd TransactionCommand) TransactionResult {
	// 获取数据库名，默认为"default"
	database := cmd.Database
	if database == "" {
		database = "default"
	}

	switch cmd.Type {
	case "get":
		// 创建GET操作
		op := &transaction.TransactionOp{
			Type:     transaction.OpGet,
			Key:      cmd.Key,
			Value:    nil,
			Database: database,
		}

		err := e.transactionMgr.AddOperation(txnID, op)
		if err != nil {
			return TransactionResult{
				Key:     cmd.Key,
				Success: false,
				Error:   err.Error(),
			}
		}

		// 使用事务管理器的GetTransactionValueWithDatabase方法
		value, found, err := e.transactionMgr.GetTransactionValueWithDatabase(txnID, cmd.Database, cmd.Key)
		if err != nil {
			return TransactionResult{
				Key:     cmd.Key,
				Found:   false,
				Success: false,
				Error:   err.Error(),
			}
		}

		return TransactionResult{
			Key:     cmd.Key,
			Value:   value,
			Found:   found,
			Success: true,
		}
	case "set":
		// 创建SET操作
		op := &transaction.TransactionOp{
			Type:     transaction.OpSet,
			Key:      cmd.Key,
			Value:    cmd.Value,
			Database: database,
		}

		err := e.transactionMgr.AddOperation(txnID, op)
		if err != nil {
			return TransactionResult{
				Key:     cmd.Key,
				Success: false,
				Error:   err.Error(),
			}
		}

		return TransactionResult{
			Key:     cmd.Key,
			Success: true,
		}

	case "delete":
		// 创建DELETE操作
		op := &transaction.TransactionOp{
			Type:     transaction.OpDelete,
			Key:      cmd.Key,
			Value:    nil,
			Database: database,
		}

		err := e.transactionMgr.AddOperation(txnID, op)
		if err != nil {
			return TransactionResult{
				Key:     cmd.Key,
				Success: false,
				Error:   err.Error(),
			}
		}

		return TransactionResult{
			Key:     cmd.Key,
			Success: true,
		}

	default:
		return TransactionResult{
			Key:     cmd.Key,
			Success: false,
			Error:   "unknown command type: " + cmd.Type,
		}
	}
}
