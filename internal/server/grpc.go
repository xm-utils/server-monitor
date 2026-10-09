package server

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"

	"server_monitor_service/internal/model"
	pb "server_monitor_service/internal/protocol/pb"
	"server_monitor_service/internal/monitor"
)

// GRPCServer 实现 ServerService，供管理端调用。
type GRPCServer struct {
	pb.UnimplementedServerServiceServer
	app *monitor.App
}

// NewGRPCServer 创建 gRPC 服务。
func NewGRPCServer(app *monitor.App) *GRPCServer {
	return &GRPCServer{app: app}
}

// RegisterService 注册 gRPC 服务到 server。
func (s *GRPCServer) RegisterService(srv *grpc.Server) {
	pb.RegisterServerServiceServer(srv, s)
	logrus.Info("gRPC ServerService 已注册")
}

// HealthCheck 返回健康状态。
func (s *GRPCServer) HealthCheck(ctx context.Context, req *pb.HealthCheckRequest) (*pb.HealthCheckResponse, error) {
	return &pb.HealthCheckResponse{
		Healthy:   s.app.SystemHealthy(),
		Version:   "1.0.0",
		Timestamp: time.Now().Unix(),
	}, nil
}

// GetSystemMetrics 返回系统指标（含扩展字段）。
func (s *GRPCServer) GetSystemMetrics(ctx context.Context, req *pb.GetSystemMetricsRequest) (*pb.GetSystemMetricsResponse, error) {
	snap, ok := s.app.Store().LatestSystem()
	if !ok {
		return &pb.GetSystemMetricsResponse{
			Metrics: &pb.SystemMetrics{},
		}, nil
	}

	// 计算汇总指标
	var diskTotal, diskUsed uint64
	for _, d := range snap.Disks {
		diskTotal += d.TotalBytes
		diskUsed += d.UsedBytes
	}

	diskPercent := 0.0
	if diskTotal > 0 {
		diskPercent = float64(diskUsed) / float64(diskTotal) * 100
	}

	// Docker 摘要
	dockerRes, dockerOK := s.app.Store().LatestDocker()
	var dockerTotal, dockerRunning, dockerStopped int32
	if dockerOK {
		dockerTotal = int32(len(dockerRes.Containers))
		for _, c := range dockerRes.Containers {
			if c.State == "running" {
				dockerRunning++
			} else {
				dockerStopped++
			}
		}
	}

	m := &pb.SystemMetrics{
		// 基础指标
		CpuPercent:      snap.CPU.UsageTotal,
		MemPercent:      snap.Memory.UsedPercent,
		DiskPercent:     diskPercent,
		MemTotal:        snap.Memory.TotalBytes,
		MemUsed:         snap.Memory.UsedBytes,
		DiskTotal:       diskTotal,
		DiskUsed:        diskUsed,
		Timestamp:       snap.Timestamp,
		Hostname:        snap.Hostname,
		NetRateIn:       snap.NetSpeed.RateInBytes,
		NetRateOut:      snap.NetSpeed.RateOutBytes,
		DockerAvailable: dockerOK && dockerRes.Available,
		DockerTotal:     dockerTotal,
		DockerRunning:   dockerRunning,
		DockerStopped:   dockerStopped,
		// 扩展字段
		Os:            snap.OS,
		KernelVer:     snap.KernelVer,
		UptimeSec:     snap.UptimeSec,
		Procs:         int32(snap.Procs),
		CpuCores:      int32(snap.CPU.Cores),
		CpuLoad1:      snap.CPU.Load1,
		CpuLoad5:      snap.CPU.Load5,
		CpuLoad15:     snap.CPU.Load15,
		MemAvailable:  snap.Memory.AvailableBytes,
		SwapTotal:     snap.Memory.SwapTotalBytes,
		SwapUsed:      snap.Memory.SwapUsedBytes,
		SwapFree:      snap.Memory.SwapFreeBytes,
	}

	// 磁盘详情
	for _, d := range snap.Disks {
		m.Disks = append(m.Disks, &pb.DiskInfo{
			Path:        d.Path,
			Fstype:      d.Fstype,
			TotalBytes:  d.TotalBytes,
			UsedBytes:   d.UsedBytes,
			FreeBytes:   d.FreeBytes,
			UsedPercent: d.UsedPercent,
		})
	}

	// Top CPU 进程
	for _, p := range snap.TopCPU {
		m.TopCpu = append(m.TopCpu, &pb.ProcessInfo{
			Pid:        p.PID,
			Name:       p.Name,
			CpuPercent: p.CPUPercent,
			MemPercent: p.MemPercent,
			RssBytes:   p.RssBytes,
		})
	}

	// Top Mem 进程
	for _, p := range snap.TopMem {
		m.TopMem = append(m.TopMem, &pb.ProcessInfo{
			Pid:        p.PID,
			Name:       p.Name,
			CpuPercent: p.CPUPercent,
			MemPercent: p.MemPercent,
			RssBytes:   p.RssBytes,
		})
	}

	return &pb.GetSystemMetricsResponse{Metrics: m}, nil
}

// GetSystemHistory 返回系统指标历史。
func (s *GRPCServer) GetSystemHistory(ctx context.Context, req *pb.GetSystemHistoryRequest) (*pb.GetSystemHistoryResponse, error) {
	limit := int(req.Limit)
	if limit <= 0 {
		limit = 100
	}
	hist := s.app.Store().SystemHistory(limit)

	resp := &pb.GetSystemHistoryResponse{}
	for _, snap := range hist {
		resp.Entries = append(resp.Entries, &pb.SystemHistoryEntry{
			Timestamp:   snap.Timestamp,
			CpuPercent:  snap.CPU.UsageTotal,
			MemPercent:  snap.Memory.UsedPercent,
		})
	}
	return resp, nil
}

// GetDockerMetrics 返回 Docker 容器列表。
func (s *GRPCServer) GetDockerMetrics(ctx context.Context, req *pb.GetDockerMetricsRequest) (*pb.GetDockerMetricsResponse, error) {
	res, ok := s.app.Store().LatestDocker()
	if !ok {
		return &pb.GetDockerMetricsResponse{}, nil
	}

	var containers []*pb.DockerContainer
	for _, c := range res.Containers {
		containers = append(containers, snapshotToPB(&c))
	}

	return &pb.GetDockerMetricsResponse{
		Containers: containers,
	}, nil
}

// GetDockerList 返回 Docker 列表结果（含 Available 状态）。
func (s *GRPCServer) GetDockerList(ctx context.Context, req *pb.GetDockerListRequest) (*pb.GetDockerListResponse, error) {
	res, ok := s.app.Store().LatestDocker()
	if !ok {
		return &pb.GetDockerListResponse{
			Result: &pb.DockerListResult{Available: false, Message: "Docker 数据尚未采集"},
		}, nil
	}

	pbRes := &pb.DockerListResult{
		Available: res.Available,
		Message:   res.Message,
	}
	for _, c := range res.Containers {
		pbRes.Containers = append(pbRes.Containers, snapshotToPB(&c))
	}

	return &pb.GetDockerListResponse{Result: pbRes}, nil
}

// GetDockerContainerDetail 实时采集单容器详情。
func (s *GRPCServer) GetDockerContainerDetail(ctx context.Context, req *pb.GetDockerContainerDetailRequest) (*pb.GetDockerContainerDetailResponse, error) {
	snap, err := s.app.DockerOne(ctx, req.ContainerId)
	if err != nil {
		return &pb.GetDockerContainerDetailResponse{NotFound: true}, nil
	}

	return &pb.GetDockerContainerDetailResponse{
		Container: snapshotToPBFull(snap),
	}, nil
}

// snapshotToPB 将 model.DockerSnapshot 转换为 pb.DockerContainer（列表用，不含详情字段）。
func snapshotToPB(c *model.DockerSnapshot) *pb.DockerContainer {
	return &pb.DockerContainer{
		Id:           c.ContainerID,
		Name:         c.Name,
		Image:        c.Image,
		State:        c.State,
		CpuPercent:   c.CPUPercent,
		MemPercent:   c.MemPercent,
		MemUsage:     c.MemUsageBytes,
		MemLimit:     c.MemLimitBytes,
		Timestamp:    c.Timestamp,
		NetRxRate:    c.NetRxRate,
		NetTxRate:    c.NetTxRate,
		BlkReadRate:  c.BlkReadRate,
		BlkWriteRate: c.BlkWriteRate,
		Status:       c.Status,
		Health:       c.Health,
		StartedAt:    c.StartedAt,
		RestartCount: int32(c.RestartCount),
		PidsCurrent:  c.PidsCurrent,
	}
}

// snapshotToPBFull 将 model.DockerSnapshot 转换为 pb.DockerContainer（含详情字段）。
func snapshotToPBFull(c *model.DockerSnapshot) *pb.DockerContainer {
	pc := snapshotToPB(c)
	pc.Env = c.Env

	for _, m := range c.Mounts {
		pc.Mounts = append(pc.Mounts, &pb.DockerMount{
			Type:        m.Type,
			Source:      m.Source,
			Destination: m.Destination,
			Rw:          m.RW,
		})
	}

	if len(c.PortBindings) > 0 {
		pc.PortBindings = make(map[string]*pb.DockerPortBindings)
		for port, binds := range c.PortBindings {
			pc.PortBindings[port] = &pb.DockerPortBindings{Host: binds}
		}
	}

	for _, ni := range c.NetworkInterfaces {
		pc.NetworkInterfaces = append(pc.NetworkInterfaces, &pb.DockerNetworkInterface{
			Name:       ni.Name,
			IpAddress:  ni.IPAddress,
			Gateway:    ni.Gateway,
			MacAddress: ni.MacAddress,
		})
	}

	return pc
}
