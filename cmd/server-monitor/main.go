package main

import (
	"context"
	"flag"
	"fmt"
	"net"

	"github.com/sirupsen/logrus"
	"github.com/xm-utils/tools/grpcx"
	"google.golang.org/grpc"

	"server_monitor_service/internal/agent"
	"server_monitor_service/internal/config"
	"server_monitor_service/internal/logx"
	"server_monitor_service/internal/monitor"
	"server_monitor_service/internal/server"
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

	// 核心运行时：采集 + 存储 + 操作
	app := monitor.New(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go app.Run(ctx)

	// HTTP 服务装配（grpcx + gin）
	handler := server.NewHandler(app)
	httpSrv := grpcx.WithHTTPServer("monitor", fmt.Sprintf(":%d", cfg.Server.ListenAddr), handler.Setup)

	// gRPC 服务装配
	grpcSrv := grpc.NewServer()
	grpcServer := server.NewGRPCServer(app)
	grpcServer.RegisterService(grpcSrv)

	// 启动 HTTP + gRPC
	srv := grpcx.NewServer([]grpcx.IServer{httpSrv})
	if err := srv.Start(); err != nil {
		logrus.Fatalf("启动 HTTP 服务失败: %v", err)
	}

	// 启动 gRPC 监听
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.Server.GrpcAddr))
	if err != nil {
		logrus.Fatalf("启动 gRPC 监听失败: %v", err)
	}
	go func() {
		logrus.Infof("gRPC ServerService 已启动，监听 :%d", cfg.Server.GrpcAddr)
		if err := grpcSrv.Serve(lis); err != nil {
			logrus.Fatalf("gRPC 服务失败: %v", err)
		}
	}()

	// 注册到管理端（如果配置了 admin_url）
	if cfg.Server.AdminURL != "" {
		adminAddr, err := agent.ParseAdminURL(cfg.Server.AdminURL)
		if err != nil {
			logrus.Warnf("解析 admin_url 失败: %v", err)
		} else {
			registerer := agent.NewRegisterer(adminAddr, cfg.Server.ListenAddr, cfg.Server.GrpcAddr, cfg.Server.HeartbeatInterval)
			go func() {
				if err := registerer.Run(ctx); err != nil {
					logrus.Warnf("注册/心跳循环失败: %v", err)
				}
			}()
		}
	}

	logrus.Infof("server_monitor_service 已启动，HTTP=%d, gRPC=%d", cfg.Server.ListenAddr, cfg.Server.GrpcAddr)

	grpcx.NewShutdown().
		InstallShutdownHook(srv).
		InstallShutdownHook(grpcx.NewCustomHook("monitor server", grpcx.WorkerPoolPriority, func() {
			cancel()
			grpcSrv.GracefulStop()
		})).
		WaitShutdown()
}
