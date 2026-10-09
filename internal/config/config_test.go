package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefault(t *testing.T) {
	c := Default()
	if c.Server.ListenAddr != 8080 {
		t.Fatalf("默认监听端口异常: %d", c.Server.ListenAddr)
	}
	if c.Collector.SystemInterval != 3*time.Second {
		t.Fatalf("默认系统采集周期异常: %v", c.Collector.SystemInterval)
	}
	if c.Docker.Operations.Enabled {
		t.Fatal("容器写操作默认应关闭")
	}
	if c.Collector.HistorySize != 300 {
		t.Fatalf("默认历史条数异常: %d", c.Collector.HistorySize)
	}
}

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	content := `
server:
  listen_addr: 9090
collector:
  system_interval: 2s
  history_size: 50
docker:
  endpoint: "tcp://127.0.0.1:2375"
  operations:
    enabled: true
    allow_labels: ["app=demo"]
log:
  level: debug
`
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if c.Server.ListenAddr != 9090 || c.Collector.HistorySize != 50 {
		t.Fatalf("文件加载异常: %+v", c.Server)
	}
	if c.Collector.SystemInterval != 2*time.Second {
		t.Fatalf("周期解析异常: %v", c.Collector.SystemInterval)
	}
	if !c.Docker.Operations.Enabled || len(c.Docker.Operations.AllowLabels) != 1 {
		t.Fatalf("operations 解析异常: %+v", c.Docker.Operations)
	}
	// 未覆盖字段应保留默认值
	if c.Collector.DockerInterval != 5*time.Second {
		t.Fatalf("未覆盖字段应保留默认: %v", c.Collector.DockerInterval)
	}
}

func TestEnvOverride(t *testing.T) {
	t.Setenv("SMS_LISTEN_ADDR", "7777")
	t.Setenv("SMS_DOCKER_OPERATIONS_ENABLED", "true")

	c, err := Load("")
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if c.Server.ListenAddr != 7777 {
		t.Fatalf("环境变量覆盖失败: %d", c.Server.ListenAddr)
	}
	if !c.Docker.Operations.Enabled {
		t.Fatal("SMS_DOCKER_OPERATIONS_ENABLED 应开启写操作")
	}
}

func TestLoadMissingExplicitPath(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.yaml")); err == nil {
		t.Fatal("显式路径不存在时应返回错误")
	}
}

func TestResolveTemplateGlob(t *testing.T) {
	c := Default()
	c.Web.TemplateDir = "/tmp/tpl"
	if got := c.ResolveTemplateGlob(); got != "/tmp/tpl/*.html" {
		t.Fatalf("模板 glob 异常: %s", got)
	}
}
