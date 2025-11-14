package cProtocol

import (
	"context"
)

// ClusterRequest 集群内部通信请求
type ClusterRequest struct {
	Head       *HEAD
	Data       []byte
	Meta       map[string]string
	SourceNode string // 源节点ID
	TargetNode string // 目标节点ID (可选)
	RequestID  string // 请求ID
	Timestamp  int64  // 时间戳
}

// ClusterResponse 集群内部通信响应
type ClusterResponse struct {
	Head       *HEAD
	Status     int
	Data       []byte
	Error      string
	ResponseTo string // 响应的请求ID
	SourceNode string // 响应节点ID
	Timestamp  int64  // 响应时间戳
}

// ClusterHandler 集群命令处理器接口
type ClusterHandler interface {
	HandleCluster(ctx context.Context, req *ClusterRequest) (*ClusterResponse, error)
}

// ClusterHandlerFunc 集群处理器函数类型
type ClusterHandlerFunc func(ctx context.Context, req *ClusterRequest) (*ClusterResponse, error)

func (f ClusterHandlerFunc) HandleCluster(ctx context.Context, req *ClusterRequest) (*ClusterResponse, error) {
	return f(ctx, req)
}
