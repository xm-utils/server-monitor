package main

import (
	"context"
	"flag"
	"fmt"

	"github.com/sirupsen/logrus"
	"github.com/xm-utils/tools/grpcx"
	"google.golang.org/grpc"

	"server_monitor_service/internal/admin"
	"server_monitor_service/internal/config"
	"server_monitor_service/internal/logx"
)

func main() {
	configPath := flag.String("config", "", "配置文件路径（默认查找 /etc/server_monitor/config.yaml 与 ./config.yaml）")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		logrus.Fatalf("加载配置失败: %v", err)
	}

	if err := logx.Init(cfg.Log); err != nil {
		logrus.Fatalf("初始化日志失败: %v", err)
	}
	// 内存存储
	store := admin.NewStore()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 启动健康轮询器
	poller := admin.NewPoller(store, cfg.Admin.PollInterval, cfg.Admin.HeartbeatTimeout, cfg.Admin.PollTimeout)
	go poller.Run(ctx)

	// gRPC 服务装配
	grpcSrv := grpcx.NewGrpcServer("admin", fmt.Sprintf(":%d", cfg.Admin.GrpcAddr), func(s *grpc.Server) {
		grpcServer := admin.NewGRPCServer(store)
		grpcServer.RegisterService(s)
	})
	// HTTP 服务装配
	handler := admin.NewHandler(store)
	httpSrv := grpcx.WithHTTPServer("admin", fmt.Sprintf(":%d", cfg.Admin.ListenAddr), handler.Setup)

	srv := grpcx.NewServer([]grpcx.IServer{grpcSrv, httpSrv})
	if err := srv.Start(); err != nil {
		logrus.Fatalf("启动 HTTP 服务失败: %v", err)
	}

	logrus.Infof("admin_monitor_service 已启动，HTTP=%d, gRPC=%d", cfg.Admin.ListenAddr, cfg.Admin.GrpcAddr)

	grpcx.NewShutdown().
		InstallShutdownHook(srv).
		InstallShutdownHook(grpcx.NewCustomHook("admin poller", grpcx.WorkerPoolPriority, cancel)).
		WaitShutdown()
}
