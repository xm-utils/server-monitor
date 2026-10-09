package docker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewClientURL(t *testing.T) {
	c := NewClient("tcp://1.2.3.4:2375", "v1.43", 0)
	if c == nil {
		t.Fatal("tcp endpoint 应成功创建 client")
	}
	if got, want := c.url("/containers/json", nil), "http://1.2.3.4:2375/v1.43/containers/json"; got != want {
		t.Errorf("url 组合异常: got %s want %s", got, want)
	}

	u := NewClient("unix:///var/run/docker.sock", "v1.41", time.Second)
	if u == nil {
		t.Fatal("unix endpoint 应成功创建 client")
	}
	if got := u.url("/_ping", nil); got != "http://docker/v1.41/_ping" {
		t.Errorf("unix url 组合异常: %s", got)
	}
}

const inspectJSON = `{
  "Id": "abcdef1234567890",
  "Name": "/cashloan-api",
  "State": {"Status":"running","Running":true,"StartedAt":"2026-10-08T09:00:00.123456789Z","Health":{"Status":"healthy"}},
  "RestartCount": 2,
  "Config": {"Image":"registry/api:v1","Labels":{"app":"cashloan"},"Env":["A=1","B=2"]},
  "HostConfig": {"Memory": 536870912},
  "Mounts": [{"Type":"bind","Source":"/data","Destination":"/mnt","RW":true}],
  "NetworkSettings": {"Ports": {"8080/tcp":[{"HostIp":"0.0.0.0","HostPort":"18080"}]}}
}`

const statsJSON = `{
  "cpu_stats": {"cpu_usage":{"total_usage":2000000000,"kernel_mode":100,"usermode":200},"system_cpu_usage":10000000000,"online_cpus":2},
  "precpu_stats": {"cpu_usage":{"total_usage":1000000000},"system_cpu_usage":5000000000},
  "memory_stats": {"usage":268435456,"limit":536870912},
  "networks": {"eth0":{"rx_bytes":1000,"tx_bytes":2000}},
  "blkio_stats": {"io_service_bytes_recursive":[{"op":"Read","device":"8:16","value":4096},{"op":"Write","device":"8:16","value":8192}]},
  "pids_stats": {"current":5}
}`

func newMockDocker(t *testing.T) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1.43/_ping", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("OK"))
	})
	mux.HandleFunc("/v1.43/containers/abc/json", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(inspectJSON))
	})
	mux.HandleFunc("/v1.43/containers/abc/stats", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("stream") != "false" {
			t.Errorf("stats 应带 stream=false")
		}
		_, _ = w.Write([]byte(statsJSON))
	})
	mux.HandleFunc("/v1.43/containers/abc/restart", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("t"); got != "10" {
			t.Errorf("restart 查询参数 t 期望 10, got %q", got)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	return httptest.NewServer(mux)
}

func TestCollectOne(t *testing.T) {
	srv := newMockDocker(t)
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "http://")
	client := NewClient("tcp://"+host, "v1.43", 2*time.Second)
	col := NewCollector(client)

	snap, err := col.CollectOne(context.Background(), "abc")
	if err != nil {
		t.Fatalf("CollectOne 失败: %v", err)
	}
	if snap.Name != "cashloan-api" {
		t.Errorf("Name 期望 cashloan-api, got %s", snap.Name)
	}
	if snap.ContainerID != "abcdef123456" {
		t.Errorf("ContainerID 应为 12 位短 ID, got %s", snap.ContainerID)
	}
	if snap.Health != "healthy" || snap.RestartCount != 2 {
		t.Errorf("inspect 字段异常: health=%s restart=%d", snap.Health, snap.RestartCount)
	}
	// CPU%: (2e9-1e9)/(10e9-5e9)*online(2)*100 = 0.2*2*100 = 40
	if snap.CPUPercent != 40 {
		t.Errorf("CPUPercent 期望 40, got %v", snap.CPUPercent)
	}
	// 内存 256Mi/512Mi = 50%
	if snap.MemPercent != 50 {
		t.Errorf("MemPercent 期望 50, got %v", snap.MemPercent)
	}
	if snap.BlkReadBytes != 4096 || snap.BlkWriteBytes != 8192 {
		t.Errorf("块设备累计异常: r=%d w=%d", snap.BlkReadBytes, snap.BlkWriteBytes)
	}
	if snap.PidsCurrent != 5 {
		t.Errorf("PidsCurrent 期望 5, got %d", snap.PidsCurrent)
	}
	if len(snap.Env) != 2 || len(snap.Mounts) != 1 || snap.PortBindings["8080/tcp"] == nil {
		t.Errorf("详情字段异常: env=%d mounts=%d ports=%v", len(snap.Env), len(snap.Mounts), snap.PortBindings)
	}
}

func TestOperatorRestartAndDisabled(t *testing.T) {
	srv := newMockDocker(t)
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")
	client := NewClient("tcp://"+host, "v1.43", 2*time.Second)

	// 开启状态：grace=10s -> 查询参数 t=10
	op := NewOperator(client, OperatorOptions{Enabled: true, Grace: 10 * time.Second})
	if err := op.Restart(context.Background(), "abc"); err != nil {
		t.Fatalf("Restart 失败: %v", err)
	}

	// 关闭状态：应返回 ErrDisabled 且不发请求
	disabled := NewOperator(client, OperatorOptions{Enabled: false, Grace: 10 * time.Second})
	err := disabled.Restart(context.Background(), "abc")
	if err != ErrDisabled {
		t.Fatalf("未启用时应返回 ErrDisabled, got %v", err)
	}
	if _, ok := err.(*OpError); !ok {
		t.Fatalf("ErrDisabled 应为 *OpError, got %T", err)
	}
}

func TestOperatorAllowed(t *testing.T) {
	op := NewOperator(nil, OperatorOptions{
		Enabled: true, Grace: 10 * time.Second,
		AllowLabels: []string{"app=cashloan"}, DenyNames: []string{"mysql-prod"},
	})

	if op.Allowed("mysql-prod", "mysql-prod", map[string]string{"app": "cashloan"}) {
		t.Error("黑名单容器不应被允许")
	}
	if !op.Allowed("c1", "api", map[string]string{"app": "cashloan"}) {
		t.Error("匹配白名单 label 应被允许")
	}
	if op.Allowed("c2", "other", map[string]string{"app": "nope"}) {
		t.Error("不匹配白名单 label 不应被允许")
	}

	// 无白名单时除黑名单外均允许
	op2 := NewOperator(nil, OperatorOptions{Enabled: true, Grace: 10 * time.Second})
	if !op2.Allowed("c3", "any", nil) {
		t.Error("无白名单应允许")
	}
}

func TestPing(t *testing.T) {
	srv := newMockDocker(t)
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")
	client := NewClient("tcp://"+host, "v1.43", 2*time.Second)
	if err := client.Ping(context.Background()); err != nil {
		t.Fatalf("Ping 应成功: %v", err)
	}
}

func TestRedeployGuards(t *testing.T) {
	// 未启用：不访问 client 即返回 ErrDisabled
	disabled := NewOperator(nil, OperatorOptions{Enabled: false})
	if _, err := disabled.Redeploy(context.Background(), "abc"); err != ErrDisabled {
		t.Fatalf("未启用时 Redeploy 应返回 ErrDisabled, got %v", err)
	}

	// 已启用但容器无 compose 标签且未配置 compose_file：应报“无法定位”
	srv := newMockDocker(t) // inspectJSON 仅含 app=cashloan 标签
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")
	client := NewClient("tcp://"+host, "v1.43", 2*time.Second)
	op := NewOperator(client, OperatorOptions{Enabled: true})
	_, err := op.Redeploy(context.Background(), "abc")
	if err == nil || !strings.Contains(err.Error(), "无法定位") {
		t.Fatalf("无 compose 信息时应报无法定位错误, got %v", err)
	}
}

func TestComposeGlobalArgsAndCmd(t *testing.T) {
	op := NewOperator(nil, OperatorOptions{Enabled: true, ComposeCommand: "docker compose"})
	gargs := composeGlobalArgs("/srv/app", []string{"docker-compose.yml", "override.yml"}, "proj")
	got := strings.Join(gargs, " ")
	want := "--project-directory /srv/app -f docker-compose.yml -f override.yml -p proj"
	if got != want {
		t.Errorf("composeGlobalArgs:\n got=%q\nwant=%q", got, want)
	}

	cmd := op.composeCmd(gargs, "up", []string{"-d", "--force-recreate"}, "api")
	joined := strings.Join(cmd, " ")
	if !strings.HasPrefix(joined, "docker compose ") || !strings.HasSuffix(joined, " up -d --force-recreate api") {
		t.Errorf("composeCmd 拼装异常: %s", joined)
	}

	// 服务名为空时不应有尾部空参数
	if joined := strings.Join(op.composeCmd(gargs, "stop", nil, ""), " "); !strings.HasSuffix(joined, "stop") {
		t.Errorf("svc 为空时应以 stop 结尾且无空参数: %q", joined)
	}
}

func TestImageInUse(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1.43/containers/json", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[
			{"Id":"deadbeefdeadbeef","Names":["/other"],"Image":"registry/api:v1","ImageID":"sha256:aaa"},
			{"Id":"abcdef1234567890","Names":["/target"],"Image":"registry/api:v1","ImageID":"sha256:aaa"}
		]`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")
	client := NewClient("tcp://"+host, "v1.43", 2*time.Second)
	op := NewOperator(client, OperatorOptions{Enabled: true})

	// 排除目标后仍有 other 使用同一镜像 -> 共用
	if inUse, err := op.imageInUse(context.Background(), "registry/api:v1", "sha256:aaa", "abcdef1234567890"); err != nil || !inUse {
		t.Errorf("期望共用 true, got %v %v", inUse, err)
	}
	// 按 ImageID 匹配同样命中
	if inUse, err := op.imageInUse(context.Background(), "", "sha256:aaa", "abcdef1234567890"); err != nil || !inUse {
		t.Errorf("按 ImageID 期望共用 true, got %v %v", inUse, err)
	}
	// 不同镜像 -> 不共用
	if inUse, err := op.imageInUse(context.Background(), "other:image:tag", "sha256:zzz", "abcdef1234567890"); err != nil || inUse {
		t.Errorf("不同镜像应不共用, got %v %v", inUse, err)
	}
}

func TestCompactNonEmpty(t *testing.T) {
	got := compactNonEmpty(strings.Split("", ","))
	if len(got) != 0 {
		t.Errorf("空字符串应得到空切片, got %v", got)
	}
	got = compactNonEmpty(strings.Split("a, ,b,", ","))
	if strings.Join(got, ",") != "a,b" {
		t.Errorf("compactNonEmpty 异常: %v", got)
	}
}
