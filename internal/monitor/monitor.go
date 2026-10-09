package monitor

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"

	"server_monitor_service/internal/collector/docker"
	"server_monitor_service/internal/collector/system"
	"server_monitor_service/internal/config"
	"server_monitor_service/internal/model"
	"server_monitor_service/internal/store"
)

// App 组装采集器、存储与操作器，是 handler 依赖的核心运行时。
type App struct {
	cfg      *config.Config
	store    *store.Store
	sysColl  *system.Collector
	dockColl *docker.Collector
	operator *docker.Operator

	lastSysAt    atomic.Int64 // 最近一次系统采集 UnixNano
	lastDockerAt atomic.Int64 // 最近一次 Docker 采集 UnixNano
	dockerOK     atomic.Bool
}

// New 依据配置构建 App 及其依赖。
func New(cfg *config.Config) *App {
	a := &App{
		cfg:     cfg,
		store:   store.New(cfg.Collector.HistorySize),
		sysColl: system.NewCollector(),
	}
	dockerClient := docker.NewClient(cfg.Docker.Endpoint, cfg.Docker.APIVersion, cfg.Docker.Timeout)
	a.dockColl = docker.NewCollector(dockerClient)
	a.operator = docker.NewOperator(dockerClient, docker.OperatorOptions{
		Enabled:         cfg.Docker.Operations.Enabled,
		Grace:           cfg.Docker.Operations.DefaultTimeout,
		AllowLabels:     cfg.Docker.Operations.AllowLabels,
		DenyNames:       cfg.Docker.Operations.DenyNames,
		ComposeCommand:  cfg.Docker.Operations.ComposeCommand,
		DockerBin:       cfg.Docker.Operations.DockerBin,
		ComposeFile:     cfg.Docker.Operations.ComposeFile,
		RedeployTimeout: cfg.Docker.Operations.RedeployTimeout,
	})
	return a
}

// Run 启动周期采集循环，直到 ctx 取消（FR-02/FR-03）。
func (a *App) Run(ctx context.Context) {
	go a.loop(ctx, a.cfg.Collector.SystemInterval, a.collectSystem)
	go a.loop(ctx, a.cfg.Collector.DockerInterval, a.collectDocker)
	// 立即执行一次，避免首个采样等待一个周期
	go a.collectSystem(ctx)
	go a.collectDocker(ctx)
	<-ctx.Done()
	logrus.Info("采集循环已停止")
}

func (a *App) loop(ctx context.Context, interval time.Duration, fn func(context.Context)) {
	if interval <= 0 {
		interval = 3 * time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			fn(ctx)
		}
	}
}

func (a *App) collectSystem(_ context.Context) {
	snap := a.sysColl.Collect()
	a.store.PushSystem(snap)
	a.lastSysAt.Store(time.Now().UnixNano())
}

func (a *App) collectDocker(ctx context.Context) {
	cctx, cancel := context.WithTimeout(ctx, a.cfg.Docker.Timeout+2*time.Second)
	defer cancel()
	res := a.dockColl.Collect(cctx)
	a.store.PushDocker(res)
	a.lastDockerAt.Store(time.Now().UnixNano())
	a.dockerOK.Store(res.Available)
}

// Accessors ---------------------------------------------------------------

func (a *App) Store() *store.Store         { return a.store }
func (a *App) Operator() *docker.Operator  { return a.operator }
func (a *App) Config() *config.Config      { return a.cfg }
func (a *App) DockerDockerAvailable() bool { return a.dockColl.Available() }

// DockerOne 实时采集单容器指标（按 id 或 name），供详情接口使用。
func (a *App) DockerOne(ctx context.Context, id string) (*model.DockerSnapshot, error) {
	return a.dockColl.CollectOne(ctx, id)
}

// SystemHealthy 判断最近一次系统采集是否在 3 个周期内完成（FR-10）。
func (a *App) SystemHealthy() bool {
	last := a.lastSysAt.Load()
	if last == 0 {
		return false
	}
	deadline := int64(3 * a.cfg.Collector.SystemInterval * time.Second / time.Nanosecond)
	return time.Now().UnixNano()-last < deadline
}

// DockerHealthy 判断 Docker 采集状态；未启用连接失败不视为致命。
func (a *App) DockerHealthy() bool {
	last := a.lastDockerAt.Load()
	if last == 0 {
		return false
	}
	deadline := int64(3 * a.cfg.Collector.DockerInterval * time.Second / time.Nanosecond)
	return time.Now().UnixNano()-last < deadline && a.dockerOK.Load()
}
