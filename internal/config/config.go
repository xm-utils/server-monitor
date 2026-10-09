package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xm-utils/tools/common"
	"gopkg.in/yaml.v3"
)

// Config 服务总配置，对应需求说明书 FR-01。
type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Admin     AdminConfig     `yaml:"admin"`
	Collector CollectorConfig `yaml:"collector"`
	Docker    DockerConfig    `yaml:"docker"`
	Log       LogConfig       `yaml:"log"`
	Web       WebConfig       `yaml:"web"`
}

type ServerConfig struct {
	ListenAddr        int           `yaml:"listen_addr"`
	GrpcAddr          int           `yaml:"grpc_addr"`
	AdminURL          string        `yaml:"admin_url"`
	HeartbeatInterval time.Duration `yaml:"heartbeat_interval"`
	ReadTimeout       time.Duration `yaml:"read_timeout"`
	WriteTimeout      time.Duration `yaml:"write_timeout"`
}

type AdminConfig struct {
	ListenAddr       int           `yaml:"listen_addr"`
	GrpcAddr         int           `yaml:"grpc_addr"`
	PollInterval     time.Duration `yaml:"poll_interval"`
	HeartbeatTimeout time.Duration `yaml:"heartbeat_timeout"`
	PollTimeout      time.Duration `yaml:"poll_timeout"`
}

type CollectorConfig struct {
	SystemInterval time.Duration `yaml:"system_interval"`
	DockerInterval time.Duration `yaml:"docker_interval"`
	HistorySize    int           `yaml:"history_size"`
}

type DockerConfig struct {
	Endpoint   string           `yaml:"endpoint"`
	Timeout    time.Duration    `yaml:"timeout"`
	APIVersion string           `yaml:"api_version"`
	Operations OperationsConfig `yaml:"operations"`
}

// OperationsConfig 对应 FR-13 容器启停/重启的安全开关与白名单。
type OperationsConfig struct {
	Enabled        bool          `yaml:"enabled"`
	DefaultTimeout time.Duration `yaml:"default_timeout"`
	AllowLabels    []string      `yaml:"allow_labels"`
	DenyNames      []string      `yaml:"deny_names"`
	// 重建/重新部署（redeploy）相关：停止→删除容器→删除镜像→拉取新镜像→启动。
	ComposeCommand  string        `yaml:"compose_command"`  // docker compose 命令前缀，默认 "docker compose"
	DockerBin       string        `yaml:"docker_bin"`       // 删除镜像使用的 docker 可执行文件，默认 "docker"
	ComposeFile     string        `yaml:"compose_file"`     // 配置回退：当容器非 compose 启动时使用的 docker-compose.yml 路径
	RedeployTimeout time.Duration `yaml:"redeploy_timeout"` // 单次重建命令超时，默认 5m
}

type LogConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

type WebConfig struct {
	RefreshIntervalMs int    `yaml:"refresh_interval_ms"`
	TemplateDir       string `yaml:"template_dir"`
	StaticDir         string `yaml:"static_dir"`
}

// Default 返回带合理默认值的配置。
func Default() *Config {
	return &Config{
		Server: ServerConfig{
			ListenAddr:        8080,
			GrpcAddr:          8081,
			AdminURL:          "127.0.0.1:9091",
			HeartbeatInterval: 30 * time.Second,
			ReadTimeout:       10 * time.Second,
			WriteTimeout:      30 * time.Second,
		},
		Admin: AdminConfig{
			ListenAddr:       9090,
			GrpcAddr:         9091,
			PollInterval:     10 * time.Second,
			HeartbeatTimeout: 90 * time.Second,
			PollTimeout:      5 * time.Second,
		},
		Collector: CollectorConfig{
			SystemInterval: 3 * time.Second,
			DockerInterval: 5 * time.Second,
			HistorySize:    300,
		},
		Docker: DockerConfig{
			Endpoint:   "unix:///var/run/docker.sock",
			Timeout:    3 * time.Second,
			APIVersion: "v1.43",
			Operations: OperationsConfig{
				Enabled:         false,
				DefaultTimeout:  10 * time.Second,
				ComposeCommand:  "docker compose",
				DockerBin:       "docker",
				RedeployTimeout: 5 * time.Minute,
			},
		},
		Log: LogConfig{Level: "info", Format: "text"},
		Web: WebConfig{
			RefreshIntervalMs: 5000,
			TemplateDir:       "./templates",
			StaticDir:         "./static",
		},
	}
}

// Load 按优先级查找配置文件并加载，随后应用环境变量覆盖（前缀 SMS_）。
// 查找顺序：显式 path -> /etc/server_monitor/config.yaml -> ./config.yaml。
func Load(path string) (*Config, error) {
	cfg := Default()

	candidates := []string{}
	if path != "" {
		candidates = append(candidates, path)
	} else {
		candidates = append(candidates,
			"/etc/server_monitor/config.yaml",
			"./config.yaml",
		)
	}

	for _, p := range candidates {
		if b, err := os.ReadFile(p); err == nil {
			if err := yaml.Unmarshal(b, cfg); err != nil {
				return nil, fmt.Errorf("解析配置文件 %s 失败: %w", p, err)
			}
			break
		} else if path != "" && os.IsNotExist(err) {
			return nil, fmt.Errorf("配置文件不存在: %s", path)
		}
	}

	applyEnvOverrides(cfg)
	return cfg, nil
}

// applyEnvOverrides 使用 SMS_ 前缀环境变量覆盖关键字段。
func applyEnvOverrides(cfg *Config) {
	get := func(k string) (string, bool) { return os.LookupEnv("SMS_" + k) }

	if v, ok := get("LISTEN_ADDR"); ok {
		cfg.Server.ListenAddr = common.StringToInt(v)
	}
	if v, ok := get("LOG_LEVEL"); ok {
		cfg.Log.Level = v
	}
	if v, ok := get("DOCKER_ENDPOINT"); ok {
		cfg.Docker.Endpoint = v
	}
	if v, ok := get("DOCKER_OPERATIONS_ENABLED"); ok {
		cfg.Docker.Operations.Enabled = strings.EqualFold(v, "true") || v == "1"
	}
	if v, ok := get("TEMPLATE_DIR"); ok {
		cfg.Web.TemplateDir = v
	}
	if v, ok := get("STATIC_DIR"); ok {
		cfg.Web.StaticDir = v
	}
}

// ResolveTemplateGlob 返回模板 glob 路径。
func (c *Config) ResolveTemplateGlob() string {
	return filepath.Join(c.Web.TemplateDir, "*.html")
}
