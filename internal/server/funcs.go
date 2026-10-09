package server

import (
	"fmt"
	"strings"
)

// humanizeBytes 将字节数格式化为易读字符串。
func humanizeBytes(v uint64) string {
	const unit = 1024
	if v < unit {
		return fmt.Sprintf("%d B", v)
	}
	div, exp := uint64(unit), 0
	for n := v / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(v)/float64(div), "KMGTPE"[exp])
}

// humanizeRate 将 bytes/s 格式化。
func humanizeRate(v float64) string {
	if v <= 0 {
		return "0 B/s"
	}
	return humanizeBytes(uint64(v)) + "/s"
}

// humanizeDuration 将秒数格式化为 d/h/m/s。
func humanizeDuration(sec uint64) string {
	d := sec / 86400
	h := (sec % 86400) / 3600
	m := (sec % 3600) / 60
	s := sec % 60
	parts := []string{}
	if d > 0 {
		parts = append(parts, fmt.Sprintf("%dd", d))
	}
	if h > 0 {
		parts = append(parts, fmt.Sprintf("%dh", h))
	}
	if m > 0 {
		parts = append(parts, fmt.Sprintf("%dm", m))
	}
	if len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%ds", s))
	}
	return strings.Join(parts, " ")
}

// levelClass 依据使用率返回颜色分级 class（需求说明书 4.3）。
func levelClass(percent, warn, danger float64) string {
	switch {
	case percent >= danger:
		return "is-danger"
	case percent >= warn:
		return "is-warn"
	default:
		return "is-ok"
	}
}

// cpuClass CPU 使用率分级。
func cpuClass(p float64) string { return levelClass(p, 70, 90) }

// memClass 内存使用率分级。
func memClass(p float64) string { return levelClass(p, 75, 90) }

// diskClass 磁盘使用率分级。
func diskClass(p float64) string { return levelClass(p, 80, 90) }
