package model

// SystemSnapshot 宿主机一次运行指标快照（需求说明书 4.1）。
// JSON 字段采用 lowerCamelCase；时间戳统一 UnixSeconds。
type SystemSnapshot struct {
	Timestamp   int64   `json:"timestamp"`
	Hostname    string  `json:"hostname"`
	OS          string  `json:"os"`
	KernelVer   string  `json:"kernelVersion"`
	BootTime    int64   `json:"bootTime"`
	UptimeSec   uint64  `json:"uptime"`
	Procs       int     `json:"procs"`

	CPU      CPUMetric     `json:"cpu"`
	Memory   MemoryMetric  `json:"memory"`
	Disks    []DiskStat    `json:"disks"`
	DiskIO   DiskIOStat    `json:"diskIO"`
	Networks []NetStat     `json:"networks"`
	NetSpeed NetSpeedStat  `json:"netSpeed"`
	Conns    ConnStats     `json:"connStats"`
	TopCPU   []ProcessStat `json:"topCpuProcesses"`
	TopMem   []ProcessStat `json:"topMemProcesses"`
}

type CPUMetric struct {
	UsageTotal   float64   `json:"usageTotal"`
	UsagePerCore []float64 `json:"usagePerCore"`
	Cores        int       `json:"cores"`
	Load1        float64   `json:"load1"`
	Load5        float64   `json:"load5"`
	Load15       float64   `json:"load15"`
}

type MemoryMetric struct {
	TotalBytes       uint64  `json:"total"`
	UsedBytes        uint64  `json:"used"`
	AvailableBytes   uint64  `json:"available"`
	UsedPercent      float64 `json:"usedPercent"`
	SwapTotalBytes   uint64  `json:"swapTotal"`
	SwapUsedBytes    uint64  `json:"swapUsed"`
	SwapFreeBytes    uint64  `json:"swapFree"`
}

type DiskStat struct {
	Path        string  `json:"path"`
	Fstype      string  `json:"fstype"`
	TotalBytes  uint64  `json:"total"`
	UsedBytes   uint64  `json:"used"`
	FreeBytes   uint64  `json:"free"`
	UsedPercent float64 `json:"usedPercent"`
}

type DiskIOStat struct {
	ReadBytes  float64 `json:"readBytes"`
	WriteBytes float64 `json:"writeBytes"`
	ReadPerSec float64 `json:"readPerSec"`
	WritePerSec float64 `json:"writePerSec"`
}

type NetStat struct {
	Name      string `json:"name"`
	BytesSent uint64 `json:"bytesSent"`
	BytesRecv uint64 `json:"bytesRecv"`
	PacketsSent uint64 `json:"packetsSent"`
	PacketsRecv uint64 `json:"packetsRecv"`
	Errin  uint64 `json:"errin"`
	Errout uint64 `json:"errout"`
}

type NetSpeedStat struct {
	RateInBytes  float64 `json:"rateIn"`
	RateOutBytes float64 `json:"rateOut"`
}

type ConnStats struct {
	Total      int `json:"total"`
	Established int `json:"established"`
	Listen     int `json:"listen"`
}

type ProcessStat struct {
	PID         int32   `json:"pid"`
	Name        string  `json:"name"`
	CPUPercent  float64 `json:"cpuPercent"`
	MemPercent  float64 `json:"memPercent"`
	RssBytes    uint64  `json:"rssBytes"`
}
