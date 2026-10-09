package admin

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "server_monitor_service/internal/protocol/pb"
)

// Poller 定时健康检查器。
type Poller struct {
	store          *Store
	pollInterval   time.Duration
	heartbeatTimeout time.Duration
	pollTimeout    time.Duration
}

// NewPoller 创建健康检查器。
func NewPoller(store *Store, pollInterval, heartbeatTimeout, pollTimeout time.Duration) *Poller {
	return &Poller{
		store:            store,
		pollInterval:     pollInterval,
		heartbeatTimeout: heartbeatTimeout,
		pollTimeout:      pollTimeout,
	}
}

// Run 启动健康检查循环。
func (p *Poller) Run(ctx context.Context) {
	ticker := time.NewTicker(p.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.check(ctx)
		}
	}
}

// check 检查所有在线服务器。
func (p *Poller) check(ctx context.Context) {
	servers := p.store.ListServers()

	for _, info := range servers {
		if info.Status != "online" {
			continue
		}

		// 检查心跳超时
		if time.Since(info.LastSeen) > p.heartbeatTimeout {
			logrus.Warnf("服务器心跳超时: %s (%s)", info.ID, info.Hostname)
			p.store.SetOffline(info.ID)
			continue
		}

		// gRPC 健康检查
		if err := p.healthCheck(ctx, info); err != nil {
			logrus.Warnf("健康检查失败: %s (%s): %v", info.ID, info.Hostname, err)
			p.store.SetOffline(info.ID)
		}
	}
}

// healthCheck 对单个服务器执行 gRPC 健康检查。
func (p *Poller) healthCheck(ctx context.Context, info *ServerInfo) error {
	addr := fmt.Sprintf("%s:%d", info.IP, info.GrpcPort)

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer conn.Close()

	client := pb.NewServerServiceClient(conn)
	cctx, cancel := context.WithTimeout(ctx, p.pollTimeout)
	defer cancel()

	resp, err := client.HealthCheck(cctx, &pb.HealthCheckRequest{})
	if err != nil {
		return err
	}

	if !resp.Healthy {
		return fmt.Errorf("服务器报告不健康")
	}

	return nil
}
