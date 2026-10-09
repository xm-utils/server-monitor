package server

import (
	"html/template"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	xcommon "github.com/xm-utils/tools/common"

	assets "server_monitor_service"
	"server_monitor_service/internal/model"
	"server_monitor_service/internal/monitor"
)

// Handler 承载 HTTP 处理逻辑，依赖 monitor.App。
type Handler struct {
	app *monitor.App
}

func NewHandler(app *monitor.App) *Handler { return &Handler{app: app} }

// Setup 在给定 gin.Engine 上注册路由、模板与静态资源（FR-05~FR-13）。
func (h *Handler) Setup(e *gin.Engine) {
	cfg := h.app.Config()

	e.SetFuncMap(FuncMap())
	h.loadTemplates(e, cfg.Web.TemplateDir)
	h.loadStatic(e, cfg.Web.StaticDir)
	group := e.Group("/")

	// 页面
	group.GET("/", h.Index)
	group.GET("/docker", h.DockerPage)
	group.GET("/docker/:id", h.DockerDetail)

	// JSON API
	group.GET("/api/metrics/system", h.APISystem)
	group.GET("/api/metrics/system/history", h.APISystemHistory)
	group.GET("/api/metrics/docker", h.APIDocker)
	group.GET("/api/metrics/docker/:id", h.APIDockerOne)
	group.GET("/api/health", h.APIHealth)
	group.GET("/api/version", h.APIVersion)

	// FR-13 写操作：仅在开关开启时注册
	if h.app.Operator().Enabled() {
		group.POST("/api/docker/:id/start", h.OpStart)
		group.POST("/api/docker/:id/stop", h.OpStop)
		group.POST("/api/docker/:id/restart", h.OpRestart)
		group.POST("/api/docker/:id/redeploy", h.OpRedeploy)
	}
}

// loadTemplates 优先从磁盘目录加载（便于开发热调试）；目录不存在时回退到内嵌模板。
func (h *Handler) loadTemplates(e *gin.Engine, dir string) {
	if dir != "" {
		if m, _ := filepath.Glob(filepath.Join(dir, "*.html")); len(m) > 0 {
			e.LoadHTMLGlob(filepath.Join(dir, "*.html"))
			return
		}
	}
	e.SetHTMLTemplate(template.Must(
		template.New("").Funcs(FuncMap()).ParseFS(assets.TemplatesFS, "templates/*.html"),
	))
}

// loadStatic 优先从磁盘目录提供静态资源；不存在时回退到内嵌资源。
func (h *Handler) loadStatic(e *gin.Engine, dir string) {
	if dir != "" {
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
			e.Static("/static", dir)
			return
		}
	}
	if sub, err := fs.Sub(assets.StaticFS, "static"); err == nil {
		e.StaticFS("/static", http.FS(sub))
	}
}

// ---- HTML ----

func (h *Handler) Index(c *gin.Context) {
	snap, _ := h.app.Store().LatestSystem()
	hist := h.app.Store().SystemHistory(100)
	data := gin.H{
		"Title":     "服务器监控概览",
		"RefreshMs": h.refreshMs(c),
		"Sys":       snap,
		"CpuTrend":  sparkline(cpuSeries(hist), 300, 60),
		"MemTrend":  sparkline(memSeries(hist), 300, 60),
		"HistCount": len(hist),
	}
	data["Docker"] = h.dockerSummaryCounts()
	c.HTML(http.StatusOK, "index.html", data)
}

func (h *Handler) DockerPage(c *gin.Context) {
	res, _ := h.app.Store().LatestDocker()
	data := gin.H{
		"Title":      "Docker 服务监控",
		"RefreshMs":  h.refreshMs(c),
		"Docker":     res,
		"OpsEnabled": h.app.Operator().Enabled(),
		"HumanBytes": humanizeBytes,
		"HumanRate":  humanizeRate,
	}
	c.HTML(http.StatusOK, "docker.html", data)
}

func (h *Handler) DockerDetail(c *gin.Context) {
	id := c.Param("id")
	res, _ := h.app.Store().LatestDocker()
	var found *model.DockerSnapshot
	for i := range res.Containers {
		if res.Containers[i].ContainerID == id || res.Containers[i].Name == id {
			found = &res.Containers[i]
			break
		}
	}
	data := gin.H{
		"Title":      "容器详情",
		"RefreshMs":  h.refreshMs(c),
		"Container":  found,
		"NotFound":   found == nil,
		"OpsEnabled": h.app.Operator().Enabled(),
		"HumanBytes": humanizeBytes,
		"HumanRate":  humanizeRate,
	}
	c.HTML(http.StatusOK, "docker_detail.html", data)
}

// ---- JSON API (common.Response) ----

func (h *Handler) APISystem(c *gin.Context) {
	if snap, ok := h.app.Store().LatestSystem(); ok {
		xcommon.GinSuccess(c, snap)
	} else {
		xcommon.GinError(c, 503, "系统指标尚未采集")
	}
}

func (h *Handler) APISystemHistory(c *gin.Context) {
	limit := intParam(c, "limit", 100)
	xcommon.GinSuccess(c, h.app.Store().SystemHistory(limit))
}

func (h *Handler) APIDocker(c *gin.Context) {
	res, _ := h.app.Store().LatestDocker()
	xcommon.GinSuccess(c, res)
}

func (h *Handler) APIDockerOne(c *gin.Context) {
	id := c.Param("id")
	// 优先实时采集（Docker 实时详情接口）
	if snap, err := h.app.DockerOne(c.Request.Context(), id); err == nil && snap != nil {
		xcommon.GinSuccess(c, snap)
		return
	}
	// 回退到最近一次缓存快照
	res, _ := h.app.Store().LatestDocker()
	for i := range res.Containers {
		if res.Containers[i].ContainerID == id || res.Containers[i].Name == id {
			xcommon.GinSuccess(c, res.Containers[i])
			return
		}
	}
	xcommon.GinError(c, 404, "容器不存在或 Docker 不可用")
}

func (h *Handler) APIHealth(c *gin.Context) {
	data := gin.H{
		"system": healthy(h.app.SystemHealthy()),
		"docker": healthy(h.app.DockerHealthy()),
		"time":   time.Now().Unix(),
	}
	xcommon.GinSuccess(c, data)
}

var buildVersion = "dev"

func (h *Handler) APIVersion(c *gin.Context) {
	xcommon.GinSuccess(c, gin.H{"version": buildVersion})
}

// ---- FR-13 写操作 ----

func (h *Handler) OpStart(c *gin.Context)    { h.doOp(c, "start") }
func (h *Handler) OpStop(c *gin.Context)     { h.doOp(c, "stop") }
func (h *Handler) OpRestart(c *gin.Context)  { h.doOp(c, "restart") }
func (h *Handler) OpRedeploy(c *gin.Context) { h.doOp(c, "redeploy") }

func (h *Handler) doOp(c *gin.Context, action string) {
	op := h.app.Operator()
	if !op.Enabled() {
		xcommon.GinError(c, 403, "容器写操作未启用")
		return
	}
	id := c.Param("id")

	// 白名单/黑名单校验
	if labels, err := op.Labels(c.Request.Context(), id); err == nil {
		if !op.Allowed(id, id, labels) {
			xcommon.GinError(c, 403, "该容器不在允许操作范围内")
			return
		}
	}

	// 重建（redeploy）可能耗时较久（删镜像 + 拉新镜像），使用更宽的超时。
	timeout := 30 * time.Second
	if action == "redeploy" {
		timeout = 5*op.RedeployTimeout() + 30*time.Second
	}
	ctx, cancel := contextWithTimeout(c, timeout)
	defer cancel()

	var (
		err error
		log string
	)
	switch action {
	case "start":
		err = op.Start(ctx, id)
	case "stop":
		err = op.Stop(ctx, id)
	case "restart":
		err = op.Restart(ctx, id)
	case "redeploy":
		log, err = op.Redeploy(ctx, id)
	}

	audit(c, id, action, err)
	if err != nil {
		msg := "操作失败: " + err.Error()
		if log != "" {
			msg += "\n" + log
		}
		xcommon.GinError(c, 500, msg)
		return
	}
	data := gin.H{"containerId": id, "action": action, "result": "ok"}
	if log != "" {
		data["log"] = log
	}
	xcommon.GinSuccess(c, data)
}

// ---- helpers ----

func (h *Handler) refreshMs(c *gin.Context) int {
	// ?refresh=0 关闭自动刷新（FR-08）
	if r := c.Query("refresh"); r != "" {
		if v, err := strconv.Atoi(r); err == nil {
			return v
		}
	}
	return h.app.Config().Web.RefreshIntervalMs
}

func (h *Handler) dockerSummaryCounts() gin.H {
	res, _ := h.app.Store().LatestDocker()
	var running, stopped int
	for _, ct := range res.Containers {
		if ct.State == "running" {
			running++
		} else {
			stopped++
		}
	}
	return gin.H{
		"Available": res.Available,
		"Total":     len(res.Containers),
		"Running":   running,
		"Stopped":   stopped,
	}
}

func healthy(ok bool) string {
	if ok {
		return "healthy"
	}
	return "unhealthy"
}

func intParam(c *gin.Context, key string, def int) int {
	if v := c.Query(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}
