# Burin ACID 事务功能文档

## 概述

Burin 客户端现已支持完整的 ACID 事务功能，提供强一致性保证和隔离级别控制。

## 特性

### ✅ 完整的 ACID 属性

- **Atomicity (原子性)**: 事务中的所有操作要么全部成功，要么全部失败
- **Consistency (一致性)**: 事务前后数据保持一致性约束
- **Isolation (隔离性)**: 支持三种隔离级别控制并发访问
- **Durability (持久性)**: 提交的事务永久保存

### 📊 隔离级别

1. **ReadCommitted (读已提交)**
   - 避免脏读
   - 允许不可重复读和幻读
   - 性能最好

2. **RepeatableRead (可重复读)** - 默认
   - 避免脏读和不可重复读
   - 允许幻读
   - 平衡性能和一致性

3. **Serializable (串行化)**
   - 最高隔离级别
   - 完全避免并发问题
   - 性能相对较低

### 🔧 功能特性

- ✅ 事务内读写操作 (Get/Set/Delete)
- ✅ 读集/写集/删除集管理
- ✅ 事务提交和回滚
- ✅ 超时控制
- ✅ 并发事务数量限制
- ✅ 分布式事务支持 (2PC)
- ✅ 完整的错误处理

## 快速开始

### 基本使用

```go
package main

import (
    "context"
    "time"
    
    "burin/client"
    "burin/client/interfaces"
)

func main() {
    // 创建客户端
    config := client.NewDefaultConfig()
    config.Connection.Endpoints = []string{"127.0.0.1:8099"}
    
    burinClient, err := client.NewClient(config)
    if err != nil {
        panic(err)
    }
    
    ctx := context.Background()
    burinClient.Connect(ctx)
    defer burinClient.Disconnect()
    
    // 开始事务
    tx, err := burinClient.BeginTransaction(ctx)
    if err != nil {
        panic(err)
    }
    
    // 在事务中执行操作
    tx.Set(ctx, "key1", []byte("value1"))
    tx.Set(ctx, "key2", []byte("value2"))
    
    value, err := tx.Get(ctx, "key1")
    if err != nil {
        tx.Rollback(ctx)
        return
    }
    
    // 提交事务
    if err := tx.Commit(ctx); err != nil {
        panic(err)
    }
}
```

### 使用选项

```go
// 设置隔离级别和超时
tx, err := burinClient.BeginTransaction(ctx,
    interfaces.WithIsolationLevel(interfaces.Serializable),
    interfaces.WithTxTimeout(30*time.Second),
    interfaces.WithTxDatabase("mydb"),
)
```

### 分布式事务

```go
// 跨多个节点的事务
tx, err := burinClient.BeginTransaction(ctx,
    interfaces.WithParticipants([]string{"node1", "node2", "node3"}),
    interfaces.WithIsolationLevel(interfaces.RepeatableRead),
)
```

## API 参考

### 事务接口

#### BeginTransaction

开始一个新事务。

```go
func (c *BurinClient) BeginTransaction(
    ctx context.Context, 
    opts ...interfaces.TransactionOption,
) (interfaces.Transaction, error)
```

**选项**:
- `WithIsolationLevel(level)` - 设置隔离级别
- `WithTxTimeout(duration)` - 设置超时时间
- `WithTxDatabase(name)` - 设置数据库
- `WithParticipants(nodes)` - 设置参与节点（分布式事务）
- `WithReadOnly()` - 设置为只读事务

**返回**: Transaction 接口实例

#### Transaction.Get

在事务中读取数据。

```go
func (tx Transaction) Get(
    ctx context.Context, 
    key string,
) ([]byte, error)
```

**特性**:
- 自动记录到读集
- 支持可重复读
- 优先读取写集中的数据

#### Transaction.Set

在事务中写入数据。

```go
func (tx Transaction) Set(
    ctx context.Context, 
    key string, 
    value []byte,
) error
```

**特性**:
- 记录到写集
- 延迟写入（提交时才真正写入）
- 只读事务不允许写入

#### Transaction.Delete

在事务中删除数据。

```go
func (tx Transaction) Delete(
    ctx context.Context, 
    key string,
) error
```

**特性**:
- 记录到删除集
- 延迟删除（提交时才真正删除）
- 只读事务不允许删除

#### Transaction.Commit

提交事务。

```go
func (tx Transaction) Commit(ctx context.Context) error
```

**行为**:
1. 验证读集（检测冲突）
2. 应用写集和删除集
3. 提交到存储层
4. 释放锁和资源

**错误**:
- 如果验证失败，事务将被中止
- 返回具体的失败原因

#### Transaction.Rollback

回滚事务。

```go
func (tx Transaction) Rollback(ctx context.Context) error
```

**行为**:
1. 丢弃所有未提交的更改
2. 释放持有的锁
3. 清理事务资源

## 使用场景

### 1. 银行转账

```go
func Transfer(client *client.BurinClient, from, to string, amount int) error {
    ctx := context.Background()
    
    tx, err := client.BeginTransaction(ctx,
        interfaces.WithIsolationLevel(interfaces.Serializable),
    )
    if err != nil {
        return err
    }
    
    // 读取余额
    fromBalance, err := tx.Get(ctx, "account:"+from)
    if err != nil {
        tx.Rollback(ctx)
        return err
    }
    
    toBalance, err := tx.Get(ctx, "account:"+to)
    if err != nil {
        tx.Rollback(ctx)
        return err
    }
    
    // 计算新余额
    fromNew := parseBalance(fromBalance) - amount
    toNew := parseBalance(toBalance) + amount
    
    if fromNew < 0 {
        tx.Rollback(ctx)
        return errors.New("insufficient balance")
    }
    
    // 更新余额
    tx.Set(ctx, "account:"+from, formatBalance(fromNew))
    tx.Set(ctx, "account:"+to, formatBalance(toNew))
    
    // 提交事务
    return tx.Commit(ctx)
}
```

### 2. 库存扣减

```go
func DeductInventory(client *client.BurinClient, productID string, quantity int) error {
    ctx := context.Background()
    
    tx, err := client.BeginTransaction(ctx,
        interfaces.WithIsolationLevel(interfaces.RepeatableRead),
    )
    if err != nil {
        return err
    }
    
    // 读取当前库存
    inventoryData, err := tx.Get(ctx, "inventory:"+productID)
    if err != nil {
        tx.Rollback(ctx)
        return err
    }
    
    currentInventory := parseInventory(inventoryData)
    
    // 检查库存是否足够
    if currentInventory < quantity {
        tx.Rollback(ctx)
        return errors.New("insufficient inventory")
    }
    
    // 扣减库存
    newInventory := currentInventory - quantity
    tx.Set(ctx, "inventory:"+productID, formatInventory(newInventory))
    
    // 记录操作日志
    logKey := fmt.Sprintf("log:%s:%d", productID, time.Now().Unix())
    logValue := fmt.Sprintf("deducted %d, remaining %d", quantity, newInventory)
    tx.Set(ctx, logKey, []byte(logValue))
    
    return tx.Commit(ctx)
}
```

### 3. 批量更新配置

```go
func UpdateConfigs(client *client.BurinClient, configs map[string]string) error {
    ctx := context.Background()
    
    tx, err := client.BeginTransaction(ctx,
        interfaces.WithTxTimeout(10*time.Second),
    )
    if err != nil {
        return err
    }
    
    // 批量更新
    for key, value := range configs {
        if err := tx.Set(ctx, "config:"+key, []byte(value)); err != nil {
            tx.Rollback(ctx)
            return err
        }
    }
    
    // 更新版本号
    version := fmt.Sprintf("%d", time.Now().Unix())
    tx.Set(ctx, "config:version", []byte(version))
    
    return tx.Commit(ctx)
}
```

## 最佳实践

### 1. 合理选择隔离级别

- 普通读写: 使用 `RepeatableRead`（默认）
- 转账/扣库存: 使用 `Serializable`
- 只读查询: 使用 `ReadCommitted` + `WithReadOnly()`

### 2. 设置合理的超时时间

```go
// 短操作
tx, _ := client.BeginTransaction(ctx, 
    interfaces.WithTxTimeout(5*time.Second))

// 长操作
tx, _ := client.BeginTransaction(ctx, 
    interfaces.WithTxTimeout(30*time.Second))
```

### 3. 及时提交或回滚

```go
tx, err := client.BeginTransaction(ctx)
if err != nil {
    return err
}
defer func() {
    if r := recover(); r != nil {
        tx.Rollback(ctx)
        panic(r)
    }
}()

// ... 操作 ...

return tx.Commit(ctx)
```

### 4. 处理冲突

```go
maxRetries := 3
for i := 0; i < maxRetries; i++ {
    tx, err := client.BeginTransaction(ctx)
    if err != nil {
        return err
    }
    
    // ... 操作 ...
    
    err = tx.Commit(ctx)
    if err == nil {
        return nil // 成功
    }
    
    if isConflictError(err) {
        time.Sleep(time.Millisecond * 100)
        continue // 重试
    }
    
    return err // 其他错误
}

return errors.New("max retries exceeded")
```

## 性能考虑

### 1. 事务大小

- ✅ 推荐: 10-100 个操作
- ⚠️ 注意: 100-1000 个操作
- ❌ 避免: 1000+ 个操作

### 2. 事务时长

- ✅ 推荐: < 1秒
- ⚠️ 注意: 1-5秒
- ❌ 避免: > 5秒

### 3. 锁竞争

- 减少事务持有锁的时间
- 使用更低的隔离级别
- 设计无锁或少锁的数据结构

## 注意事项

1. **事务不支持嵌套**: 一个事务内不能开始另一个事务
2. **网络分区**: 分布式事务在网络分区时可能失败
3. **资源限制**: 默认最多 100 个并发事务
4. **状态检查**: 使用 `tx.Status()` 检查事务状态

## 故障处理

### 超时处理

```go
tx, _ := client.BeginTransaction(ctx, 
    interfaces.WithTxTimeout(5*time.Second))

// 监听上下文取消
select {
case <-tx.Context().Done():
    // 事务已超时，自动回滚
    return tx.Context().Err()
default:
    // 继续执行
}
```

### 连接失败

```go
err := tx.Commit(ctx)
if err != nil {
    if isNetworkError(err) {
        // 重新连接并重试
        client.Reconnect()
        // ... 重试逻辑 ...
    }
}
```

## 监控和调试

### 查看事务状态

```go
status := tx.Status()
switch status {
case interfaces.TxStatusPending:
    // 事务进行中
case interfaces.TxStatusCommitted:
    // 已提交
case interfaces.TxStatusAborted:
    // 已回滚
}
```

### 获取事务ID

```go
txID := tx.ID()
log.Printf("Transaction started: %s", txID)
```

## 相关资料

- [事务示例代码](./examples/transaction_demo.go)
- [CLIENT_IMPROVEMENT_SUMMARY.md](../CLIENT_IMPROVEMENT_SUMMARY.md)
- [Burin 架构文档](../ai.txt)

## 更新日志

### 2025-11-10
- ✅ 实现完整的 ACID 事务客户端
- ✅ 支持三种隔离级别
- ✅ 实现读集/写集管理
- ✅ 添加超时和并发控制
- ✅ 创建完整的事务示例
