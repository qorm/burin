# Docker 快速参考

## 🚀 快速启动

```bash
cd docker
make build      # 构建镜像
make up-d       # 启动集群
make status     # 查看状态
```

## 📋 常用命令

| 命令 | 说明 |
|------|------|
| `make build` | 构建镜像 |
| `make up-d` | 后台启动集群 |
| `make down` | 停止并移除集群 |
| `make restart` | 重启集群 |
| `make status` | 查看状态 |
| `make logs` | 查看日志 |
| `make logs-f` | 实时查看日志 |
| `make clean` | 清理所有数据 |
| `make dev-up` | 启动单节点开发环境 |

## 🔍 查看日志

```bash
# 所有节点
make logs

# 实时跟踪
make logs-f

# 特定节点
docker compose logs burin-node1
docker compose logs -f burin-node2
```

## 🔧 进入容器

```bash
docker compose exec burin-node1 /bin/bash
```

## 🌐 端口

| 节点 | 客户端端口 | Raft端口 |
|------|-----------|----------|
| node1 | 8099 | 8300 |
| node2 | 8090 | 8310 |
| node3 | 8199 | 8320 |

## 🧪 测试连接

```bash
# 使用 CLI 连接
./build/burin-cli-darwin-arm64
> connect localhost:8099
> login admin burin2025
> set test "hello"
> get test
```

## 🐛 故障排查

```bash
# 查看容器状态
docker compose ps

# 查看详细日志
docker compose logs burin-node1

# 重启特定节点
docker compose restart burin-node1

# 完全重置
make clean
make build
make up-d
```
