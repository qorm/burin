#!/bin/bash

# Burin CLI 多平台编译脚本
# 支持 Linux, macOS, Windows 的 amd64 和 arm64 架构

set -e

VERSION=${VERSION:-"1.0.0"}
BUILD_TIME=$(date -u '+%Y-%m-%d_%H:%M:%S')
GIT_COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")

OUTPUT_DIR="./build"
BINARY_NAME="burin-cli"

# 清理并创建输出目录
# rm -rf ${OUTPUT_DIR}
mkdir -p ${OUTPUT_DIR}

# 编译参数
LDFLAGS="-s -w -X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME} -X main.GitCommit=${GIT_COMMIT}"

echo "================================================"
echo "开始编译 Burin CLI v${VERSION}"
echo "构建时间: ${BUILD_TIME}"
echo "Git Commit: ${GIT_COMMIT}"
echo "================================================"

# 定义平台和架构
declare -a PLATFORMS=(
    "linux/amd64"
    "linux/arm64"
    "darwin/amd64"
    "darwin/arm64"
    "windows/amd64"
    "windows/arm64"
)

# 编译所有平台
for PLATFORM in "${PLATFORMS[@]}"; do
    GOOS=${PLATFORM%/*}
    GOARCH=${PLATFORM#*/}
    
    OUTPUT_NAME="${BINARY_NAME}-${GOOS}-${GOARCH}"
    
    if [ "$GOOS" = "windows" ]; then
        OUTPUT_NAME="${OUTPUT_NAME}.exe"
    fi
    
    OUTPUT_PATH="${OUTPUT_DIR}/${OUTPUT_NAME}"
    
    echo ""
    echo "编译: ${GOOS}/${GOARCH}"
    echo "输出: ${OUTPUT_PATH}"
    
    CGO_ENABLED=0 GOOS=${GOOS} GOARCH=${GOARCH} go build \
        -ldflags="${LDFLAGS}" \
        -o ${OUTPUT_PATH} \
        ./cli
    
    if [ $? -eq 0 ]; then
        echo "✅ ${GOOS}/${GOARCH} 编译成功"
        
        # 显示文件大小
        if [ "$GOOS" = "darwin" ]; then
            SIZE=$(stat -f%z "${OUTPUT_PATH}")
        else
            SIZE=$(stat -c%s "${OUTPUT_PATH}" 2>/dev/null || echo "unknown")
        fi
        
        if [ "$SIZE" != "unknown" ]; then
            SIZE_MB=$(echo "scale=2; ${SIZE}/1024/1024" | bc)
            echo "   文件大小: ${SIZE_MB} MB"
        fi
    else
        echo "❌ ${GOOS}/${GOARCH} 编译失败"
        exit 1
    fi
done

echo ""
echo "================================================"
echo "所有平台编译完成！"
echo "输出目录: ${OUTPUT_DIR}"
echo "================================================"
echo ""
ls -lh ${OUTPUT_DIR}
