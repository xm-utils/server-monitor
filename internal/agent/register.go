package agent

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "server_monitor_service/internal/protocol/pb"
)

// Registerer 负责向管理端注册与心跳续期。
type Registerer struct {
	adminURL    string
	hostname    string
	ip          string
	port        int
	grpcPort    int
	interval    time.Duration
	serverID    string
	conn        *grpc.ClientConn
	client      pb.AdminServiceClient
}

// NewRegisterer 创建注册器。
func NewRegisterer(adminURL string, port, grpcPort int, interval time.Duration) *Registerer {
	hostname, _ := os.Hostname()
	return &Registerer{
		adminURL: adminURL,
		hostname: hostname,
		ip:       getLocalIP(),
		port:     port,
		grpcPort: grpcPort,
		interval: interval,
	}
}

// getLocalIP 获取本机非回环 IPv4 地址。
func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		if netAddr, ok := addr.(*net.IPNet); ok && !netAddr.IP.IsLoopback() {
			if netAddr.IP.To4() != nil {
				return netAddr.IP.String()
			}
		}
	}
	return ""
}

// Run 启动注册与心跳循环，直到 ctx 取消。
func (r *Registerer) Run(ctx context.Context) error {
	// 连接管理端
	conn, err := grpc.NewClient(r.adminURL, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("连接管理端失败: %w", err)
	}
	r.conn = conn
	r.client = pb.NewAdminServiceClient(conn)

	// 首次注册
	if err := r.register(ctx); err != nil {
		logrus.Warnf("首次注册失败: %v，将继续重试", err)
	}

	// 定时心跳
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			r.conn.Close()
			return nil
		case <-ticker.C:
			if err := r.register(ctx); err != nil {
				logrus.Warnf("心跳续期失败: %v", err)
			}
		}
	}
}

// register 向管理端发送注册/心跳请求。
func (r *Registerer) register(ctx context.Context) error {
	req := &pb.RegisterRequest{
		Hostname:     r.hostname,
		Ip:           r.ip,
		Port:         int32(r.port),
		OsName:       runtime.GOOS,
		Arch:         runtime.GOARCH,
		Capabilities: []string{"docker"},
		GrpcPort:     int32(r.grpcPort),
	}

	resp, err := r.client.Register(ctx, req)
	if err != nil {
		return err
	}

	r.serverID = resp.Id
	logrus.Infof("已注册到管理端，serverID=%s，心跳间隔=%ds", resp.Id, resp.HeartbeatSec)
	return nil
}

// ServerID 返回当前注册的服务器 ID。
func (r *Registerer) ServerID() string {
	return r.serverID
}

// SetIP 设置 IP 地址（可选）。
func (r *Registerer) SetIP(ip string) {
	r.ip = ip
}

// GenerateServerID 根据 hostname:grpcPort 生成唯一 ID（SHA256 前 12 位）。
func GenerateServerID(hostname string, grpcPort int) string {
	key := fmt.Sprintf("%s:%d", hostname, grpcPort)
	hash := sha256.Sum256([]byte(key))
	return fmt.Sprintf("%x", hash[:6])
}

// Reporter 负责向管理端上报异常。
type Reporter struct {
	adminURL string
	conn     *grpc.ClientConn
	client   pb.AdminServiceClient
	serverID string
}

// NewReporter 创建异常上报器。
func NewReporter(adminURL string) *Reporter {
	return &Reporter{adminURL: adminURL}
}

// Init 初始化连接（需在注册后调用）。
func (r *Reporter) Init(ctx context.Context) error {
	conn, err := grpc.NewClient(r.adminURL, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("连接管理端失败: %w", err)
	}
	r.conn = conn
	r.client = pb.NewAdminServiceClient(conn)
	return nil
}

// SetServerID 设置服务器 ID。
func (r *Reporter) SetServerID(id string) {
	r.serverID = id
}

// ReportError 异步上报异常（非阻塞）。
func (r *Reporter) ReportError(ctx context.Context, component, message, severity string) {
	if r.client == nil || r.serverID == "" {
		return
	}

	go func() {
		req := &pb.ErrorReport{
			ServerId:  r.serverID,
			Component: component,
			Message:   message,
			Timestamp: time.Now().Unix(),
			Severity:  severity,
		}

		cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		if _, err := r.client.ReportError(cctx, req); err != nil {
			logrus.Warnf("上报异常失败 [%s]: %v", component, err)
		}
	}()
}

// Close 关闭连接。
func (r *Reporter) Close() {
	if r.conn != nil {
		r.conn.Close()
	}
}

// ParseAdminURL 解析管理端 URL 提取 host:port。
func ParseAdminURL(adminURL string) (string, error) {
	adminURL = strings.TrimPrefix(adminURL, "http://")
	adminURL = strings.TrimPrefix(adminURL, "https://")
	if adminURL == "" {
		return "", fmt.Errorf("admin_url 为空")
	}
	return adminURL, nil
}
