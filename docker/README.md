# Burin Docker 部署指南

本目录包含 Burin 集群的 Docker 部署配置文件。

## 📁 目录结构

```
docker/
├── Dockerfile              # Docker 镜像构建文件
├── docker-compose.yml      # Docker Compose 编排文件
├── manage.sh              # 集群管理脚本
├── .dockerignore          # Docker 构建排除文件
├── config/                # 配置文件目录
│   ├── node1.yaml        # 节点1配置（Bootstrap Leader）
│   ├── node2.yaml        # 节点2配置（Follower）
│   └── node3.yaml        # 节点3配置（Follower）
└── README.md             # 本文件
```

## 🚀 快速开始

### 前置要求

- Docker 20.10+
- Docker Compose V2

### 1. 构建镜像

```bash
cd docker
./manage.sh build
```

### 2. 启动集群

```bash
# 后台启动
./manage.sh up-d

# 前台启动（查看实时日志）
./manage.sh up
```

### 3. 检查状态

```bash
./manage.sh status
```

### 4. 查看日志

```bash
# 查看所有节点日志
./manage.sh logs

# 查看特定节点日志
./manage.sh logs burin-node1

# 实时查看日志
./manage.sh logs-f burin-node1
```

## 🔧 管理命令

### 启动和停止

```bash
# 后台启动集群
./manage.sh up-d

# 前台启动集群
./manage.sh up

# 停止集群（保留数据）
./manage.sh stop

# 启动已停止的集群
./manage.sh start

# 重启集群
./manage.sh restart

# 停止并移除集群
./manage.sh down
```

### 日志和调试

```bash
# 查看集群状态
./manage.sh status

# 查看所有日志
./manage.sh logs

# 查看特定节点日志
./manage.sh logs burin-node1

# 实时跟踪日志
./manage.sh logs-f

# 查看运行中的容器
./manage.sh ps
```

### 容器操作

```bash
# 进入节点容器
./manage.sh exec burin-node1

# 在容器内执行命令
docker compose exec burin-node1 /app/burin --version
```

### 清理

```bash
# 清理所有数据（包括容器和卷）
./manage.sh clean
```

## 🌐 网络配置

集群使用自定义桥接网络：

- **网络**: `burin-cluster`
- **子网**: `172.20.0.0/16`

节点 IP 地址：
- `burin-node1`: 172.20.0.11
- `burin-node2`: 172.20.0.12
- `burin-node3`: 172.20.0.13

## 🔌 端口映射

### 节点 1 (Bootstrap Leader)
- 客户端端口: `8099` → `8099`
- Raft 端口: `8300` → `8300`

### 节点 2 (Follower)
- 客户端端口: `8090` → `8090`
- Raft 端口: `8310` → `8310`

### 节点 3 (Follower)
- 客户端端口: `8199` → `8199`
- Raft 端口: `8320` → `8320`

## 💾 数据持久化

数据通过 Docker 卷持久化存储：

- `burin-node1-data`: 节点1数据
- `burin-node1-logs`: 节点1日志
- `burin-node2-data`: 节点2数据
- `burin-node2-logs`: 节点2日志
- `burin-node3-data`: 节点3数据
- `burin-node3-logs`: 节点3日志

### 查看卷

```bash
docker volume ls | grep burin
```

### 删除卷

```bash
docker compose down -v
```

## 🔍 健康检查

每个节点配置了健康检查：

- **间隔**: 10秒
- **超时**: 5秒
- **重试**: 5次
- **启动等待**: 10-15秒

检查节点健康状态：

```bash
docker compose ps
```

## 🔧 自定义配置

### 修改配置文件

编辑 `config/node*.yaml` 文件来自定义配置：

```bash
vim config/node1.yaml
```

修改后需要重启集群：

```bash
./manage.sh restart
```

### 环境变量

在 `docker-compose.yml` 中可以设置环境变量：

```yaml
environment:
  - NODE_ID=node-01
  - LOG_LEVEL=debug
```

## 📊 监控

### 查看容器资源使用

```bash
docker stats burin-node1 burin-node2 burin-node3
```

### 查看容器日志

```bash
# 最近100行
docker compose logs --tail=100

# 实时跟踪
docker compose logs -f --tail=50
```

## 🐛 故障排查

### 节点无法启动

1. 检查日志：
```bash
./manage.sh logs burin-node1
```

2. 检查配置文件：
```bash
cat config/node1.yaml
```

3. 检查端口占用：
```bash
netstat -an | grep 8099
```

### 集群无法形成

1. 确保 node1 先启动（Bootstrap）
2. 检查网络连接：
```bash
docker compose exec burin-node2 ping burin-node1
```

3. 查看 Raft 日志

### 数据问题

清理所有数据重新开始：
```bash
./manage.sh clean
./manage.sh build
./manage.sh up-d
```

## 🧪 测试连接

使用 CLI 工具连接到集群：

```bash
# 从宿主机连接
./build/burin-cli-darwin-arm64
> connect localhost:8099
> login admin burin2025
> set test "Hello Docker"
> get test
```

## 🔐 安全建议

1. **修改默认密码**: 在生产环境中更改默认的认证密码
2. **使用 TLS**: 配置 TLS 证书保护通信
3. **网络隔离**: 使用防火墙限制访问
4. **定期备份**: 定期备份数据卷

## 📝 生产环境建议

1. **资源限制**: 在 docker-compose.yml 中添加资源限制
```yaml
deploy:
  resources:
    limits:
      cpus: '2'
      memory: 4G
    reservations:
      cpus: '1'
      memory: 2G
```

2. **日志轮转**: 配置日志大小限制
```yaml
logging:
  driver: "json-file"
  options:
    max-size: "100m"
    max-file: "3"
```

3. **监控告警**: 集成 Prometheus 和 Grafana

4. **自动重启**: 已配置 `restart: unless-stopped`

## 🆘 获取帮助

```bash
./manage.sh help
```

查看所有可用命令和使用示例。

## 📚 相关文档

- [主 README](../README.md)
- [客户端文档](../client/README.md)
- [配置说明](../config/config.go)

---

如有问题，请提交 Issue 或查看项目文档。
