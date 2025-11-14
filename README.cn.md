# Burin

[English](README.md) | 简体中文

<p align="center">
  <strong>高性能分布式缓存系统</strong>
</p>

<p align="center">
  <a href="#特性">特性</a> •
  <a href="#快速开始">快速开始</a> •
  <a href="#架构">架构</a> •
  <a href="#使用示例">使用示例</a> •
  <a href="#配置">配置</a> •
  <a href="#构建">构建</a>
</p>

---

## 简介

Burin 是一个高性能的分布式缓存系统，基于 Raft 共识算法实现强一致性。它提供了丰富的数据结构支持、事务处理、地理位置查询等功能，适用于需要高可用性和数据一致性的场景。

## 特性

### 核心特性
- ✅ **分布式一致性**: 基于 Raft 共识算法，保证数据强一致性
- ✅ **多种数据结构**: 支持 String、Hash、List、Set、Sorted Set 等
- ✅ **事务支持**: 提供 ACID 事务处理能力
- ✅ **地理位置查询**: 内置 GeoHash 支持，实现高效的地理位置搜索
- ✅ **TTL 管理**: 灵活的过期时间控制
- ✅ **多数据库**: 支持多个逻辑数据库隔离
- ✅ **认证授权**: 完整的用户认证和权限管理
- ✅ **批量操作**: 高效的批量读写接口

### 高级特性
- 🚀 **高性能**: 使用 BadgerDB 作为存储引擎，优化的序列化协议
- 🔄 **故障转移**: 自动节点故障检测和恢复
- 📊 **监控指标**: 内置 Prometheus 指标支持
- 🔧 **灵活配置**: 支持 YAML 配置文件
- 🛠️ **命令行工具**: 功能完善的 CLI 和交互式客户端
- 📦 **连接池**: 高效的连接池管理

## 快速开始

### 方式一：使用 Docker（推荐）

**前置要求**: Docker 20.10+ 和 Docker Compose V2

```bash
# 克隆仓库
git clone https://github.com/qorm/burin.git
cd burin/docker

# 构建并启动集群
make build
make up-d

# 查看状态
make status

# 查看日志
make logs
```

更多 Docker 部署信息请参考 [Docker 部署指南](./docker/README.md)。

### 方式二：本地编译运行

**前置要求**: Go 1.24.0 或更高版本

```bash
# 克隆仓库
git clone https://github.com/qorm/burin.git
cd burin

# 构建服务器
./build.sh

# 构建 CLI 工具
./build-cli.sh
```

### 启动服务器

```bash
# 生成默认配置文件
./build/burin-darwin-arm64 -generate-config

# 启动节点
./build/burin-darwin-arm64 -config burin.yaml
```

### 使用 CLI 连接

```bash
# 启动交互式客户端
./build/burin-cli-darwin-arm64

# 连接到服务器
connect 127.0.0.1:8099

# 登录（默认用户名：burin，密码：burin@secret）
login burin burin@secret

# 基本操作
set mykey "Hello Burin"
get mykey
del mykey
```

## 架构

### 系统架构

```
┌─────────────────────────────────────────────────────────┐
│                    Client Applications                   │
└───────────────────┬─────────────────────────────────────┘
                    │
        ┌───────────┴──────────┐
        │   Burin Client SDK   │
        └───────────┬──────────┘
                    │
┌───────────────────┴─────────────────────────────────────┐
│                    Burin Cluster                         │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐             │
│  │  Node 1  │  │  Node 2  │  │  Node 3  │             │
│  │ (Leader) │◄─┤(Follower)│◄─┤(Follower)│             │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘             │
│       │             │             │                     │
│  ┌────▼─────────────▼─────────────▼─────┐              │
│  │         Raft Consensus Layer         │              │
│  └────┬─────────────┬─────────────┬─────┘              │
│       │             │             │                     │
│  ┌────▼────┐   ┌────▼────┐   ┌────▼────┐              │
│  │ BadgerDB│   │ BadgerDB│   │ BadgerDB│              │
│  └─────────┘   └─────────┘   └─────────┘              │
└─────────────────────────────────────────────────────────┘
```

### 核心组件

- **cProtocol**: 自定义的二进制协议，支持高效的客户端-服务器通信
- **Consensus**: 基于 Raft 的共识层，保证数据一致性
- **Store**: BadgerDB 存储引擎封装，提供持久化能力
- **Transaction**: MVCC 事务管理器
- **Business**: 业务逻辑层，处理各种数据操作
- **Client**: Go 客户端 SDK，提供简洁的 API

## 使用示例

### 基本缓存操作

```go
package main

import (
    "context"
    "fmt"
    "time"
    
    "burin/client"
)

func main() {
    // 创建配置
    config := client.NewDefaultConfig()
    config.Connection.Endpoint = "localhost:8099"
    config.Auth.Username = "burin"
    config.Auth.Password = "burin@secret"
    
    // 创建客户端
    burinClient, err := client.NewClient(config)
    if err != nil {
        panic(err)
    }
    
    // 连接并登录
    if err := burinClient.Connect(); err != nil {
        panic(err)
    }
    defer burinClient.Disconnect()
    
    // 设置缓存（带过期时间）
    err = burinClient.Set("user:1001", []byte(`{"name":"Alice","age":25}`), 
        client.WithTTL(5*time.Minute))
    if err != nil {
        panic(err)
    }
    
    // 获取缓存
    resp, err := burinClient.Get("user:1001")
    if err != nil {
        panic(err)
    }
    
    if resp.Found {
        fmt.Printf("Value: %s\n", string(resp.Value))
    }
    
    // 批量操作
    keys := []string{"key1", "key2", "key3"}
    values, err := burinClient.MGet(keys...)
    if err != nil {
        panic(err)
    }
    
    for key, value := range values {
        fmt.Printf("%s: %s\n", key, string(value))
    }
}
```

### 事务操作

```go
// 开始事务
txn, err := burinClient.BeginTransaction()
if err != nil {
    panic(err)
}

// 事务内操作
txn.Set("account:1", []byte("1000"))
txn.Set("account:2", []byte("2000"))

// 提交事务
err = burinClient.CommitTransaction(txn.ID)
if err != nil {
    // 回滚事务
    burinClient.RollbackTransaction(txn.ID)
    panic(err)
}
```

### 地理位置查询

```go
// 添加地理位置
err = burinClient.GeoAdd("locations", map[string]client.GeoPoint{
    "store1": {Latitude: 39.9042, Longitude: 116.4074}, // 北京
    "store2": {Latitude: 31.2304, Longitude: 121.4737}, // 上海
})

// 查询附近的位置（5公里范围内）
nearby, err := burinClient.GeoRadius("locations", 
    39.9042, 116.4074, 5.0, "km")
if err != nil {
    panic(err)
}

for _, loc := range nearby {
    fmt.Printf("Location: %s, Distance: %.2f km\n", 
        loc.Member, loc.Distance)
}
```

### Hash 操作

```go
// 设置 Hash 字段
err = burinClient.HSet("user:1001", "name", []byte("Alice"))
err = burinClient.HSet("user:1001", "age", []byte("25"))

// 获取 Hash 字段
value, err := burinClient.HGet("user:1001", "name")

// 获取所有字段
fields, err := burinClient.HGetAll("user:1001")
for field, value := range fields {
    fmt.Printf("%s: %s\n", field, string(value))
}
```

## 配置

### 服务器配置示例

```yaml
# 应用配置
app:
  name: "Burin"
  version: "1.0.0"
  environment: "production"
  node_id: "node1"
  data_dir: "./data"
  default_database: "default"

# 日志配置
log:
  level: "info"
  format: "json"
  output: "file"
  file: "./logs/burin.log"

# 缓存配置
cache:
  max_databases: 16
  default_ttl: "1h"
  max_value_size: 1048576  # 1MB
  enable_compression: true

# 共识配置
consensus:
  node_id: "node1"
  bind_addr: "127.0.0.1:8001"
  data_dir: "./data/raft"
  bootstrap: true
  join_addresses: []

# 事务配置
transaction:
  max_concurrent_transactions: 1000
  transaction_timeout: "30s"
  isolation_level: "read_committed"

# Burin 服务器配置
burin:
  bind_address: "0.0.0.0:8099"
  max_connections: 1000
  read_timeout: "30s"
  write_timeout: "30s"
  enable_auth: true
```

### 客户端配置

```go
config := client.NewDefaultConfig()

// 连接配置
config.Connection.Endpoints = []string{
    "node1:8099",
    "node2:8099",
    "node3:8099",
}
config.Connection.DialTimeout = 5 * time.Second
config.Connection.RequestTimeout = 10 * time.Second

// 重试配置
config.Retry.MaxAttempts = 3
config.Retry.InitialBackoff = 100 * time.Millisecond
config.Retry.MaxBackoff = 5 * time.Second

// 连接池配置
config.Pool.MaxSize = 100
config.Pool.MinSize = 10
config.Pool.IdleTimeout = 5 * time.Minute

// 认证配置
config.Auth.Username = "burin"
config.Auth.Password = "burin@secret"
```

## 构建

### 构建所有平台版本

```bash
# 构建服务器（所有平台）
./build.sh

# 构建 CLI 工具（所有平台）
./build-cli.sh
```

### 构建特定平台

```bash
# macOS ARM64
GOOS=darwin GOARCH=arm64 go build -o build/burin-darwin-arm64 main.go

# Linux AMD64
GOOS=linux GOARCH=amd64 go build -o build/burin-linux-amd64 main.go
```

### 运行测试

```bash
# 运行单元测试
go test ./...

# 运行集成测试
cd test_suite
go run main.go
```

## 集群部署

### Docker 部署（推荐）

使用 Docker Compose 快速部署三节点集群：

```bash
cd docker

# 构建镜像
make build

# 启动集群
make up-d

# 查看状态
make status
```

详细说明请参考 [Docker 部署指南](./docker/README.md)。

### 本地部署

启动三节点集群：

```bash
# 节点 1 (Leader - Bootstrap)
./build/burin-darwin-arm64 -config build/burin-node1.yaml

# 节点 2 (Follower)
./build/burin-darwin-arm64 -config build/burin-node2.yaml

# 节点 3 (Follower)
./build/burin-darwin-arm64 -config build/burin-node3.yaml
```

或使用管理脚本：

```bash
# 启动所有节点
./start.sh start

# 查看状态
./start.sh status

# 停止所有节点
./start.sh stop
```

### 节点配置要点

每个节点需要配置：
- 唯一的 `node_id`
- 不同的 `bind_addr`（Raft 通信地址）
- 不同的 `burin.bind_address`（客户端服务地址）
- 第一个节点设置 `bootstrap: true`
- 其他节点配置 `join_addresses` 指向 Leader

## 性能特点

- **高吞吐量**: 单节点支持数万 QPS
- **低延迟**: 平均响应时间 < 1ms（本地网络）
- **内存高效**: 使用 BadgerDB LSM 树结构，内存占用低
- **并发友好**: 支持大量并发连接和事务

## 监控和运维

### 健康检查

```bash
# CLI 健康检查
health

# 集群状态
cluster-status
```

### Prometheus 指标

Burin 暴露以下指标（如果启用）：
- 请求计数和延迟
- 缓存命中率
- 事务成功/失败率
- 连接池状态
- Raft 集群状态

## 目录结构

```
burin/
├── main.go              # 服务器入口
├── go.mod               # Go 模块定义
├── build.sh             # 服务器构建脚本
├── build-cli.sh         # CLI 构建脚本
├── auth/                # 认证授权模块
├── business/            # 业务逻辑层
├── cid/                 # 集群ID管理
├── cli/                 # CLI 工具
├── client/              # Go 客户端 SDK
├── config/              # 配置管理
├── consensus/           # Raft 共识层
├── cProtocol/           # 通信协议
├── examples/            # 使用示例
├── store/               # 存储引擎
├── transaction/         # 事务管理
└── test_suite/          # 集成测试套件
```

## 贡献

欢迎提交 Issue 和 Pull Request！

## 许可证

本项目采用 MIT 许可证。

## 相关文档

- [客户端使用指南](./client/README.md)
- [连接池文档](./client/POOL.md)
- [事务文档](./client/TRANSACTION.md)
- [迁移清单](./client/MIGRATION_CHECKLIST.md)

## 联系方式

如有问题或建议，请通过 Issue 与我们联系。

## 依赖库

Burin 项目基于以下优秀的开源库构建：

### 核心依赖

| 库名 | 版本 | 用途 |
|------|------|------|
| [BadgerDB](https://github.com/dgraph-io/badger) | v4.8.0 | 高性能 KV 存储引擎，LSM 树结构 |
| [HashiCorp Raft](https://github.com/hashicorp/raft) | v1.5.0 | 分布式共识算法实现 |
| [raft-boltdb](https://github.com/hashicorp/raft-boltdb) | latest | Raft 日志存储后端 |
| [Logrus](https://github.com/sirupsen/logrus) | v1.9.3 | 结构化日志库 |

### 工具库

| 库名 | 版本 | 用途 |
|------|------|------|
| [Sonic](https://github.com/bytedance/sonic) | v1.14.2 | 高性能 JSON 序列化/反序列化 |
| [Viper](https://github.com/spf13/viper) | v1.21.0 | 配置文件管理 |
| [Cobra](https://github.com/spf13/cobra) | v1.9.1 | CLI 命令行工具框架 |
| [Readline](https://github.com/chzyer/readline) | v1.5.1 | 交互式命令行支持 |
| [Prometheus Client](https://github.com/prometheus/client_golang) | v1.4.0 | 监控指标导出 |

### 特别感谢

- **BadgerDB**: 提供了高性能、低延迟的 LSM 树存储引擎
- **HashiCorp Raft**: 成熟稳定的 Raft 共识算法实现
- **Sonic**: 字节跳动开源的超高性能 JSON 库
- **Viper & Cobra**: 让配置管理和 CLI 开发变得简单

---

<p align="center">
  Made with ❤️ by Burin Team
</p>
