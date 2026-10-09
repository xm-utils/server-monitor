package model

// DockerSnapshot 单个容器一次运行指标快照（需求说明书 4.2）。
type DockerSnapshot struct {
	Timestamp    int64  `json:"timestamp"`
	ContainerID  string `json:"containerId"`
	Name         string `json:"name"`
	Image        string `json:"image"`
	ImageID      string `json:"imageId"`
	State        string `json:"state"`
	Status       string `json:"status"`
	Created      int64  `json:"created"`
	StartedAt    int64  `json:"startedAt"`
	RestartCount int    `json:"restartCount"`

	CPUPercent     float64 `json:"cpuPercent"`
	CPUKernelMode  uint64  `json:"cpuKernelMode"`
	CPUUserMode    uint64  `json:"cpuUserMode"`
	MemUsageBytes  uint64  `json:"memUsage"`
	MemLimitBytes  uint64  `json:"memLimit"`
	MemPercent     float64 `json:"memPercent"`

	NetRxBytes   uint64  `json:"netRx"`
	NetTxBytes   uint64  `json:"netTx"`
	NetRxRate    float64 `json:"netRxRate"`
	NetTxRate    float64 `json:"netTxRate"`
	BlkReadBytes  uint64  `json:"blkRead"`
	BlkWriteBytes uint64  `json:"blkWrite"`
	BlkReadRate   float64 `json:"blkReadRate"`
	BlkWriteRate  float64 `json:"blkWriteRate"`

	PidsCurrent  uint64            `json:"pidsCurrent"`
	BlockDevices []BlockDeviceStat `json:"blockDevices,omitempty"`
	Networks     map[string]NetStat `json:"networksMap,omitempty"`
	Health       string            `json:"health"`

	// 详情用（列表页可空）
	Env               []string            `json:"env,omitempty"`
	Mounts            []Mount             `json:"mounts,omitempty"`
	PortBindings      map[string][]string `json:"portBindings,omitempty"`
	NetworkInterfaces []NetworkInterface  `json:"networkInterfaces,omitempty"`
}

type BlockDeviceStat struct {
	Device     string `json:"device"`
	ReadBytes  uint64 `json:"readBytes"`
	WriteBytes uint64 `json:"writeBytes"`
}

type Mount struct {
	Type        string `json:"type"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	RW          bool   `json:"rw"`
}

// NetworkInterface 容器网卡信息。
type NetworkInterface struct {
	Name       string `json:"name"`
	IPAddress  string `json:"ipAddress"`
	Gateway    string `json:"gateway"`
	MacAddress string `json:"macAddress"`
}

// DockerListResult Docker 页/接口聚合结果。
type DockerListResult struct {
	Available bool             `json:"available"`
	Message   string           `json:"message,omitempty"`
	Containers []DockerSnapshot `json:"containers"`
}
