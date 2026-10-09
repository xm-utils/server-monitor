package server

import (
	"context"
	"fmt"
	"html/template"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"server_monitor_service/internal/model"
)

// FuncMap 注册模板可用的格式化函数。
func FuncMap() template.FuncMap {
	return template.FuncMap{
		"humanizeBytes":    humanizeBytes,
		"humanizeRate":     humanizeRate,
		"humanizeDuration": humanizeDuration,
		"cpuClass":         cpuClass,
		"memClass":         memClass,
		"diskClass":        diskClass,
		"fmtTime":          fmtTime,
		"fmtPercent":       fmtPercent,
		"add":              func(a, b int) int { return a + b },
	}
}

func fmtTime(unix int64) string {
	if unix <= 0 {
		return "-"
	}
	return time.Unix(unix, 0).Format("2006-01-02 15:04:05")
}

func fmtPercent(v float64) string {
	return fmt.Sprintf("%.1f%%", v)
}

// sparkline 将数值序列转换为内联 SVG polyline 的 points 字符串（FR-04 趋势）。
func sparkline(values []float64, w, h int) string {
	if len(values) == 0 {
		return ""
	}
	maxV := values[0]
	minV := values[0]
	for _, v := range values {
		if v > maxV {
			maxV = v
		}
		if v < minV {
			minV = v
		}
	}
	if maxV == minV {
		maxV = minV + 1
	}
	n := len(values)
	if n == 1 {
		n = 2
	}
	pts := make([]string, 0, len(values))
	step := float64(w) / float64(n-1)
	for i, v := range values {
		x := float64(i) * step
		y := float64(h) - (v-minV)/(maxV-minV)*float64(h)
		pts = append(pts, fmt.Sprintf("%.1f,%.1f", x, y))
	}
	return strings.Join(pts, " ")
}

func cpuSeries(hist []model.SystemSnapshot) []float64 {
	out := make([]float64, 0, len(hist))
	for _, s := range hist {
		out = append(out, s.CPU.UsageTotal)
	}
	return out
}

func memSeries(hist []model.SystemSnapshot) []float64 {
	out := make([]float64, 0, len(hist))
	for _, s := range hist {
		out = append(out, s.Memory.UsedPercent)
	}
	return out
}

// contextWithTimeout 基于请求上下文派生超时上下文。
func contextWithTimeout(c *gin.Context, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(c.Request.Context(), d)
}

// audit 记录容器写操作审计日志（FR-13）。
func audit(c *gin.Context, id, action string, err error) {
	result := "success"
	if err != nil {
		result = "failure"
	}
	logrus.WithFields(logrus.Fields{
		"event":     "docker_op",
		"ip":        c.ClientIP(),
		"container": id,
		"action":    action,
		"result":    result,
		"err":       fmt.Sprint(err),
	}).Info("容器启停/重启操作审计")
}
