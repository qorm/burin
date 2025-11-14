# Burin Docker 部署 Makefile

.PHONY: help build up up-d down stop start restart status logs logs-f clean ps dev-up dev-down

# 默认目标
.DEFAULT_GOAL := help

# 帮助信息
help:
	@echo "Burin Docker 部署命令"
	@echo ""
	@echo "集群部署 (3节点):"
	@echo "  make build      - 构建 Docker 镜像"
	@echo "  make up         - 启动集群（前台）"
	@echo "  make up-d       - 启动集群（后台）"
	@echo "  make down       - 停止并移除集群"
	@echo "  make stop       - 停止集群"
	@echo "  make start      - 启动已停止的集群"
	@echo "  make restart    - 重启集群"
	@echo "  make status     - 查看集群状态"
	@echo "  make logs       - 查看日志"
	@echo "  make logs-f     - 实时查看日志"
	@echo "  make clean      - 清理所有数据"
	@echo "  make ps         - 查看容器"
	@echo ""
	@echo "开发环境 (单节点):"
	@echo "  make dev-up     - 启动开发环境"
	@echo "  make dev-down   - 停止开发环境"
	@echo "  make dev-logs   - 查看开发环境日志"

# 构建镜像
build:
	@echo "构建 Burin Docker 镜像..."
	docker compose build

# 启动集群（前台）
up:
	@echo "启动 Burin 集群（前台）..."
	docker compose up

# 启动集群（后台）
up-d:
	@echo "启动 Burin 集群（后台）..."
	docker compose up -d
	@echo "✓ 集群已启动"
	@make status

# 停止并移除集群
down:
	@echo "停止并移除 Burin 集群..."
	docker compose down
	@echo "✓ 集群已停止"

# 停止集群
stop:
	@echo "停止 Burin 集群..."
	docker compose stop
	@echo "✓ 集群已停止"

# 启动已停止的集群
start:
	@echo "启动已停止的集群..."
	docker compose start
	@echo "✓ 集群已启动"
	@make status

# 重启集群
restart:
	@echo "重启 Burin 集群..."
	docker compose restart
	@echo "✓ 集群已重启"
	@make status

# 查看状态
status:
	@echo "Burin 集群状态:"
	@docker compose ps

# 查看日志
logs:
	docker compose logs

# 实时查看日志
logs-f:
	docker compose logs -f

# 清理所有数据
clean:
	@echo "清理所有数据..."
	docker compose down -v
	docker system prune -f
	@echo "✓ 清理完成"

# 查看容器
ps:
	docker compose ps

# 开发环境 - 启动单节点
dev-up:
	@echo "启动开发环境（单节点）..."
	docker compose -f docker-compose.dev.yml up -d
	@echo "✓ 开发环境已启动"
	@docker compose -f docker-compose.dev.yml ps

# 开发环境 - 停止
dev-down:
	@echo "停止开发环境..."
	docker compose -f docker-compose.dev.yml down
	@echo "✓ 开发环境已停止"

# 开发环境 - 查看日志
dev-logs:
	docker compose -f docker-compose.dev.yml logs -f

# 进入节点1
exec-node1:
	docker compose exec burin-node1 /bin/bash

# 进入节点2
exec-node2:
	docker compose exec burin-node2 /bin/bash

# 进入节点3
exec-node3:
	docker compose exec burin-node3 /bin/bash

# 查看节点1日志
logs-node1:
	docker compose logs -f burin-node1

# 查看节点2日志
logs-node2:
	docker compose logs -f burin-node2

# 查看节点3日志
logs-node3:
	docker compose logs -f burin-node3
