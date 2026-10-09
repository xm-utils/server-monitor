package docker

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"server_monitor_service/internal/model"
)

// Collector 采集 Docker 容器运行指标（FR-03），并缓存上一次累计值用于计算速率。
type Collector struct {
	client *Client
	mu     sync.Mutex
	prev   map[string]prevCounters
}

type prevCounters struct {
	netRx    uint64
	netTx    uint64
	blkRead  uint64
	blkWrite uint64
	at       time.Time
}

func NewCollector(client *Client) *Collector {
	return &Collector{client: client, prev: map[string]prevCounters{}}
}

// Available 报告 Docker 是否可连接。
func (c *Collector) Available() bool {
	if c.client == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return c.client.Ping(ctx) == nil
}

// Collect 采集全部容器指标快照。Docker 不可用时返回 Available=false，服务不中断。
func (c *Collector) Collect(ctx context.Context) model.DockerListResult {
	if c.client == nil {
		return model.DockerListResult{Available: false, Message: "docker client 未初始化"}
	}
	list, err := c.client.ListContainers(ctx, true)
	if err != nil {
		return model.DockerListResult{Available: false, Message: "Docker 未连接: " + err.Error()}
	}

	now := time.Now()
	result := model.DockerListResult{Available: true}
	for i := range list {
		sum := list[i]
		snap := model.DockerSnapshot{
			Timestamp:   now.Unix(),
			ContainerID: shortID(sum.ID),
			Name:        trimName(sum.Names),
			Image:       sum.Image,
			ImageID:     sum.ImageID,
			State:       sum.State,
			Status:      sum.Status,
			Created:     sum.Created,
		}

		if ins, err := c.client.InspectContainer(ctx, sum.ID); err == nil && ins != nil {
			c.applyInspect(&snap, ins)
		}
		if st, err := c.client.ContainerStats(ctx, sum.ID); err == nil && st != nil {
			c.applyStats(&snap, st, now)
		}

		result.Containers = append(result.Containers, snap)
	}

	// 清理已消失容器的缓存
	c.mu.Lock()
	present := make(map[string]bool, len(result.Containers))
	for _, s := range result.Containers {
		present[s.ContainerID] = true
	}
	for id := range c.prev {
		if !present[id] {
			delete(c.prev, id)
		}
	}
	c.mu.Unlock()

	return result
}

// CollectOne 实时采集单个容器的指标快照（按 id 或 name）。
// 与 Collect 复用同一套速率缓存，便于详情接口获取实时 CPU/内存。
func (c *Collector) CollectOne(ctx context.Context, id string) (*model.DockerSnapshot, error) {
	if c.client == nil {
		return nil, fmt.Errorf("docker client 未初始化")
	}
	ins, err := c.client.InspectContainer(ctx, id)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	snap := model.DockerSnapshot{
		Timestamp:   now.Unix(),
		ContainerID: shortID(ins.ID),
		Name:        strings.TrimPrefix(ins.Name, "/"),
		Image:       ins.Config.Image,
		State:       ins.State.Status,
	}
	c.applyInspect(&snap, ins)
	if st, err := c.client.ContainerStats(ctx, id); err == nil && st != nil {
		c.applyStats(&snap, st, now)
	}
	return &snap, nil
}

func (c *Collector) applyInspect(snap *model.DockerSnapshot, ins *containerInspect) {
	snap.RestartCount = ins.RestartCount
	if ins.Config.Image != "" {
		snap.Image = ins.Config.Image
	}
	snap.State = ins.State.Status
	if t, err := time.Parse(time.RFC3339Nano, ins.State.StartedAt); err == nil {
		snap.StartedAt = t.Unix()
	}
	if ins.State.Health != nil {
		snap.Health = ins.State.Health.Status
	} else {
		snap.Health = "none"
	}
	if ins.HostConfig.Memory > 0 {
		snap.MemLimitBytes = uint64(ins.HostConfig.Memory)
	}
	snap.Env = ins.Config.Env
	for _, m := range ins.Mounts {
		snap.Mounts = append(snap.Mounts, model.Mount{
			Type: m.Type, Source: m.Source, Destination: m.Destination, RW: m.RW,
		})
	}
	if len(ins.NetworkSettings.Ports) > 0 {
		snap.PortBindings = map[string][]string{}
		for port, binds := range ins.NetworkSettings.Ports {
			for _, b := range binds {
				snap.PortBindings[port] = append(snap.PortBindings[port], b.HostIP+":"+b.HostPort)
			}
		}
	}
	// 网卡与 IP 信息
	if len(ins.NetworkSettings.Networks) > 0 {
		for name, net := range ins.NetworkSettings.Networks {
			snap.NetworkInterfaces = append(snap.NetworkInterfaces, model.NetworkInterface{
				Name:       name,
				IPAddress:  net.IPAddress,
				Gateway:    net.Gateway,
				MacAddress: net.MacAddress,
			})
		}
	}
}

func (c *Collector) applyStats(snap *model.DockerSnapshot, st *rawStats, now time.Time) {
	snap.MemUsageBytes = st.MemoryStats.Usage
	if st.MemoryStats.Limit > 0 && snap.MemLimitBytes == 0 {
		snap.MemLimitBytes = st.MemoryStats.Limit
	}
	if snap.MemLimitBytes > 0 {
		snap.MemPercent = round2(float64(snap.MemUsageBytes) / float64(snap.MemLimitBytes) * 100)
	}
	snap.PidsCurrent = st.PidsStats.Current
	snap.CPUKernelMode = st.CPUStats.CPUUsage.KernelMode
	snap.CPUUserMode = st.CPUStats.CPUUsage.UserMode

	// CPU% —— 与 docker stats 一致的算法：单次采样使用 cpu_stats 与 precpu_stats 差值。
	cpuDelta := subU64(st.CPUStats.CPUUsage.TotalUsage, st.PrecpuStats.CPUUsage.TotalUsage)
	sysDelta := subU64(st.CPUStats.SystemCPUUsage, st.PrecpuStats.SystemCPUUsage)
	online := st.CPUStats.OnlineCPUs
	if online == 0 {
		online = 1
	}
	if sysDelta > 0 && cpuDelta > 0 {
		snap.CPUPercent = round2(float64(cpuDelta) / float64(sysDelta) * float64(online) * 100)
	}

	// 网络：累计与按网络分组。
	var netRx, netTx uint64
	if len(st.Networks) > 0 {
		snap.Networks = map[string]model.NetStat{}
	}
	for name, ns := range st.Networks {
		netRx += ns.RxBytes
		netTx += ns.TxBytes
		snap.Networks[name] = model.NetStat{
			Name: name, BytesRecv: ns.RxBytes, BytesSent: ns.TxBytes,
			PacketsRecv: ns.RxPackets, PacketsSent: ns.TxPackets,
		}
	}
	snap.NetRxBytes = netRx
	snap.NetTxBytes = netTx

	// 块设备 IO。
	var blkRead, blkWrite uint64
	devAgg := map[string]*model.BlockDeviceStat{}
	for _, rec := range st.BlkioStats.IOServiceBytesRecursive {
		switch strings.ToLower(rec.Op) {
		case "read":
			blkRead += rec.Value
		case "write":
			blkWrite += rec.Value
		}
		if _, ok := devAgg[rec.Device]; !ok {
			devAgg[rec.Device] = &model.BlockDeviceStat{Device: rec.Device}
		}
		d := devAgg[rec.Device]
		switch strings.ToLower(rec.Op) {
		case "read":
			d.ReadBytes += rec.Value
		case "write":
			d.WriteBytes += rec.Value
		}
	}
	snap.BlkReadBytes = blkRead
	snap.BlkWriteBytes = blkWrite
	for _, d := range devAgg {
		snap.BlockDevices = append(snap.BlockDevices, *d)
	}

	// 速率：与上一次采样差分。
	c.mu.Lock()
	if prev, ok := c.prev[snap.ContainerID]; ok && !prev.at.IsZero() {
		dt := now.Sub(prev.at).Seconds()
		if dt > 0 {
			snap.NetRxRate = ratePerSec(netRx, prev.netRx, dt)
			snap.NetTxRate = ratePerSec(netTx, prev.netTx, dt)
			snap.BlkReadRate = ratePerSec(blkRead, prev.blkRead, dt)
			snap.BlkWriteRate = ratePerSec(blkWrite, prev.blkWrite, dt)
		}
	}
	c.prev[snap.ContainerID] = prevCounters{
		netRx: netRx, netTx: netTx, blkRead: blkRead, blkWrite: blkWrite, at: now,
	}
	c.mu.Unlock()
}

// Operator 提供容器启停/重启/重建能力（FR-13），并执行开关与白名单校验。
type Operator struct {
	client      *Client
	enabled     bool
	grace       int
	allowLabels []string
	denyNames   []string

	// redeploy 相关
	composePrefix   []string // docker compose 命令前缀，如 ["docker","compose"]
	dockerBin       string   // 删除镜像使用的二进制文件，默认 docker
	composeFile     string   // 配置回退的 compose 文件路径
	redeployTimeout time.Duration
}

// OperatorOptions 聚合 Operator 的构造参数，避免过长形参列表。
type OperatorOptions struct {
	Enabled         bool
	Grace           time.Duration
	AllowLabels     []string
	DenyNames       []string
	ComposeCommand  string // "docker compose"；空则默认
	DockerBin       string // 空则默认 "docker"
	ComposeFile     string
	RedeployTimeout time.Duration // 空则默认 5m
}

func NewOperator(client *Client, opts OperatorOptions) *Operator {
	secs := int(opts.Grace.Seconds())
	if secs <= 0 {
		secs = 10
	}
	prefix := strings.Fields(opts.ComposeCommand)
	if len(prefix) == 0 {
		prefix = []string{"docker", "compose"}
	}
	dockerBin := opts.DockerBin
	if dockerBin == "" {
		dockerBin = "docker"
	}
	rt := opts.RedeployTimeout
	if rt <= 0 {
		rt = 5 * time.Minute
	}
	return &Operator{
		client:          client,
		enabled:         opts.Enabled,
		grace:           secs,
		allowLabels:     opts.AllowLabels,
		denyNames:       opts.DenyNames,
		composePrefix:   prefix,
		dockerBin:       dockerBin,
		composeFile:     opts.ComposeFile,
		redeployTimeout: rt,
	}
}

// ErrDisabled 表示写操作未开启。
var ErrDisabled = &OpError{Code: 403, Msg: "容器写操作未启用（docker.operations.enabled=false）"}

// OpError 携带业务错误码。
type OpError struct {
	Code int
	Msg  string
}

func (e *OpError) Error() string { return e.Msg }

// Enabled 报告写操作是否开启。
func (o *Operator) Enabled() bool { return o.enabled }

// Allowed 基于白名单/黑名单判断某容器是否可被操作。
func (o *Operator) Allowed(id string, name string, labels map[string]string) bool {
	for _, dn := range o.denyNames {
		if dn == id || dn == name {
			return false
		}
	}
	if len(o.allowLabels) == 0 {
		return true
	}
	for _, l := range o.allowLabels {
		k, v, hasVal := strings.Cut(l, "=")
		if lv, ok := labels[k]; ok && (!hasVal || lv == v) {
			return true
		}
	}
	return false
}

// Start 启动容器。
func (o *Operator) Start(ctx context.Context, id string) error {
	if !o.enabled {
		return ErrDisabled
	}
	return o.client.StartContainer(ctx, id)
}

// Stop 停止容器。
func (o *Operator) Stop(ctx context.Context, id string) error {
	if !o.enabled {
		return ErrDisabled
	}
	return o.client.StopContainer(ctx, id, o.grace)
}

// Restart 重启容器。
func (o *Operator) Restart(ctx context.Context, id string) error {
	if !o.enabled {
		return ErrDisabled
	}
	return o.client.RestartContainer(ctx, id, o.grace)
}

// Compose 标准标签键（由 docker compose 启动的容器自带）。
const (
	labelComposeService = "com.docker.compose.service"
	labelComposeProject = "com.docker.compose.project"
	labelComposeFiles   = "com.docker.compose.project.config_files"
	labelComposeWorkDir = "com.docker.compose.project.working_dir"
)

// RedeployTimeout 返回单次重建命令的超时时长。
func (o *Operator) RedeployTimeout() time.Duration { return o.redeployTimeout }

// Redeploy 重建/重新部署容器：停止 → 删除容器 → 删除镜像（仅当无其他容器共用时）→ 拉取新镜像 → 启动。
// compose 文件优先从容器标签获取，回退到配置中的 compose_file。
// 删除镜像带共用保护：若镜像仍被其他容器使用则跳过，避免影响别的服务；删除失败亦为非致命（不中断重建）。
// 返回每一步的命令输出，供上层展示执行日志。
func (o *Operator) Redeploy(ctx context.Context, id string) (string, error) {
	if !o.enabled {
		return "", ErrDisabled
	}
	if o.client == nil {
		return "", fmt.Errorf("docker client 未初始化")
	}

	ins, err := o.client.InspectContainer(ctx, id)
	if err != nil {
		return "", err
	}
	labels := ins.Config.Labels
	svc := labels[labelComposeService]
	project := labels[labelComposeProject]
	workDir := labels[labelComposeWorkDir]
	imageRef := ins.Config.Image // 镜像名:tag
	imageID := ins.Image         // 镜像 ID

	files := compactNonEmpty(strings.Split(labels[labelComposeFiles], ","))
	if len(files) == 0 && o.composeFile != "" {
		files = []string{o.composeFile}
	}
	if len(files) == 0 {
		return "", fmt.Errorf("无法定位 docker-compose.yml：容器非 compose 启动且未配置 docker.operations.compose_file")
	}

	gargs := composeGlobalArgs(workDir, files, project)
	var log strings.Builder

	// fatal 执行一个必须成功的步骤，失败则包装错误。
	fatal := func(label string, args []string) error {
		if err := o.runStep(ctx, workDir, args, &log, label); err != nil {
			return fmt.Errorf("%s 失败: %w", label, err)
		}
		return nil
	}

	// 1) 停止
	if err := fatal("compose stop", o.composeCmd(gargs, "stop", nil, svc)); err != nil {
		return log.String(), err
	}
	// 2) 删除容器
	if err := fatal("compose rm", o.composeCmd(gargs, "rm", []string{"-f"}, svc)); err != nil {
		return log.String(), err
	}
	// 3) 删除镜像（仅删该服务镜像；若被其他容器共用则跳过）
	if image := firstNonEmpty(imageRef, imageID); image != "" {
		if inUse, err := o.imageInUse(ctx, imageRef, imageID, id); err != nil {
			fmt.Fprintf(&log, "==> 检查镜像共用情况失败，跳过删除: %v\n", err)
		} else if inUse {
			fmt.Fprintf(&log, "==> 镜像 %s 仍被其他容器使用，跳过删除\n", image)
		} else if err := o.runStep(ctx, workDir, []string{o.dockerBin, "image", "rm", image}, &log, "docker image rm "+image); err != nil {
			fmt.Fprintf(&log, "    删除镜像失败（继续重建）: %v\n", err)
		}
	}
	// 4) 拉取新镜像
	if err := fatal("compose pull", o.composeCmd(gargs, "pull", nil, svc)); err != nil {
		return log.String(), err
	}
	// 5) 启动
	if err := fatal("compose up", o.composeCmd(gargs, "up", []string{"-d", "--force-recreate"}, svc)); err != nil {
		return log.String(), err
	}

	return log.String(), nil
}

// imageInUse 检查除目标容器外，是否仍有其他容器使用给定镜像（按镜像名或 ID 匹配）。
// 目标容器此时已被 compose rm 删除，excludeID 作为兜底避免误判。
func (o *Operator) imageInUse(ctx context.Context, imageRef, imageID, excludeID string) (bool, error) {
	list, err := o.client.ListContainers(ctx, true)
	if err != nil {
		return false, err
	}
	exc := shortID(excludeID)
	for _, c := range list {
		if c.ID == excludeID || shortID(c.ID) == exc {
			continue
		}
		if (imageRef != "" && c.Image == imageRef) || (imageID != "" && c.ImageID == imageID) {
			return true, nil
		}
	}
	return false, nil
}

// firstNonEmpty 返回第一个非空字符串。
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// composeCmd 拼装完整 docker compose 命令：前缀 + 全局参数 + 动作(+flags)[+service]。
func (o *Operator) composeCmd(global []string, action string, flags []string, svc string) []string {
	args := append(append([]string{}, o.composePrefix...), global...)
	args = append(args, action)
	args = append(args, flags...)
	if svc != "" {
		args = append(args, svc)
	}
	return args
}

// runStep 执行一条命令，输出写入 log；每一步独立受 redeployTimeout 约束。
func (o *Operator) runStep(ctx context.Context, dir string, args []string, log *strings.Builder, label string) error {
	fmt.Fprintf(log, "==> %s: %s\n", label, strings.Join(args, " "))
	if len(args) == 0 {
		return nil
	}
	cctx, cancel := context.WithTimeout(ctx, o.redeployTimeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, args[0], args[1:]...)
	if dir != "" {
		cmd.Dir = dir
	}
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	log.WriteString(strings.TrimRight(buf.String(), "\n"))
	log.WriteString("\n")
	if err != nil {
		fmt.Fprintf(log, "    步骤返回错误: %v\n", err)
		return err
	}
	return nil
}

// composeGlobalArgs 拼装 compose 全局参数（工作目录 / 配置文件 / 项目名）。
func composeGlobalArgs(workDir string, files []string, project string) []string {
	var args []string
	if workDir != "" {
		args = append(args, "--project-directory", workDir)
	}
	for _, f := range files {
		args = append(args, "-f", f)
	}
	if project != "" {
		args = append(args, "-p", project)
	}
	return args
}

// compactNonEmpty 就地去除空白项并 trim。
func compactNonEmpty(in []string) []string {
	out := in[:0]
	for _, s := range in {
		if t := strings.TrimSpace(s); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// Labels 读取容器标签，供白名单校验使用。
func (o *Operator) Labels(ctx context.Context, id string) (map[string]string, error) {
	ins, err := o.client.InspectContainer(ctx, id)
	if err != nil {
		return nil, err
	}
	return ins.Config.Labels, nil
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func trimName(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return strings.TrimPrefix(names[0], "/")
}

func subU64(a, b uint64) uint64 {
	if a >= b {
		return a - b
	}
	return 0
}

func ratePerSec(cur, prev uint64, dt float64) float64 {
	return round2(float64(subU64(cur, prev)) / dt)
}

func round2(v float64) float64 {
	return float64(int64(v*100+0.5)) / 100
}
