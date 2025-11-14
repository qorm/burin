# Burin Docker 镜像
# 多阶段构建，减小镜像体积

# 构建阶段
FROM golang:1.24-alpine AS builder

# 安装必要的工具
RUN apk add --no-cache git make bash

# 设置工作目录
WORKDIR /build

# 复制 go.mod 和 go.sum
COPY go.mod go.sum ./

# 下载依赖
RUN go mod download

# 复制源代码
COPY . .

# 构建二进制文件
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o burin main.go

# 运行阶段
FROM alpine:latest

# 安装运行时依赖
RUN apk add --no-cache ca-certificates tzdata bash

# 设置时区
ENV TZ=Asia/Shanghai

# 创建非 root 用户
RUN addgroup -g 1000 burin && \
    adduser -D -u 1000 -G burin burin

# 设置工作目录
WORKDIR /app

# 从构建阶段复制二进制文件
COPY --from=builder /build/burin /app/burin

# 创建数据目录
RUN mkdir -p /app/data /app/logs && \
    chown -R burin:burin /app

# 切换到非 root 用户
USER burin

# 暴露端口
# 8099: Burin 客户端服务端口
# 8300: Raft 共识协议端口
EXPOSE 8099 8300

# 健康检查
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD nc -z localhost 8099 || exit 1

# 启动命令
ENTRYPOINT ["/app/burin"]
CMD ["-config", "/app/config/burin.yaml"]
