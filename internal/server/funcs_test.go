package server

import (
	"strings"
	"testing"

	"server_monitor_service/internal/model"
)

func TestHumanizeBytes(t *testing.T) {
	cases := []struct {
		in   uint64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KiB"},
		{1536, "1.5 KiB"},
		{1024 * 1024, "1.0 MiB"},
		{uint64(3) * 1024 * 1024 * 1024, "3.0 GiB"},
	}
	for _, c := range cases {
		if got := humanizeBytes(c.in); got != c.want {
			t.Errorf("humanizeBytes(%d)=%q, 期望 %q", c.in, got, c.want)
		}
	}
}

func TestHumanizeRate(t *testing.T) {
	if got := humanizeRate(0); got != "0 B/s" {
		t.Errorf("humanizeRate(0)=%q", got)
	}
	if got := humanizeRate(2048); got != "2.0 KiB/s" {
		t.Errorf("humanizeRate(2048)=%q", got)
	}
}

func TestHumanizeDuration(t *testing.T) {
	cases := []struct {
		in   uint64
		want string
	}{
		{45, "45s"},
		{120, "2m"},
		{3600, "1h"},
		{90061, "1d 1h 1m"},
	}
	for _, c := range cases {
		if got := humanizeDuration(c.in); got != c.want {
			t.Errorf("humanizeDuration(%d)=%q, 期望 %q", c.in, got, c.want)
		}
	}
}

func TestLevelClass(t *testing.T) {
	if cpuClass(50) != "is-ok" || cpuClass(80) != "is-warn" || cpuClass(95) != "is-danger" {
		t.Errorf("cpuClass 分级异常: %s %s %s", cpuClass(50), cpuClass(80), cpuClass(95))
	}
	if memClass(95) != "is-danger" {
		t.Errorf("memClass 异常")
	}
	if diskClass(50) != "is-ok" {
		t.Errorf("diskClass 异常")
	}
}

func TestSparkline(t *testing.T) {
	if got := sparkline(nil, 300, 60); got != "" {
		t.Errorf("空序列应返回空串, got %q", got)
	}
	pts := strings.Fields(sparkline([]float64{0, 50, 100}, 300, 60))
	if len(pts) != 3 {
		t.Fatalf("期望 3 个点, got %d: %v", len(pts), pts)
	}
	if pts[0] != "0.0,60.0" {
		t.Errorf("最小值应在底部 y=60, got %s", pts[0])
	}
	if pts[2] != "300.0,0.0" {
		t.Errorf("最大值应在顶部 y=0, got %s", pts[2])
	}
	// 常量序列不应 panic（maxV==minV 分支）
	if got := sparkline([]float64{5, 5, 5}, 300, 60); got == "" {
		t.Errorf("常量序列应仍生成点")
	}
}

func TestSeries(t *testing.T) {
	hist := []model.SystemSnapshot{{Timestamp: 1}, {Timestamp: 2}}
	hist[0].CPU.UsageTotal = 10
	hist[0].Memory.UsedPercent = 40
	if cs := cpuSeries(hist); len(cs) != 2 || cs[0] != 10 {
		t.Errorf("cpuSeries 异常: %v", cs)
	}
	if ms := memSeries(hist); len(ms) != 2 || ms[0] != 40 {
		t.Errorf("memSeries 异常: %v", ms)
	}
}

func TestFmt(t *testing.T) {
	if fmtPercent(12.34) != "12.3%" {
		t.Errorf("fmtPercent 异常: %s", fmtPercent(12.34))
	}
	if fmtTime(0) != "-" {
		t.Errorf("fmtTime(0) 应为 -")
	}
}
