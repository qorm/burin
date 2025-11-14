# Burin 客户端连接池

## 概述

客户端连接池用于管理多个 `BurinClient` 实例，提供连接复用、自动健康检查和生命周期管理功能。

## 特性

- **连接复用**: 避免频繁创建/销毁连接的开销
- **并发安全**: 支持多个 goroutine 同时使用
- **自动健康检查**: 定期检查连接健康状态
- **生命周期管理**: 自动回收过期和不健康的连接
- **灵活配置**: 可配置最小/最大连接数、超时时间等

## 快速开始

### 创建连接池

```go
import (
    "time"
    "burin/client"
)

config := client.NewDefaultConfig()
config.Connection.Endpoint = "localhost:8520"
config.Auth.Username = "admin"
config.Auth.Password = "admin123"

pool, err := client.NewClientPool(
    config,
    10,             // 最大连接数
    2,              // 最小连接数（预创建）
    5*time.Minute,  // 空闲超时
    30*time.Minute, // 最大生命周期
)
if err != nil {
    log.Fatal(err)
}
defer pool.Close()
```

### 使用连接池

```go
// 获取客户端
pc, err := pool.Get()
if err != nil {
    log.Fatal(err)
}
defer pc.Close() // 归还到池中

// 获取底层客户端并使用
c := pc.Get()
err = c.Set("key", []byte("value"))
if err != nil {
    log.Fatal(err)
}
```

### 并发使用

```go
var wg sync.WaitGroup

for i := 0; i < 10; i++ {
    wg.Add(1)
    go func(id int) {
        defer wg.Done()
        
        pc, err := pool.Get()
        if err != nil {
            log.Printf("Failed to get client: %v", err)
            return
        }
        defer pc.Close()
        
        c := pc.Get()
        // 执行操作...
        c.Set(fmt.Sprintf("key_%d", id), []byte("value"))
    }(i)
}

wg.Wait()
```

## 配置参数

### NewClientPool 参数

| 参数 | 类型 | 说明 |
|-----|------|------|
| config | *ClientConfig | 客户端配置 |
| maxSize | int | 最大连接数（默认10） |
| minSize | int | 最小连接数（默认0） |
| idleTimeout | time.Duration | 空闲超时时间（默认5分钟） |
| maxLifetime | time.Duration | 最大生命周期（默认30分钟） |

### 连接池行为

- **最小连接数**: 池初始化时会预创建 `minSize` 个连接，并在清理时保持这个数量
- **最大连接数**: 池中最多同时存在 `maxSize` 个连接，超过此限制会返回 `ErrPoolEmpty`
- **空闲超时**: 连接空闲超过 `idleTimeout` 会被回收（但会保持最小连接数）
- **最大生命周期**: 连接存活超过 `maxLifetime` 会被强制回收

## API 文档

### ClientPool

```go
type ClientPool struct {
    // 内部字段...
}
```

#### 方法

##### Get() (*PooledClient, error)

从池中获取一个客户端连接。如果池中没有可用连接且未达到最大连接数，会创建新连接。

返回值:
- `*PooledClient`: 池化的客户端连接
- `error`: 错误信息，可能返回 `ErrPoolEmpty` 或 `ErrPoolClosed`

##### Close() error

关闭连接池，释放所有连接。

##### Stats() map[string]interface{}

获取连接池统计信息。

返回字段:
- `max_size`: 最大连接数
- `min_size`: 最小连接数
- `current_size`: 当前连接总数
- `available`: 池中可用连接数
- `in_use`: 正在使用的连接数
- `idle_timeout`: 空闲超时时间
- `max_lifetime`: 最大生命周期
- `closed`: 是否已关闭

### PooledClient

```go
type PooledClient struct {
    // 内部字段...
}
```

#### 方法

##### Get() *BurinClient

获取底层的 `BurinClient` 实例。

##### Close() error

将连接归还到池中。这不会真正关闭连接，而是将其放回池中供其他goroutine使用。

##### IsHealthy() bool

检查客户端连接是否健康。

## 最佳实践

### 1. 使用 defer 归还连接

```go
pc, err := pool.Get()
if err != nil {
    return err
}
defer pc.Close() // 确保连接被归还

// 使用连接...
```

### 2. 合理设置连接池大小

- **最小连接数**: 根据应用的基准负载设置，避免冷启动延迟
- **最大连接数**: 根据峰值负载和服务器容量设置，避免资源耗尽

示例：
```go
// 低负载应用
pool, _ := client.NewClientPool(config, 5, 1, ...)

// 高负载应用
pool, _ := client.NewClientPool(config, 100, 10, ...)
```

### 3. 监控连接池状态

定期检查连接池统计信息，用于监控和调优：

```go
ticker := time.NewTicker(1 * time.Minute)
defer ticker.Stop()

for range ticker.C {
    stats := pool.Stats()
    log.Printf("Pool stats: current=%d, available=%d, in_use=%d",
        stats["current_size"], stats["available"], stats["in_use"])
}
```

### 4. 处理 ErrPoolEmpty

当连接池已满时，`Get()` 会返回 `ErrPoolEmpty`。根据业务需求处理：

```go
pc, err := pool.Get()
if err == client.ErrPoolEmpty {
    // 选项1: 等待一段时间后重试
    time.Sleep(100 * time.Millisecond)
    pc, err = pool.Get()
    
    // 选项2: 返回错误给调用者
    // return fmt.Errorf("too many concurrent requests")
}
```

### 5. 避免长时间持有连接

尽快归还连接到池中，让其他 goroutine 可以使用：

```go
// 不推荐: 长时间持有连接
pc, _ := pool.Get()
doLongRunningTask() // 可能需要几分钟
pc.Close()

// 推荐: 只在需要时获取连接
doPreparationWork()
pc, _ := pool.Get()
doQuickDatabaseOperation()
pc.Close()
doPostProcessing()
```

## 性能考虑

### 连接创建开销

创建新的 Burin 客户端连接包括：
1. TCP 连接建立
2. 协议握手
3. 身份认证（如果配置）

使用连接池可以避免这些开销，特别是在高并发场景下。

### 内存使用

每个连接会占用一定内存：
- 连接缓冲区
- 客户端状态
- 相关元数据

合理设置最大连接数，避免内存耗尽。

### 清理周期

连接池每30秒执行一次清理，检查过期连接。这个周期是固定的，可以根据需要修改源码。

## 错误处理

### ErrPoolEmpty

连接池已达到最大连接数且没有可用连接。

处理方法：
- 增加最大连接数
- 实现重试逻辑
- 返回服务不可用错误

### ErrPoolClosed

尝试从已关闭的连接池获取连接。

处理方法：
- 检查应用生命周期管理
- 确保在关闭池之前完成所有操作

## 示例程序

完整示例请参考 `examples/pool/main.go`：

```bash
cd examples/pool
go run main.go
```

## 与 cProtocol 连接池的区别

- **cProtocol 连接池**: 管理 TCP 连接，位于协议层
- **Client 连接池**: 管理完整的 BurinClient 实例，位于应用层

两者可以配合使用：
- cProtocol 连接池处理底层网络连接复用
- Client 连接池处理应用级别的客户端实例管理

通常建议：
- 单个应用实例：使用 Client 连接池
- 需要细粒度连接控制：使用 cProtocol 连接池
- 高性能场景：两者配合使用

## 注意事项

1. **线程安全**: `ClientPool` 是线程安全的，可以在多个 goroutine 中并发使用
2. **资源清理**: 务必在应用退出时调用 `pool.Close()` 释放资源
3. **健康检查开销**: `IsHealthy()` 会发送请求到服务器，频繁调用可能影响性能
4. **连接复用**: 归还的连接会被其他 goroutine 复用，确保使用完后立即归还
5. **配置一致性**: 池中所有客户端使用相同的配置，无法为单个连接定制配置
