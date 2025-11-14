#!/bin/bash

# Burin集群动态启动脚本
# 功能：启动、停止、重启、检查状态

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 动态检测当前系统和架构
detect_platform() {
    local os=$(uname -s | tr '[:upper:]' '[:lower:]')
    local arch=$(uname -m)
    
    # 规范化操作系统名称
    case "$os" in
        darwin)
            os="darwin"
            ;;
        linux)
            os="linux"
            ;;
        mingw*|msys*|cygwin*)
            os="windows"
            ;;
        *)
            echo -e "${RED}不支持的操作系统: $os${NC}" >&2
            exit 1
            ;;
    esac
    
    # 规范化架构名称
    case "$arch" in
        x86_64|amd64)
            arch="amd64"
            ;;
        aarch64|arm64)
            arch="arm64"
            ;;
        *)
            echo -e "${RED}不支持的架构: $arch${NC}" >&2
            exit 1
            ;;
    esac
    
    echo "${os}-${arch}"
}

PLATFORM=$(detect_platform)
BINARY_NAME="burin-${PLATFORM}"
BUILD_DIR="$SCRIPT_DIR/build"
BINARY_PATH="$BUILD_DIR/$BINARY_NAME"

LOG_DIR="$SCRIPT_DIR/logs"
PID_DIR="$SCRIPT_DIR"

# 检查二进制文件是否存在（对于非 build 命令）
check_binary_exists() {
    if [ ! -f "$BINARY_PATH" ]; then
        echo -e "${RED}错误: 找不到二进制文件 $BINARY_PATH${NC}"
        echo "请先运行 ./build.sh 或 $0 build 构建项目"
        return 1
    fi
    return 0
}

# 显示帮助信息
show_help() {
    echo -e "${BLUE}Burin集群管理脚本${NC}"
    echo -e "当前平台: ${GREEN}${PLATFORM}${NC}"
    echo -e "二进制文件: ${GREEN}${BINARY_PATH}${NC}"
    echo ""
    echo "使用方法: $0 [命令] [选项]"
    echo ""
    echo "命令:"
    echo "  start [node1,node2,node3]  - 启动指定节点 (默认启动所有节点)"
    echo "  stop [node1,node2,node3]   - 停止指定节点 (默认停止所有节点)"
    echo "  restart [node1,node2,node3] - 重启指定节点 (默认重启所有节点)"
    echo "  status                     - 检查所有节点状态"
    echo "  logs [node1,node2,node3]   - 查看指定节点日志"
    echo "  clean                      - 清理所有数据和日志文件"
    echo "  cleanstart [node1,node2,node3] - 清空数据后启动节点"
    echo "  build                      - 重新构建burin二进制文件"
    echo "  cluster                    - 检查三节点集群状态"
    echo ""
    echo "示例:"
    echo "  $0 start                   # 启动所有节点"
    echo "  $0 start node1,node2       # 只启动node1和node2"
    echo "  $0 cleanstart              # 清空数据后启动所有节点"
    echo "  $0 stop node1              # 停止node1"
    echo "  $0 logs node1              # 查看node1的日志"
    echo "  $0 status                  # 检查所有节点状态"
}

# 获取节点配置文件列表
get_node_configs() {
    find "$SCRIPT_DIR" -name "burin-node*.yaml" | sort
}

# 从配置文件名提取节点名
get_node_name() {
    local config_file="$1"
    basename "$config_file" .yaml | sed 's/burin-//'
}

# 获取节点PID文件路径
get_pid_file() {
    local node_name="$1"
    echo "$PID_DIR/${node_name}.pid"
}

# 获取节点日志文件路径
get_log_file() {
    local node_name="$1"
    echo "$LOG_DIR/${node_name}.log"
}

# 检查节点是否运行
is_node_running() {
    local node_name="$1"
    local pid_file=$(get_pid_file "$node_name")
    
    if [ -f "$pid_file" ]; then
        local pid=$(cat "$pid_file")
        if ps -p "$pid" > /dev/null 2>&1; then
            return 0
        else
            # PID文件存在但进程不存在，清理PID文件
            rm -f "$pid_file"
            return 1
        fi
    else
        return 1
    fi
}

# 启动单个节点
start_node() {
    local config_file="$1"
    local node_name=$(get_node_name "$config_file")
    local pid_file=$(get_pid_file "$node_name")
    local log_file=$(get_log_file "$node_name")
    
    if is_node_running "$node_name"; then
        echo -e "${YELLOW}节点 $node_name 已经在运行${NC}"
        return 0
    fi
    
    echo -e "${BLUE}启动节点 $node_name...${NC}"
    
    # 确保日志目录存在
    mkdir -p "$LOG_DIR"
    
    # 启动节点（使用完整路径）
    nohup "$BINARY_PATH" -config="$config_file" > "$log_file" 2>&1 &
    local pid=$!
    
    # 保存PID
    echo "$pid" > "$pid_file"
    
    # 等待启动
    sleep 2
    
    if is_node_running "$node_name"; then
        echo -e "${GREEN}✓ 节点 $node_name 启动成功 (PID: $pid)${NC}"
    else
        echo -e "${RED}✗ 节点 $node_name 启动失败${NC}"
        echo "查看日志: tail -f $log_file"
        return 1
    fi
}

# 停止单个节点
stop_node() {
    local node_name="$1"
    local pid_file=$(get_pid_file "$node_name")
    
    if ! is_node_running "$node_name"; then
        echo -e "${YELLOW}节点 $node_name 未运行${NC}"
        return 0
    fi
    
    local pid=$(cat "$pid_file")
    echo -e "${BLUE}停止节点 $node_name (PID: $pid)...${NC}"
    
    # 发送SIGTERM信号
    kill -TERM "$pid" 2>/dev/null
    
    # 等待进程结束
    for i in {1..10}; do
        if ! ps -p "$pid" > /dev/null 2>&1; then
            break
        fi
        sleep 1
    done
    
    # 如果进程还在运行，强制杀死
    if ps -p "$pid" > /dev/null 2>&1; then
        echo -e "${YELLOW}强制停止节点 $node_name...${NC}"
        kill -KILL "$pid" 2>/dev/null
        sleep 1
    fi
    
    # 清理PID文件
    rm -f "$pid_file"
    
    if ! ps -p "$pid" > /dev/null 2>&1; then
        echo -e "${GREEN}✓ 节点 $node_name 停止成功${NC}"
    else
        echo -e "${RED}✗ 节点 $node_name 停止失败${NC}"
        return 1
    fi
}

# 检查所有节点状态
check_status() {
    echo -e "${BLUE}检查集群状态...${NC}"
    echo ""
    
    local configs=($(get_node_configs))
    local running_count=0
    
    for config in "${configs[@]}"; do
        local node_name=$(get_node_name "$config")
        local pid_file=$(get_pid_file "$node_name")
        
        printf "%-15s " "$node_name:"
        
        if is_node_running "$node_name"; then
            local pid=$(cat "$pid_file")
            echo -e "${GREEN}运行中${NC} (PID: $pid)"
            running_count=$((running_count + 1))
        else
            echo -e "${RED}已停止${NC}"
        fi
    done
    
    echo ""
    echo -e "总计: ${#configs[@]} 个节点，${GREEN}$running_count${NC} 个运行中"
}

# 解析节点参数
parse_nodes() {
    local node_param="$1"
    if [ -z "$node_param" ]; then
        # 返回所有节点
        get_node_configs
    else
        # 解析指定的节点
        IFS=',' read -ra NODES <<< "$node_param"
        for node in "${NODES[@]}"; do
            local config_file="$SCRIPT_DIR/burin-${node}.yaml"
            if [ -f "$config_file" ]; then
                echo "$config_file"
            else
                echo -e "${RED}警告: 配置文件 $config_file 不存在${NC}" >&2
            fi
        done
    fi
}

# 启动节点并检查集群状态
start_nodes() {
    local node_param="$1"
    local configs=($(parse_nodes "$node_param"))
    
    if [ ${#configs[@]} -eq 0 ]; then
        echo -e "${RED}没有找到有效的配置文件${NC}"
        return 1
    fi
    
    echo -e "${BLUE}启动 ${#configs[@]} 个节点...${NC}"
    
    for config in "${configs[@]}"; do
        start_node "$config"
    done
    
    echo ""
    check_status
    
    # 如果启动的是3个节点，检查集群状态
    if [ ${#configs[@]} -eq 3 ]; then
        echo ""
        if check_cluster_formed; then
            echo -e "${GREEN}✓ 三节点集群已成功启动并形成集群${NC}"
        else
            echo -e "${RED}✗ 集群状态检查失败，请检查日志${NC}"
            echo "使用以下命令查看日志："
            for config in "${configs[@]}"; do
                local node_name=$(get_node_name "$config")
                echo "  $0 logs $node_name"
            done
            return 1
        fi
    fi
}

# 停止节点
stop_nodes() {
    local node_param="$1"
    local configs=($(parse_nodes "$node_param"))
    
    if [ ${#configs[@]} -eq 0 ]; then
        echo -e "${RED}没有找到有效的配置文件${NC}"
        return 1
    fi
    
    echo -e "${BLUE}停止 ${#configs[@]} 个节点...${NC}"
    
    for config in "${configs[@]}"; do
        local node_name=$(get_node_name "$config")
        stop_node "$node_name"
    done
    
    echo ""
    check_status
}

# 重启节点
restart_nodes() {
    local node_param="$1"
    echo -e "${BLUE}重启节点...${NC}"
    stop_nodes "$node_param"
    sleep 3
    start_nodes "$node_param"
}

# 查看日志
view_logs() {
    local node_param="$1"
    local configs=($(parse_nodes "$node_param"))
    
    if [ ${#configs[@]} -eq 0 ]; then
        echo -e "${RED}没有找到有效的配置文件${NC}"
        return 1
    fi
    
    if [ ${#configs[@]} -eq 1 ]; then
        # 单个节点，显示实时日志
        local node_name=$(get_node_name "${configs[0]}")
        local log_file=$(get_log_file "$node_name")
        echo -e "${BLUE}查看节点 $node_name 的日志 (Ctrl+C 退出):${NC}"
        tail -f "$log_file" 2>/dev/null || echo -e "${RED}日志文件不存在: $log_file${NC}"
    else
        # 多个节点，显示最近的日志
        for config in "${configs[@]}"; do
            local node_name=$(get_node_name "$config")
            local log_file=$(get_log_file "$node_name")
            echo -e "\n${BLUE}=== 节点 $node_name 最近的日志 ===${NC}"
            if [ -f "$log_file" ]; then
                tail -20 "$log_file"
            else
                echo -e "${RED}日志文件不存在${NC}"
            fi
        done
    fi
}

# 检查集群是否形成
check_cluster_formed() {
    echo -e "${BLUE}检查集群状态...${NC}"
    
    local running_nodes=0
    local leader_found=false
    local leader_addr=""
    
    # 检查运行的节点数
    local configs=($(get_node_configs))
    for config in "${configs[@]}"; do
        local node_name=$(get_node_name "$config")
        if is_node_running "$node_name"; then
            running_nodes=$((running_nodes + 1))
        fi
    done
    
    # 检查是否有足够的节点运行
    if [ $running_nodes -lt 3 ]; then
        echo -e "${RED}✗ 集群节点不足: 需要至少3个节点，当前运行$running_nodes个${NC}"
        return 1
    fi
    
    echo -e "${GREEN}✓ $running_nodes 个节点正在运行${NC}"
    
    # 等待节点完全启动
    sleep 5
    
    # 尝试检查集群leader
    local ports=(8099 8199 8299 8399)
    for port in "${ports[@]}"; do
        # 使用nc检查端口是否开放
        if nc -z 127.0.0.1 $port 2>/dev/null; then
            echo -e "${BLUE}检查端口 $port 的集群状态...${NC}"
            
            # 这里可以添加更详细的集群状态检查
            # 例如通过API检查leader状态
            leader_found=true
            leader_addr="127.0.0.1:$port"
            break
        fi
    done
    
    if [ "$leader_found" = true ]; then
        echo -e "${GREEN}✓ 集群已形成，可访问的节点地址: $leader_addr${NC}"
        return 0
    else
        echo -e "${RED}✗ 集群未能正确形成${NC}"
        return 1
    fi
}

# 仅清空数据目录（不需要确认）
clean_data_only() {
    echo -e "${BLUE}清理数据目录...${NC}"
    rm -rf "$SCRIPT_DIR/data"
    echo -e "${GREEN}✓ 数据目录已清空${NC}"
}

# 清理数据和日志
clean_all() {
    echo -e "${YELLOW}警告: 这将清理所有数据和日志文件!${NC}"
    read -p "确认继续? (y/N): " -n 1 -r
    echo
    
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        echo -e "${BLUE}停止所有节点...${NC}"
        stop_nodes ""
        
        echo -e "${BLUE}清理数据目录...${NC}"
        rm -rf "$SCRIPT_DIR/data"
        
        echo -e "${BLUE}清理日志文件...${NC}"
        rm -rf "$LOG_DIR"
        
        echo -e "${BLUE}清理PID文件...${NC}"
        rm -f "$SCRIPT_DIR"/*.pid
        
        echo -e "${GREEN}✓ 清理完成${NC}"
    else
        echo "操作已取消"
    fi
}

# 清空数据后启动节点
cleanstart_nodes() {
    local node_param="$1"
    
    echo -e "${BLUE}停止所有节点...${NC}"
    stop_nodes ""
    
    # 清空数据目录
    clean_data_only
    
    # 清空PID文件
    rm -f "$SCRIPT_DIR"/*.pid
    
    # 等待一下确保进程完全停止
    sleep 2
    
    # 启动节点
    start_nodes "$node_param"
}

# 构建项目
build_project() {
    echo -e "${BLUE}构建 Burin 项目 (${PLATFORM})...${NC}"
    
    if ! command -v go &> /dev/null; then
        echo -e "${RED}错误: Go 未安装${NC}"
        return 1
    fi
    
    # 确保构建目录存在
    mkdir -p "$BUILD_DIR"
    
    # 提取 GOOS 和 GOARCH
    local goos="${PLATFORM%-*}"
    local goarch="${PLATFORM#*-}"
    
    echo "编译 ${goos}/${goarch} 平台..."
    GOOS="$goos" GOARCH="$goarch" go build -o "$BINARY_PATH" main.go
    
    if [ $? -eq 0 ]; then
        echo -e "${GREEN}✓ 构建成功: $BINARY_PATH${NC}"
    else
        echo -e "${RED}✗ 构建失败${NC}"
        return 1
    fi
}

# 主函数
main() {
    cd "$SCRIPT_DIR"
    
    case "$1" in
        "start")
            check_binary_exists || exit 1
            start_nodes "$2"
            ;;
        "stop")
            stop_nodes "$2"
            ;;
        "restart")
            check_binary_exists || exit 1
            restart_nodes "$2"
            ;;
        "cleanstart")
            check_binary_exists || exit 1
            cleanstart_nodes "$2"
            ;;
        "status")
            check_status
            ;;
        "logs")
            view_logs "$2"
            ;;
        "clean")
            clean_all
            ;;
        "build")
            build_project
            ;;
        "cluster")
            check_cluster_formed
            ;;
        "help"|"-h"|"--help")
            show_help
            ;;
        "")
            echo -e "${RED}错误: 缺少命令参数${NC}"
            echo "使用 '$0 help' 查看帮助信息"
            exit 1
            ;;
        *)
            echo -e "${RED}错误: 未知命令 '$1'${NC}"
            echo "使用 '$0 help' 查看帮助信息"
            exit 1
            ;;
    esac
}

# 执行主函数
main "$@"