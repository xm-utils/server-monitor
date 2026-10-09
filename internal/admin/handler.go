package admin

import (
	"context"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	xcommon "github.com/xm-utils/tools/common"

	assets "server_monitor_service"
	pb "server_monitor_service/internal/protocol/pb"
)

// Handler 管理端 HTTP 处理逻辑。
type Handler struct {
	store *Store
}

// NewHandler 创建 HTTP 处理器。
func NewHandler(store *Store) *Handler {
	return &Handler{store: store}
}

// Setup 注册路由。
func (h *Handler) Setup(e *gin.Engine) {
	// 加载模板
	h.loadTemplates(e)
	h.loadStatic(e)

	group := e.Group("/")

	// 页面
	group.GET("/", h.Index)
	group.GET("/servers/:id", h.ServerDetail)
	group.GET("/servers/:id/docker/:containerId", h.DockerContainerDetail)

	// JSON API
	group.GET("/api/servers", h.APIServers)
	group.GET("/api/servers/:id", h.APIServer)
	group.GET("/api/servers/:id/metrics/system", h.APISystemMetrics)
	group.GET("/api/servers/:id/metrics/docker", h.APIDockerMetrics)
	group.GET("/api/servers/:id/docker/:containerId", h.APIDockerContainerDetail)
	group.GET("/api/servers/:id/errors", h.APIErrors)
	group.DELETE("/api/servers/:id", h.DeleteServer)
}

// Index 服务器列表页。
func (h *Handler) Index(c *gin.Context) {
	servers := h.store.ListServers()
	c.HTML(http.StatusOK, "admin_index.html", gin.H{
		"Title":   "服务器监控管理",
		"Servers": servers,
	})
}

// ServerDetail 单服务器详情页（Tab 布局：服务器信息 + Docker 服务）。
func (h *Handler) ServerDetail(c *gin.Context) {
	id := c.Param("id")
	info := h.store.GetServer(id)
	errors := h.store.GetErrors(id)

	if info == nil {
		c.HTML(http.StatusOK, "admin_detail.html", gin.H{
			"Title":    "服务器详情",
			"NotFound": true,
		})
		return
	}

	// 通过 gRPC 获取丰富系统指标
	sysMetrics, err := h.fetchSystemMetrics(c.Request.Context(), info)
	if err != nil {
		logrus.Warnf("获取系统指标失败 [%s]: %v", id, err)
	}

	// 获取系统历史（趋势图）
	history, err := h.fetchSystemHistory(c.Request.Context(), info, 100)
	if err != nil {
		logrus.Warnf("获取系统历史失败 [%s]: %v", id, err)
	}

	// 获取 Docker 列表
	dockerResult, err := h.fetchDockerList(c.Request.Context(), info)
	if err != nil {
		logrus.Warnf("获取 Docker 列表失败 [%s]: %v", id, err)
	}

	// 计算趋势 polyline
	cpuTrend := sparkline(history, "cpu", 300, 60)
	memTrend := sparkline(history, "mem", 300, 60)

	c.HTML(http.StatusOK, "admin_detail.html", gin.H{
		"Title":        "服务器详情",
		"Server":       info,
		"ServerID":     id,
		"Errors":       errors,
		"NotFound":     false,
		"Sys":          sysMetrics,
		"HistCount":    len(history),
		"CpuTrend":     cpuTrend,
		"MemTrend":     memTrend,
		"DockerResult": dockerResult,
		"DockerAvail":  dockerResult != nil && dockerResult.Available,
	})
}

// APIServers 服务器列表 JSON。
func (h *Handler) APIServers(c *gin.Context) {
	xcommon.GinSuccess(c, h.store.ListServers())
}

// APIServer 单服务器 JSON。
func (h *Handler) APIServer(c *gin.Context) {
	id := c.Param("id")
	info := h.store.GetServer(id)
	if info == nil {
		xcommon.GinError(c, 404, "服务器不存在")
		return
	}
	xcommon.GinSuccess(c, info)
}

// APISystemMetrics 代理转发系统指标。
func (h *Handler) APISystemMetrics(c *gin.Context) {
	id := c.Param("id")
	info := h.store.GetServer(id)
	if info == nil {
		xcommon.GinError(c, 404, "服务器不存在")
		return
	}

	metrics, err := h.fetchSystemMetrics(c.Request.Context(), info)
	if err != nil {
		logrus.Warnf("获取系统指标失败 [%s]: %v", id, err)
		xcommon.GinError(c, 502, "获取指标失败: "+err.Error())
		return
	}

	xcommon.GinSuccess(c, metrics)
}

// APIDockerMetrics 代理转发 Docker 指标。
func (h *Handler) APIDockerMetrics(c *gin.Context) {
	id := c.Param("id")
	info := h.store.GetServer(id)
	if info == nil {
		xcommon.GinError(c, 404, "服务器不存在")
		return
	}

	containers, err := h.fetchDockerMetrics(c.Request.Context(), info)
	if err != nil {
		logrus.Warnf("获取 Docker 指标失败 [%s]: %v", id, err)
		xcommon.GinError(c, 502, "获取指标失败: "+err.Error())
		return
	}

	xcommon.GinSuccess(c, containers)
}

// APIErrors 异常列表。
func (h *Handler) APIErrors(c *gin.Context) {
	id := c.Param("id")
	errors := h.store.GetErrors(id)
	xcommon.GinSuccess(c, errors)
}

// DeleteServer 移除服务器。
func (h *Handler) DeleteServer(c *gin.Context) {
	id := c.Param("id")
	h.store.RemoveServer(id)
	xcommon.GinSuccess(c, gin.H{"id": id, "result": "ok"})
}

// DockerContainerDetail 容器详情页。
func (h *Handler) DockerContainerDetail(c *gin.Context) {
	serverID := c.Param("id")
	containerID := c.Param("containerId")
	info := h.store.GetServer(serverID)

	if info == nil {
		c.HTML(http.StatusOK, "admin_docker_detail.html", gin.H{
			"Title":    "容器详情",
			"NotFound": true,
		})
		return
	}

	container, err := h.fetchDockerContainerDetail(c.Request.Context(), info, containerID)
	if err != nil {
		logrus.Warnf("获取容器详情失败 [%s/%s]: %v", serverID, containerID, err)
		c.HTML(http.StatusOK, "admin_docker_detail.html", gin.H{
			"Title":    "容器详情",
			"NotFound": true,
			"Error":    err.Error(),
		})
		return
	}

	c.HTML(http.StatusOK, "admin_docker_detail.html", gin.H{
		"Title":     "容器详情",
		"Server":    info,
		"Container": container,
		"NotFound":  false,
	})
}

// APIDockerContainerDetail 代理转发单容器详情。
func (h *Handler) APIDockerContainerDetail(c *gin.Context) {
	serverID := c.Param("id")
	containerID := c.Param("containerId")
	info := h.store.GetServer(serverID)
	if info == nil {
		xcommon.GinError(c, 404, "服务器不存在")
		return
	}

	container, err := h.fetchDockerContainerDetail(c.Request.Context(), info, containerID)
	if err != nil {
		xcommon.GinError(c, 502, "获取容器详情失败: "+err.Error())
		return
	}

	xcommon.GinSuccess(c, container)
}

// ---- gRPC 代理方法 ----

// fetchSystemMetrics 从服务端获取系统指标。
func (h *Handler) fetchSystemMetrics(ctx context.Context, info *ServerInfo) (*pb.SystemMetrics, error) {
	addr := fmt.Sprintf("%s:%d", info.IP, info.GrpcPort)

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	client := pb.NewServerServiceClient(conn)
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	resp, err := client.GetSystemMetrics(cctx, &pb.GetSystemMetricsRequest{})
	if err != nil {
		return nil, err
	}

	return resp.Metrics, nil
}

// fetchSystemHistory 从服务端获取系统历史。
func (h *Handler) fetchSystemHistory(ctx context.Context, info *ServerInfo, limit int) ([]*pb.SystemHistoryEntry, error) {
	addr := fmt.Sprintf("%s:%d", info.IP, info.GrpcPort)

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	client := pb.NewServerServiceClient(conn)
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	resp, err := client.GetSystemHistory(cctx, &pb.GetSystemHistoryRequest{Limit: int32(limit)})
	if err != nil {
		return nil, err
	}

	return resp.Entries, nil
}

// fetchDockerMetrics 从服务端获取 Docker 指标。
func (h *Handler) fetchDockerMetrics(ctx context.Context, info *ServerInfo) ([]*pb.DockerContainer, error) {
	addr := fmt.Sprintf("%s:%d", info.IP, info.GrpcPort)

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	client := pb.NewServerServiceClient(conn)
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	resp, err := client.GetDockerMetrics(cctx, &pb.GetDockerMetricsRequest{})
	if err != nil {
		return nil, err
	}

	return resp.Containers, nil
}

// fetchDockerList 从服务端获取 Docker 列表（含 Available 状态）。
func (h *Handler) fetchDockerList(ctx context.Context, info *ServerInfo) (*pb.DockerListResult, error) {
	addr := fmt.Sprintf("%s:%d", info.IP, info.GrpcPort)

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	client := pb.NewServerServiceClient(conn)
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	resp, err := client.GetDockerList(cctx, &pb.GetDockerListRequest{})
	if err != nil {
		return nil, err
	}

	return resp.Result, nil
}

// fetchDockerContainerDetail 从服务端获取单容器详情。
func (h *Handler) fetchDockerContainerDetail(ctx context.Context, info *ServerInfo, containerID string) (*pb.DockerContainer, error) {
	addr := fmt.Sprintf("%s:%d", info.IP, info.GrpcPort)

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	client := pb.NewServerServiceClient(conn)
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	resp, err := client.GetDockerContainerDetail(cctx, &pb.GetDockerContainerDetailRequest{
		ContainerId: containerID,
	})
	if err != nil {
		return nil, err
	}

	if resp.NotFound {
		return nil, fmt.Errorf("容器不存在")
	}

	return resp.Container, nil
}

// ---- 模板与静态资源 ----

// loadTemplates 从嵌入文件系统加载模板。
func (h *Handler) loadTemplates(e *gin.Engine) {
	// 优先从磁盘加载（开发模式）
	dir := "./templates/admin"
	if m, _ := filepath.Glob(filepath.Join(dir, "*.html")); len(m) > 0 {
		e.SetFuncMap(adminFuncMap())
		e.LoadHTMLGlob(filepath.Join(dir, "*.html"))
		return
	}

	// 从嵌入文件系统加载
	tmpl := template.Must(template.New("").Funcs(adminFuncMap()).ParseFS(assets.TemplatesFS, "templates/admin/*.html"))
	e.SetHTMLTemplate(tmpl)
}

// adminFuncMap 管理端模板函数。
func adminFuncMap() template.FuncMap {
	return template.FuncMap{
		"fmtTime": func(unix int64) string {
			if unix <= 0 {
				return "-"
			}
			return time.Unix(unix, 0).Format("2006-01-02 15:04:05")
		},
		"formatTime": func(unix int64) string {
			if unix <= 0 {
				return "-"
			}
			return time.Unix(unix, 0).Format("2006-01-02 15:04:05")
		},
		"fmtPercent": func(v float64) string {
			return fmt.Sprintf("%.1f%%", v)
		},
		"humanizeBytes": func(v uint64) string {
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
		},
		"humanizeRate": func(v float64) string {
			if v <= 0 {
				return "0 B/s"
			}
			const unit = 1024
			uv := uint64(v)
			if uv < unit {
				return fmt.Sprintf("%d B/s", uv)
			}
			div, exp := uint64(unit), 0
			for n := uv / unit; n >= unit; n /= unit {
				div *= unit
				exp++
			}
			return fmt.Sprintf("%.1f %ciB/s", float64(uv)/float64(div), "KMGTPE"[exp])
		},
		"humanizeDuration": func(sec uint64) string {
			d := time.Duration(sec) * time.Second
			days := int(d.Hours()) / 24
			hours := int(d.Hours()) % 24
			mins := int(d.Minutes()) % 60
			if days > 0 {
				return fmt.Sprintf("%d天 %d小时", days, hours)
			}
			if hours > 0 {
				return fmt.Sprintf("%d小时 %d分钟", hours, mins)
			}
			return fmt.Sprintf("%d分钟", mins)
		},
		"shortID": func(id string) string {
			if len(id) > 12 {
				return id[:12]
			}
			return id
		},
		"cpuClass": func(v float64) string {
			if v >= 90 {
				return "val-critical"
			}
			if v >= 70 {
				return "val-warning"
			}
			return ""
		},
		"memClass": func(v float64) string {
			if v >= 90 {
				return "val-critical"
			}
			if v >= 80 {
				return "val-warning"
			}
			return ""
		},
		"diskClass": func(v float64) string {
			if v >= 90 {
				return "val-critical"
			}
			if v >= 80 {
				return "val-warning"
			}
			return ""
		},
	}
}

// loadStatic 从嵌入文件系统提供静态资源。
func (h *Handler) loadStatic(e *gin.Engine) {
	dir := "./static"
	if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
		e.Static("/static", dir)
		return
	}

	// 嵌入资源：/static/admin/style.css -> static/admin/style.css
	if sub, err := fs.Sub(assets.StaticFS, "static"); err == nil {
		e.StaticFS("/static", http.FS(sub))
	}
}

// ---- 辅助函数 ----

// sparkline 将历史数据转换为 SVG polyline points 字符串。
func sparkline(entries []*pb.SystemHistoryEntry, field string, w, h int) string {
	if len(entries) == 0 {
		return ""
	}

	values := make([]float64, 0, len(entries))
	for _, e := range entries {
		switch field {
		case "cpu":
			values = append(values, e.CpuPercent)
		case "mem":
			values = append(values, e.MemPercent)
		}
	}

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

