# Burin Client 使用指南 - 简化版API

## 核心改进

**Context 内置化**: 不再需要每次调用都传递 `context.Context`，客户端内部维护上下文！

## 快速开始

### 基本使用（无需传递ctx）

```go
package main

import (
    "burin/client"
)

func main() {
    // 1. 创建配置
    config := client.NewDefaultConfig()
    config.Connection.Endpoints = []string{"localhost:8099"}
    
    // 2. 创建客户端
    burinClient, err := client.NewClient(config)
    if err != nil {
        panic(err)
    }
    
    // 3. 连接（无需ctx）
    if err := burinClient.Connect(); err != nil {
        panic(err)
    }
    defer burinClient.Disconnect()
    
    // 4. 使用缓存（无需ctx）
    burinClient.Set("key1", []byte("value1"))
    
    resp, err := burinClient.Get("key1")
    if err != nil {
        panic(err)
    }
    println(string(resp.Value))
    
    // 5. 批量操作（无需ctx）
    burinClient.MSet(map[string][]byte{
        "key2": []byte("value2"),
        "key3": []byte("value3"),
    })
    
    results, _ := burinClient.MGet([]string{"key1", "key2", "key3"})
    for key, resp := range results {
        println(key, "=", string(resp.Value))
    }
}
```

### 自定义Context（可选）

如果需要特殊的context（如超时、取消），可以使用 `WithContext()` 方法：

```go
import (
    "context"
    "time"
)

// 设置带超时的context
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

burinClient.WithContext(ctx)

// 后续操作都会使用这个context
burinClient.Set("key", []byte("value"))
burinClient.Get("key")
```

## API对比

### 旧API（需要传递ctx）

```go
ctx := context.Background()

// 每次都要传ctx
burinClient.Connect(ctx)
burinClient.Set(ctx, "key", []byte("value"))
resp, _ := burinClient.Get(ctx, "key")
burinClient.Delete(ctx, "key")
```

### 新API（ctx内置）

```go
// 无需传ctx，更简洁
burinClient.Connect()
burinClient.Set("key", []byte("value"))
resp, _ := burinClient.Get("key")
burinClient.Delete("key")
```

## 完整示例

### 1. 缓存操作

```go
// 设置值
burinClient.Set("user:1001", []byte(`{"name":"Alice"}`))

// 设置带TTL的值
burinClient.Set("session:abc", []byte("data"), 
    client.WithTTL(10*time.Minute))

// 设置到指定数据库
burinClient.Set("config:app", []byte("value"), 
    client.WithDatabase("production"))

// 获取值
resp, err := burinClient.Get("user:1001")
if err != nil {
    log.Fatal(err)
}
if resp.Found {
    fmt.Println(string(resp.Value))
}

// 检查存在
exists, _ := burinClient.Exists("user:1001")
fmt.Println("Exists:", exists)

// 删除
burinClient.Delete("user:1001")

// 批量操作
burinClient.MSet(map[string][]byte{
    "key1": []byte("value1"),
    "key2": []byte("value2"),
    "key3": []byte("value3"),
})

results, _ := burinClient.MGet([]string{"key1", "key2", "key3"})
```

### 2. 队列操作

```go
// 创建队列
burinClient.CreateQueue("myqueue", interfaces.QueueType("standard"))

// 发布消息
result, err := burinClient.Publish("myqueue", []byte("hello"))
if err != nil {
    log.Fatal(err)
}
fmt.Println("Message ID:", result.MessageID)

// 发布带优先级的消息
burinClient.Publish("myqueue", []byte("urgent"), 
    client.WithPriority(10))

// 消费消息
messages, err := burinClient.Consume("myqueue", 10)
for _, msg := range messages {
    fmt.Println("Received:", string(msg.Body))
}
```

### 3. 事务操作

```go
import "context"

// 事务仍需要context（用于超时控制）
ctx := context.Background()

// 开始事务
tx, err := burinClient.BeginTransaction(
    interfaces.WithIsolationLevel(interfaces.Serializable),
    interfaces.WithTxTimeout(30*time.Second),
)
if err != nil {
    log.Fatal(err)
}

// 事务内操作
tx.Set(ctx, "account:alice", []byte("1000"))
tx.Set(ctx, "account:bob", []byte("500"))

balance, _ := tx.Get(ctx, "account:alice")
fmt.Println("Balance:", string(balance))

// 提交
if err := tx.Commit(ctx); err != nil {
    tx.Rollback(ctx)
    log.Fatal(err)
}
```

## 高级用法

### 1. 动态切换Context

```go
// 默认context
burinClient.Set("key1", []byte("value1"))

// 切换到超时context
ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
defer cancel()

burinClient.WithContext(ctx)
burinClient.Set("key2", []byte("value2"))  // 使用超时context

// 恢复默认context
burinClient.WithContext(context.Background())
burinClient.Set("key3", []byte("value3"))  // 使用默认context
```

### 2. 链式调用

```go
client, _ := client.NewClient(config)
client.Connect()

// 可以链式设置context
client.WithContext(customCtx).
    Set("key", []byte("value"))
```

### 3. 获取当前Context

```go
currentCtx := burinClient.Context()
fmt.Println("Current context:", currentCtx)
```

## 迁移指南

### 从旧API迁移

只需简单删除所有 `ctx` 参数：

```bash
# 查找需要迁移的代码
grep -r "burinClient.Get(ctx," .
grep -r "burinClient.Set(ctx," .
grep -r "burinClient.Delete(ctx," .

# 批量替换（谨慎使用）
sed -i 's/burinClient\.Get(ctx, /burinClient.Get(/g' *.go
sed -i 's/burinClient\.Set(ctx, /burinClient.Set(/g' *.go
sed -i 's/burinClient\.Delete(ctx, /burinClient.Delete(/g' *.go
sed -i 's/\.Connect(ctx)/.Connect()/g' *.go
```

## 最佳实践

### 1. 默认使用简化API

```go
// ✅ 推荐：简洁清晰
burinClient.Set("key", []byte("value"))

// ❌ 不推荐：除非真的需要特殊context
ctx := context.Background()
burinClient.WithContext(ctx)
burinClient.Set("key", []byte("value"))
```

### 2. 特殊场景使用WithContext

```go
// ✅ 需要超时控制时
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
burinClient.WithContext(ctx).Set("key", []byte("value"))

// ✅ 需要取消操作时
ctx, cancel := context.WithCancel(context.Background())
go someWork(cancel)  // 可能会调用cancel
burinClient.WithContext(ctx).Get("key")
```

### 3. 事务保留Context

```go
// 事务操作仍然需要显式传递context
ctx := context.Background()
tx, _ := burinClient.BeginTransaction()
tx.Set(ctx, "key", []byte("value"))
tx.Commit(ctx)
```

## 性能说明

- **零额外开销**: Context内置不会影响性能
- **内存优化**: 单个context实例，减少重复创建
- **并发安全**: 可以通过WithContext安全地更新context

## 注意事项

1. **并发场景**: 如果多个goroutine共享同一个客户端并需要不同的context，每个goroutine应该调用`WithContext()`
2. **事务特殊性**: 事务操作仍需要显式传递context，用于精确控制事务超时
3. **向后兼容**: 子客户端（Cache()/Queue()）的接口仍然需要context参数

## 完整示例程序

```go
package main

import (
    "fmt"
    "log"
    "time"
    
    "burin/client"
    "burin/client/interfaces"
)

func main() {
    // 创建客户端
    config := client.NewDefaultConfig()
    config.Connection.Endpoints = []string{"localhost:8099"}
    
    c, err := client.NewClient(config)
    if err != nil {
        log.Fatal(err)
    }
    
    // 连接 - 无需ctx
    if err := c.Connect(); err != nil {
        log.Fatal(err)
    }
    defer c.Disconnect()
    
    fmt.Println("✓ 已连接到Burin服务器")
    
    // 基本操作 - 无需ctx
    c.Set("greeting", []byte("Hello, Burin!"))
    c.Set("user:1", []byte(`{"name":"Alice","age":25}`))
    
    // 带选项 - 无需ctx
    c.Set("session:abc", []byte("session_data"), 
        client.WithTTL(10*time.Minute),
        client.WithDatabase("sessions"))
    
    // 读取 - 无需ctx
    resp, err := c.Get("greeting")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("✓ 读取成功: %s\n", string(resp.Value))
    
    // 批量操作 - 无需ctx
    c.MSet(map[string][]byte{
        "key1": []byte("value1"),
        "key2": []byte("value2"),
        "key3": []byte("value3"),
    })
    
    results, _ := c.MGet([]string{"key1", "key2", "key3"})
    fmt.Printf("✓ 批量读取: %d 个键\n", len(results))
    
    // 队列操作 - 无需ctx
    c.CreateQueue("tasks", interfaces.QueueType("standard"))
    c.Publish("tasks", []byte("task1"))
    c.Publish("tasks", []byte("task2"))
    
    messages, _ := c.Consume("tasks", 10)
    fmt.Printf("✓ 消费消息: %d 条\n", len(messages))
    
    fmt.Println("✓ 所有操作完成")
}
```

## 总结

新的API设计遵循以下原则：

1. **简洁性**: 移除重复的ctx参数，代码更清晰
2. **灵活性**: 保留WithContext方法应对特殊需求
3. **一致性**: 所有基本操作API统一，易于记忆
4. **实用性**: 99%的场景不需要显式传递context

享受更简洁的Burin客户端API！🎉
