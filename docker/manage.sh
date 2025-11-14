#!/bin/bash

# Burin Docker 集群管理脚本
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 显示帮助信息
show_help() {
    echo -e "${BLUE}Burin Docker 集群管理脚本${NC}"
    echo ""
    echo "使用方法: $0 [命令]"
    echo ""
    echo "命令:"
    echo "  build          - 构建 Docker 镜像"
    echo "  up             - 启动集群（前台运行）"
    echo "  up-d           - 启动集群（后台运行）"
    echo "  down           - 停止并移除集群"
    echo "  stop           - 停止集群（保留容器）"
    echo "  start          - 启动已停止的集群"
    echo "  restart        - 重启集群"
    echo "  status         - 查看集群状态"
    echo "  logs [node]    - 查看日志（不指定节点则查看所有）"
    echo "  logs-f [node]  - 实时查看日志"
    echo "  exec <node>    - 进入节点容器"
    echo "  clean          - 清理所有数据（包括容器和卷）"
    echo "  ps             - 查看运行中的容器"
    echo "  help           - 显示此帮助信息"
    echo ""
    echo "节点名称:"
    echo "  burin-node1    - 节点 1 (Bootstrap Leader)"
    echo "  burin-node2    - 节点 2 (Follower)"
    echo "  burin-node3    - 节点 3 (Follower)"
    echo ""
    echo "示例:"
    echo "  $0 build              # 构建镜像"
    echo "  $0 up-d               # 后台启动集群"
    echo "  $0 status             # 查看状态"
    echo "  $0 logs burin-node1   # 查看 node1 日志"
    echo "  $0 exec burin-node1   # 进入 node1 容器"
    echo "  $0 down               # 停止集群"
}

# 检查 Docker 和 Docker Compose
check_dependencies() {
    if ! command -v docker &> /dev/null; then
        echo -e "${RED}错误: Docker 未安装${NC}"
        exit 1
    fi
    
    if ! docker compose version &> /dev/null; then
        echo -e "${RED}错误: Docker Compose 未安装或版本过旧${NC}"
        echo "请安装 Docker Compose V2"
        exit 1
    fi
}

# 构建镜像
build_image() {
    echo -e "${BLUE}构建 Burin Docker 镜像...${NC}"
    docker compose build
    echo -e "${GREEN}✓ 镜像构建完成${NC}"
}

# 启动集群（前台）
start_cluster() {
    echo -e "${BLUE}启动 Burin 集群（前台运行）...${NC}"
    docker compose up
}

# 启动集群（后台）
start_cluster_detached() {
    echo -e "${BLUE}启动 Burin 集群（后台运行）...${NC}"
    docker compose up -d
    echo -e "${GREEN}✓ 集群已在后台启动${NC}"
    echo ""
    show_status
}

# 停止并移除集群
stop_cluster() {
    echo -e "${BLUE}停止并移除 Burin 集群...${NC}"
    docker compose down
    echo -e "${GREEN}✓ 集群已停止并移除${NC}"
}

# 停止集群（保留容器）
stop_cluster_only() {
    echo -e "${BLUE}停止 Burin 集群...${NC}"
    docker compose stop
    echo -e "${GREEN}✓ 集群已停止${NC}"
}

# 启动已停止的集群
start_stopped_cluster() {
    echo -e "${BLUE}启动已停止的集群...${NC}"
    docker compose start
    echo -e "${GREEN}✓ 集群已启动${NC}"
    echo ""
    show_status
}

# 重启集群
restart_cluster() {
    echo -e "${BLUE}重启 Burin 集群...${NC}"
    docker compose restart
    echo -e "${GREEN}✓ 集群已重启${NC}"
    echo ""
    show_status
}

# 查看集群状态
show_status() {
    echo -e "${BLUE}Burin 集群状态:${NC}"
    docker compose ps
    echo ""
    echo -e "${BLUE}容器健康状态:${NC}"
    docker ps --filter "name=burin-node" --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"
}

# 查看日志
view_logs() {
    local node=$1
    if [ -z "$node" ]; then
        echo -e "${BLUE}查看所有节点日志:${NC}"
        docker compose logs
    else
        echo -e "${BLUE}查看 $node 日志:${NC}"
        docker compose logs "$node"
    fi
}

# 实时查看日志
view_logs_follow() {
    local node=$1
    if [ -z "$node" ]; then
        echo -e "${BLUE}实时查看所有节点日志:${NC}"
        docker compose logs -f
    else
        echo -e "${BLUE}实时查看 $node 日志:${NC}"
        docker compose logs -f "$node"
    fi
}

# 进入容器
exec_container() {
    local node=$1
    if [ -z "$node" ]; then
        echo -e "${RED}错误: 请指定节点名称${NC}"
        echo "可用节点: burin-node1, burin-node2, burin-node3"
        exit 1
    fi
    
    echo -e "${BLUE}进入 $node 容器...${NC}"
    docker compose exec "$node" /bin/bash
}

# 清理所有数据
clean_all() {
    echo -e "${YELLOW}警告: 这将删除所有容器、卷和数据!${NC}"
    read -p "确认继续? (y/N): " -n 1 -r
    echo
    
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        echo -e "${BLUE}停止并移除集群...${NC}"
        docker compose down -v
        
        echo -e "${BLUE}清理未使用的数据...${NC}"
        docker system prune -f
        
        echo -e "${GREEN}✓ 清理完成${NC}"
    else
        echo "操作已取消"
    fi
}

# 查看运行中的容器
show_ps() {
    docker compose ps
}

# 主函数
main() {
    check_dependencies
    
    case "$1" in
        "build")
            build_image
            ;;
        "up")
            start_cluster
            ;;
        "up-d")
            start_cluster_detached
            ;;
        "down")
            stop_cluster
            ;;
        "stop")
            stop_cluster_only
            ;;
        "start")
            start_stopped_cluster
            ;;
        "restart")
            restart_cluster
            ;;
        "status")
            show_status
            ;;
        "logs")
            view_logs "$2"
            ;;
        "logs-f")
            view_logs_follow "$2"
            ;;
        "exec")
            exec_container "$2"
            ;;
        "clean")
            clean_all
            ;;
        "ps")
            show_ps
            ;;
        "help"|"-h"|"--help"|"")
            show_help
            ;;
        *)
            echo -e "${RED}错误: 未知命令 '$1'${NC}"
            echo "使用 '$0 help' 查看帮助信息"
            exit 1
            ;;
    esac
}

main "$@"
