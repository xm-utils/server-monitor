# server_monitor_service

服务器运行指标监控系统，采用**管理端 + 服务端**架构：
- **服务端（agent）**：部署在每台被监控服务器上，采集本机系统与 Docker 指标，通过 gRPC 向管理端注册并上报异常
- **管理端（admin）**：集中管理所有服务器，定时健康检查，提供 Web 仪表盘展示 aggregated 指标与异常信息

需求详见 [docs/需求说明书.md](docs/需求说明书.md)，排期见 [docs/生产排期计划.md](docs/生产排期计划.md)。

## 架构

```
┌──────────────┐  gRPC Register/ReportError   ┌──────────────────┐
│  cmd/server  │ ────────────────────────────→ │   cmd/admin      │
│  (agent)     │ ← gRPC HealthCheck            │   (management)   │
│              │                                │                  │
│  gRPC Server │ ← gRPC GetMetrics (on-demand) │  HTTP Web UI     │
│  采集+暴露    │                                │  代理转发指标     │
│  异常上报     │ ── gRPC ReportError ────────→ │  缓存异常信息     │
└──────────────                                ──────────────────┘
```

## 技术栈

- Go 1.24
- HTTP 服务：`github.com/xm-utils/tools/grpcx`（底层 `gin`）
- gRPC：`google.golang.org/grpc`
- 统一响应：`github.com/xm-utils/tools/common`（`common.Response`，成功 `code=200`）
- 系统指标：`github.com/shirou/gopsutil/v4`
- Docker 指标与操作：**Docker Engine API（标准库 net/http，经 unix socket / tcp）**
- 日志：`github.com/sirupsen/logrus`

> 说明：需求书原计划使用 `github.com/docker/docker/client`，但当前版本依赖链要求 Go ≥ 1.26，与项目 Go 1.24 冲突，故改为标准库直连 Docker Engine API 实现所需接口（list/inspect/stats/start/stop/restart）。

## 目录结构

```
cmd/server-monitor/main.go   服务端入口：采集 + gRPC + HTTP API
cmd/admin-monitor/main.go    管理端入口：存储 + 轮询 + Web UI
internal/protocol/pb         gRPC Proto 定义与生成代码
internal/agent               服务端注册/心跳/异常上报
internal/admin               管理端存储/轮询/HTTP 处理
internal/config              配置加载（YAML + SMS_ 环境变量覆盖）
internal/logx                日志封装
internal/model               SystemSnapshot / DockerSnapshot
internal/store               环形缓存 Ring Buffer
internal/collector/system    系统指标采集
internal/collector/docker    Docker Engine API 客户端 + 采集器 + Operator
internal/monitor             App 运行时：采集编排 + 健康时间戳
internal/server              服务端 HTTP handler（容器操作）+ gRPC server
templates/admin/             管理端 HTML 模板
static/admin/                管理端静态资源
configs/                     配置示例（server.example.yaml / admin.example.yaml）
deploy/                      systemd 单元文件（server.service / admin.service）
```

## 快速开始

```bash
# 生成/整理依赖（使用 Go 1.24 工具链）
GOTOOLCHAIN=go1.24.2 go mod tidy

# 生成 gRPC 代码（如已生成可跳过）
make proto

# 构建
make build            # 本机构建（macOS 上为 darwin 二进制）
make build-linux      # 交叉编译 Linux 静态二进制

# 运行服务端（默认 HTTP :8080, gRPC :8081）
make run-server

# 运行管理端（默认 HTTP :9090, gRPC :9091）
make run-admin
```

### 部署到 CentOS

`make build-linux` 以 `CGO_ENABLED=0` 交叉编译，产出**静态链接**的 ELF 二进制（无任何动态依赖），可直接拷贝到 CentOS 运行。

#### 服务端部署

```bash
# 64 位 x86 机器用 amd64；ARM（如飞腾/鲲鹏）用 arm64
scp bin/server-monitor-linux-amd64 user@centos-host:/usr/local/bin/server-monitor

# 安装配置
ssh user@centos-host 'mkdir -p /etc/server_monitor_service'
scp configs/server.example.yaml user@centos-host:/etc/server_monitor_service/server.yaml

# 编辑配置，设置 admin_url 指向管理端
ssh user@centos-host 'vi /etc/server_monitor_service/server.yaml'

# 启动
ssh user@centos-host 'chmod +x /usr/local/bin/server-monitor && /usr/local/bin/server-monitor -config /etc/server_monitor_service/server.yaml'
```

#### 管理端部署

```bash
scp bin/admin-monitor-linux-amd64 user@centos-host:/usr/local/bin/admin-monitor
scp configs/admin.example.yaml user@centos-host:/etc/server_monitor_service/admin.yaml

ssh user@centos-host 'chmod +x /usr/local/bin/admin-monitor && /usr/local/bin/admin-monitor -config /etc/server_monitor_service/admin.yaml'
```

#### systemd 开机自启

仓库提供单元文件：
- [deploy/server.service](deploy/server.service) — 服务端
- [deploy/admin.service](deploy/admin.service) — 管理端

```bash
# 服务端
install -m 0755 bin/server-monitor-linux-amd64 /usr/local/bin/server-monitor
mkdir -p /etc/server_monitor_service && cp configs/server.example.yaml /etc/server_monitor_service/server.yaml
cp deploy/server.service /etc/systemd/system/server-monitor.service
systemctl daemon-reload && systemctl enable --now server-monitor

# 管理端
install -m 0755 bin/admin-monitor-linux-amd64 /usr/local/bin/admin-monitor
cp configs/admin.example.yaml /etc/server_monitor_service/admin.yaml
cp deploy/admin.service /etc/systemd/system/admin-monitor.service
systemctl daemon-reload && systemctl enable --now admin-monitor

# 查看日志
journalctl -u server-monitor -f
journalctl -u admin-monitor -f
```

### 访问管理端

- `http://localhost:9090/`        服务器列表页
- `http://localhost:9090/servers/{id}`  单服务器详情（含实时指标 + 异常面板）
- `http://localhost:9090/api/servers`   服务器列表 JSON
- `http://localhost:9090/api/servers/{id}/metrics/system`  系统指标（代理转发）
- `http://localhost:9090/api/servers/{id}/metrics/docker`  Docker 指标（代理转发）
- `http://localhost:9090/api/servers/{id}/errors`  异常列表

## 测试

```bash
GOTOOLCHAIN=go1.24.2 go test ./...
```

覆盖：配置加载/环境变量覆盖、环形缓存读写与回绕、格式化与趋势函数、Docker Engine API 客户端（httptest 模拟 daemon，含实时详情 `CollectOne`、CPU%/内存%计算、restart 宽限参数）、写操作开关与白/黑名单。

## 容器启停/重启（FR-13）

写操作**默认关闭**，需在服务端配置中开启：

```yaml
docker:
  operations:
    enabled: true
    allow_labels: ["app=cashloan"]   # 白名单，可选
    deny_names: ["mysql-prod"]       # 黑名单，可选
```

- 开启后才注册 `POST /api/docker/{id}/start|stop|restart|redeploy` 与页面按钮。
- 未开启时访问写接口返回 `code=403`（路由未注册时为 404）。
- 所有操作写入审计日志（时间、来源 IP、容器、动作、结果）。
- 生产强烈建议仅本机监听（`127.0.0.1`）并通过 Nginx + Basic Auth 反向代理。

## 重建/重新部署（redeploy）

`POST /api/docker/{id}/redeploy` 执行：**停止 → 删除容器 → 删除镜像 → 拉取新镜像 → 启动**，基于 `docker compose` 命令（`os/exec`）实现，以复用 compose 文件中声明的 env/挂载/端口/网络配置。

- **compose 文件来源**：优先读容器 inspect 标签 `com.docker.compose.project.config_files` / `working_dir` / `service` / `project`（compose 启动的容器自带）；若无则回退配置 `docker.operations.compose_file`；两者都无则报错。
- **仅删该服务镜像**：删镜像前先检查该镜像（按名/ID）是否仍被其他容器使用；若共用则跳过删除（日志中提示），避免影响别的服务；删除失败亦不中断重建。
- **执行日志展示**：接口返回每一步命令输出（`data.log`）；页面顶部/详情区新增结果面板 `#op-result`，redeploy 的日志写入其中并通过 sessionStorage 跨自动刷新保留展示（成功/失败/进行中分色）。
- 需宿主机已安装 `docker` CLI 且服务进程有权限；可用 `compose_command`（如 `docker-compose`）、`docker_bin`、`redeploy_timeout` 调整。
- 重建耗时较长，接口服务端超时按 `5×redeploy_timeout` 放宽；页面按钮需二次确认。

## 前置条件

- Linux：`/proc`、`/sys` 可读；Docker Engine 运行且 `docker.sock` 可访问。
- macOS：仅作为开发验证环境。
- **管理端**：需能访问所有服务端的 gRPC 端口（默认 8081）。
- **服务端**：需配置 `admin_url` 指向管理端 gRPC 地址（默认 9091）。
