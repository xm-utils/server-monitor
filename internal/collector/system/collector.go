package system

import (
	"sort"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	gpsnet "github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/process"

	"server_monitor_service/internal/model"
)

// Collector 采集宿主机整体运行指标（FR-02）。
// 内部维护上一次网络/磁盘累计值用于计算速率。
type Collector struct {
	mu        sync.Mutex
	prevNet   map[string]gpsnet.IOCountersStat
	prevNetAt time.Time
	prevDisk  map[string]disk.IOCountersStat
	prevDiskAt time.Time

	topN int
}

func NewCollector() *Collector {
	return &Collector{
		prevNet: map[string]gpsnet.IOCountersStat{},
		topN:    10,
	}
}

// Collect 采集一次系统快照，单项失败不影响其余字段（返回零值）。
func (c *Collector) Collect() model.SystemSnapshot {
	snap := model.SystemSnapshot{Timestamp: time.Now().Unix()}

	if info, err := host.Info(); err == nil {
		snap.Hostname = info.Hostname
		snap.OS = info.OS
		snap.KernelVer = info.KernelVersion
		snap.BootTime = int64(info.BootTime)
		snap.UptimeSec = info.Uptime
		snap.Procs = int(info.Procs)
	}

	c.fillCPU(&snap)
	c.fillMemory(&snap)
	c.fillDisks(&snap)
	c.fillDiskIO(&snap)
	c.fillNetwork(&snap)
	c.fillConns(&snap)
	c.fillTopProcesses(&snap)

	return snap
}

func (c *Collector) fillCPU(snap *model.SystemSnapshot) {
	snap.CPU.Cores, _ = cpu.Counts(true)
	if per, err := cpu.Percent(0, false); err == nil && len(per) > 0 {
		snap.CPU.UsageTotal = round2(per[0])
	}
	if perCore, err := cpu.Percent(0, true); err == nil {
		snap.CPU.UsagePerCore = make([]float64, len(perCore))
		for i, v := range perCore {
			snap.CPU.UsagePerCore[i] = round2(v)
		}
	}
	if avg, err := load.Avg(); err == nil && avg != nil {
		snap.CPU.Load1 = round2(avg.Load1)
		snap.CPU.Load5 = round2(avg.Load5)
		snap.CPU.Load15 = round2(avg.Load15)
	}
}

func (c *Collector) fillMemory(snap *model.SystemSnapshot) {
	if vm, err := mem.VirtualMemory(); err == nil && vm != nil {
		snap.Memory.TotalBytes = vm.Total
		snap.Memory.UsedBytes = vm.Used
		snap.Memory.AvailableBytes = vm.Available
		snap.Memory.UsedPercent = round2(vm.UsedPercent)
	}
	if sw, err := mem.SwapMemory(); err == nil && sw != nil {
		snap.Memory.SwapTotalBytes = sw.Total
		snap.Memory.SwapUsedBytes = sw.Used
		snap.Memory.SwapFreeBytes = sw.Free
	}
}

func (c *Collector) fillDisks(snap *model.SystemSnapshot) {
	parts, err := disk.Partitions(false)
	if err != nil {
		return
	}
	for _, p := range parts {
		usage, err := disk.Usage(p.Mountpoint)
		if err != nil || usage == nil {
			continue
		}
		snap.Disks = append(snap.Disks, model.DiskStat{
			Path:        p.Mountpoint,
			Fstype:      p.Fstype,
			TotalBytes:  usage.Total,
			UsedBytes:   usage.Used,
			FreeBytes:   usage.Free,
			UsedPercent: round2(usage.UsedPercent),
		})
	}
}

func (c *Collector) fillDiskIO(snap *model.SystemSnapshot) {
	counters, err := disk.IOCounters()
	if err != nil {
		return
	}
	now := time.Now()

	var totRead, totWrite uint64
	for _, io := range counters {
		totRead += io.ReadBytes
		totWrite += io.WriteBytes
	}
	snap.DiskIO.ReadBytes = float64(totRead)
	snap.DiskIO.WriteBytes = float64(totWrite)

	c.mu.Lock()
	if c.prevDisk != nil && !c.prevDiskAt.IsZero() {
		dt := now.Sub(c.prevDiskAt).Seconds()
		if dt > 0 {
			var pr, pw uint64
			for _, io := range c.prevDisk {
				pr += io.ReadBytes
				pw += io.WriteBytes
			}
			if totRead >= pr {
				snap.DiskIO.ReadPerSec = round2(float64(totRead-pr) / dt)
			}
			if totWrite >= pw {
				snap.DiskIO.WritePerSec = round2(float64(totWrite-pw) / dt)
			}
		}
	}
	c.prevDisk = counters
	c.prevDiskAt = now
	c.mu.Unlock()
}

func (c *Collector) fillNetwork(snap *model.SystemSnapshot) {
	counters, err := gpsnet.IOCounters(true)
	if err != nil {
		return
	}
	now := time.Now()

	var rateIn, rateOut float64
	c.mu.Lock()
	dt := now.Sub(c.prevNetAt).Seconds()
	for _, io := range counters {
		snap.Networks = append(snap.Networks, model.NetStat{
			Name:        io.Name,
			BytesSent:   io.BytesSent,
			BytesRecv:   io.BytesRecv,
			PacketsSent: io.PacketsSent,
			PacketsRecv: io.PacketsRecv,
			Errin:       io.Errin,
			Errout:      io.Errout,
		})
		if dt > 0 && !c.prevNetAt.IsZero() {
			if prev, ok := c.prevNet[io.Name]; ok {
				if io.BytesRecv >= prev.BytesRecv {
					rateIn += float64(io.BytesRecv - prev.BytesRecv)
				}
				if io.BytesSent >= prev.BytesSent {
					rateOut += float64(io.BytesSent - prev.BytesSent)
				}
			}
		}
	}
	if dt > 0 {
		snap.NetSpeed.RateInBytes = round2(rateIn / dt)
		snap.NetSpeed.RateOutBytes = round2(rateOut / dt)
	}
	c.prevNet = make(map[string]gpsnet.IOCountersStat, len(counters))
	for _, io := range counters {
		c.prevNet[io.Name] = io
	}
	c.prevNetAt = now
	c.mu.Unlock()
}

func (c *Collector) fillConns(snap *model.SystemSnapshot) {
	conns, err := gpsnet.Connections("tcp")
	if err != nil {
		return
	}
	snap.Conns.Total = len(conns)
	for _, conn := range conns {
		switch conn.Status {
		case "ESTABLISHED":
			snap.Conns.Established++
		case "LISTEN":
			snap.Conns.Listen++
		}
	}
}

func (c *Collector) fillTopProcesses(snap *model.SystemSnapshot) {
	procs, err := process.Processes()
	if err != nil {
		return
	}
	stats := make([]model.ProcessStat, 0, len(procs))
	for _, p := range procs {
		name, _ := p.Name()
		cpuPct, _ := p.CPUPercent()
		memPct, _ := p.MemoryPercent()
		var rss uint64
		if mi, err := p.MemoryInfo(); err == nil && mi != nil {
			rss = mi.RSS
		}
		stats = append(stats, model.ProcessStat{
			PID:        p.Pid,
			Name:       name,
			CPUPercent: round2(cpuPct),
			MemPercent: round2(float64(memPct)),
			RssBytes:   rss,
		})
	}

	byCPU := append([]model.ProcessStat(nil), stats...)
	sort.Slice(byCPU, func(i, j int) bool { return byCPU[i].CPUPercent > byCPU[j].CPUPercent })
	snap.TopCPU = top(byCPU, c.topN)

	byMem := append([]model.ProcessStat(nil), stats...)
	sort.Slice(byMem, func(i, j int) bool { return byMem[i].RssBytes > byMem[j].RssBytes })
	snap.TopMem = top(byMem, c.topN)
}

func top(s []model.ProcessStat, n int) []model.ProcessStat {
	if len(s) > n {
		s = s[:n]
	}
	out := make([]model.ProcessStat, len(s))
	copy(out, s)
	return out
}

func round2(v float64) float64 {
	return float64(int64(v*100+0.5)) / 100
}
