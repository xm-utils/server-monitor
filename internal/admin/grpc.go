package admin

import (
	"context"

	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"

	pb "server_monitor_service/internal/protocol/pb"
)

// GRPCServer 实现 AdminService，接收服务端注册与异常上报。
type GRPCServer struct {
	pb.UnimplementedAdminServiceServer
	store *Store
}

// NewGRPCServer 创建管理端 gRPC 服务。
func NewGRPCServer(store *Store) *GRPCServer {
	return &GRPCServer{store: store}
}

// RegisterService 注册 gRPC 服务到 server。
func (s *GRPCServer) RegisterService(srv *grpc.Server) {
	pb.RegisterAdminServiceServer(srv, s)
	logrus.Info("gRPC AdminService 已注册")
}

// Register 处理服务端注册/心跳请求。
func (s *GRPCServer) Register(ctx context.Context, req *pb.RegisterRequest) (*pb.RegisterResponse, error) {
	info := s.store.Register(req)
	logrus.Infof("服务端注册: %s (%s:%d)", info.ID, info.Hostname, info.GrpcPort)

	return &pb.RegisterResponse{
		Id:           info.ID,
		HeartbeatSec: 30,
	}, nil
}

// ReportError 处理异常上报。
func (s *GRPCServer) ReportError(ctx context.Context, req *pb.ErrorReport) (*pb.ErrorReportResponse, error) {
	s.store.AddError(req.ServerId, req)
	logrus.Warnf("收到异常上报 [%s]: %s - %s", req.ServerId, req.Component, req.Message)

	return &pb.ErrorReportResponse{}, nil
}
