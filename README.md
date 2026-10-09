# Server Monitor

轻量级服务器运行指标监控系统，采用 **管理端（Admin）+ 服务端（Agent）** 架构，支持单二进制部署，无需外部依赖。

[![Go Version](https://img.shields.io/badge/Go-1.24-blue)](https://go.dev)
[![License](https://img.shields.io/badge/License-MIT-green)](#license)

## 功能特性

- **服务器指标采集**：CPU、内存、磁盘、网络、负载、进程等实时指标，基于 gopsutil
- **Docker 容器监控**：容器列表、CPU/内存/网络/磁盘 IO 速率、端口映射、挂载、网卡、环境变量
- **容器操作**（默认关闭）：启动/停止/重启/重建（基于 docker compose）
- **管理端仪表盘**：多服务器集中管理、Tab 式详情页（服务器信息 / Docker 服务 / 异常记录）
- **gRPC 通信**：管理端按需拉取指标，服务端主动注册 + 心跳保活
- **单二进制部署**：模板与静态资源通过 `go:embed` 打包，拷贝即用
- **趋势图**：CPU / 内存历史迷你 SVG 曲线
- **异常上报**：服务端自动上报采集器异常，管理端缓存并展示

## 架构

```
┌─────────────────────┐                          ┌─────────────────────┐
│   服务端 (Agent)     │  ──── gRPC 注册/心跳 ───→  │   管理端 (Admin)     │
│                     │  ←─── gRPC 指标拉取 ────  │                     │
│  • 系统指标采集      │                          │  • 服务器列表        │
│  • Docker 指标采集   │  ──── gRPC 异常上报 ───→  │  • 服务器详情 Tab   │
│  • HTTP API + Web   │                          │  • Docker 容器详情   │
│  • gRPC Server      │                          │  • 异常记录         │
│  • 容器操作 (可选)   │                          │  • 代理转发指标      │
└─────────────────────┘                          └─────────────────────┘
        :8080 / :8081                                      :9090 / :9091
     (HTTP / gRPC)                                  (HTTP / gRPC)
```

## 技术栈

| 层次 | 选型 | 说明 |
|------|------|------|
| 语言 | Go 1.24 | 单二进制，交叉编译零依赖 |
| HTTP | gin (via grpcx) | 路由与 HTML 渲染 |
| RPC | gRPC + Protobuf | 管理端 ↔ 服务端通信 |
| 统一响应 | xm-utils/tools/common | `code=200` JSON 格式 |
| 系统指标 | gopsutil/v4 | 跨平台 CPU/内存/磁盘/网络/进程 |
| Docker 指标 | Docker Engine API | 标准库 net/http，经 unix socket 直连 |
| 模板 | html/template + go:embed | 服务端渲染，模板内嵌二进制 |
| 日志 | logrus | 结构化，支持级别配置 |

> Docker 采集使用标准库 net/http 直连 Engine API（而非 docker/client），因当前 client 版本要求 Go ≥ 1.26。

## 目录结构

```
server_monitor/
├── cmd/
│   ├── server-monitor/main.go     服务端入口
│   └── admin-monitor/main.go      管理端入口
├── internal/
│   ├── agent/                     服务端注册、心跳、异常上报
│   ├── admin/                     管理端存储、轮询、HTTP handler
│   ├── collector/
│   │   ├── docker/                Docker Engine API 客户端 + 采集器
│   │   └── system/                系统指标采集器
│   ├── config/                    配置加载（YAML + 环境变量覆盖）
│   ├── logx/                      日志初始化
│   ├── model/                     数据模型（SystemSnapshot / DockerSnapshot）
│   ├── monitor/                   运行时编排（采集周期、健康时间戳）
│   ├── protocol/pb/               gRPC Proto 定义与生成代码
│   ├── server/                    服务端 HTTP handler + gRPC server
│   └── store/                     环形缓存（Ring Buffer）
├── templates/
│   ├── layout.html                服务端公共布局
│   ├── index.html                 服务端系统概览页
│   ├── docker.html                服务端 Docker 列表页
│   ├── docker_detail.html         服务端容器详情页
│   └── admin/                     管理端模板
│       ├── layout.html            管理端公共布局
│       ├── index.html             服务器列表页
│       ├── detail.html            服务器详情页（Tab 式）
│       └── docker_detail.html     容器详情页
├── static/
│   ├── css/                       服务端样式
│   ├── js/                        服务端脚本
│   └── admin/                     管理端静态资源
├── configs/                       配置示例
├── deploy/                        systemd 单元文件
├── docs/                          需求说明书、排期计划
├── Makefile                       构建脚本
└── assets.go                      go:embed 资源声明
```

## 快速开始

### 构建

```bash
# 需要 Go 1.24 工具链
export GOTOOLCHAIN=go1.24.2

# 本机构建
make build

# 交叉编译 Linux 静态二进制（部署用）
make build-linux
# 产出：
#   bin/server-monitor-linux-amd64
#   bin/admin-monitor-linux-amd64
```

### 本地运行

```bash
# 启动管理端（HTTP :9090, gRPC :9091）
make run-admin

# 启动服务端（HTTP :8080, gRPC :8081）
make run-server
```

服务端启动后会自动向管理端注册（需配置 `admin_url`）。

### 访问页面

| 端 | 地址 | 说明 |
|----|------|------|
| 管理端 | `http://localhost:9090/` | 服务器列表 |
| 管理端 | `http://localhost:9090/servers/{id}` | 服务器详情（Tab：系统 / Docker / 异常） |
| 管理端 | `http://localhost:9090/servers/{id}/docker/{cid}` | 容器详情 |
| 服务端 | `http://localhost:8080/` | 本机系统概览 |
| 服务端 | `http://localhost:8080/docker` | 本机 Docker 列表 |
| 服务端 | `http://localhost:8080/docker/{id}` | 本机容器详情 |

## API 接口

所有 JSON 接口统一响应格式：

```json
{ "code": 200, "message": "操作成功", "data": { ... } }
```

### 管理端 API

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/servers` | 服务器列表 |
| GET | `/api/servers/{id}` | 单服务器信息 |
| GET | `/api/servers/{id}/metrics/system` | 系统指标（代理转发） |
| GET | `/api/servers/{id}/metrics/docker` | Docker 容器列表（代理转发） |
| GET | `/api/servers/{id}/docker/{cid}` | 单容器详情（代理转发） |
| GET | `/api/servers/{id}/errors` | 异常记录 |
| DELETE | `/api/servers/{id}` | 移除服务器 |

### 服务端 API

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/metrics/system` | 最新系统快照 |
| GET | `/api/metrics/system/history` | 系统历史（`?limit=100`） |
| GET | `/api/metrics/docker` | Docker 容器列表 |
| GET | `/api/metrics/docker/{id}` | 单容器指标（优先实时采集） |
| GET | `/api/health` | 健康检查 |
| GET | `/api/version` | 构建版本 |
| POST | `/api/docker/{id}/start` | 启动容器 |
| POST | `/api/docker/{id}/stop` | 停止容器 |
| POST | `/api/docker/{id}/restart` | 重启容器 |
| POST | `/api/docker/{id}/redeploy` | 重建容器（compose 全流程） |

> 写操作接口（start/stop/restart/redeploy）需开启 `docker.operations.enabled`，默认关闭。

## 配置说明

### 服务端 (`configs/server.example.yaml`)

```yaml
server:
  listen_addr: 8080                     # HTTP 端口
  grpc_addr: 8081                       # gRPC 端口
  admin_url: "192.168.1.100:9091"       # 管理端 gRPC 地址（必填）
  heartbeat_interval: 30s               # 心跳间隔

collector:
  system_interval: 3s                   # 系统采集周期
  docker_interval: 5s                   # Docker 采集周期
  history_size: 300                     # 内存保留采样条数

docker:
  enabled: true                         # 是否启用 Docker 采集
  endpoint: "unix:///var/run/docker.sock"
  operations:
    enabled: false                      # 容器写操作开关（默认关闭）
    allow_labels: []                    # 白名单标签
    deny_names: []                      # 黑名单容器名

log:
  level: info
```

### 管理端 (`configs/admin.example.yaml`)

```yaml
admin:
  listen_addr: 9090                     # HTTP 端口
  grpc_addr: 9091                       # gRPC 端口（接收注册/心跳）
  poll_interval: 10s                    # 健康检查间隔
  heartbeat_timeout: 90s                # 心跳超时（标记 offline）
  poll_timeout: 5s                      # gRPC 调用超时

log:
  level: info
```

### 环境变量覆盖

所有配置项支持 `SMS_` 前缀环境变量覆盖，例如：

```bash
SMS_LISTEN_ADDR=9090
SMS_ADMIN_URL=10.0.0.1:9091
SMS_LOG_LEVEL=debug
```

## 部署

### CentOS / Linux systemd

```bash
# 1. 上传二进制
scp bin/server-monitor-linux-amd64 user@host:/usr/local/bin/server-monitor
scp bin/admin-monitor-linux-amd64 user@host:/usr/local/bin/admin-monitor

# 2. 安装配置
mkdir -p /etc/server_monitor
cp configs/server.example.yaml /etc/server_monitor/server.yaml
cp configs/admin.example.yaml /etc/server_monitor/admin.yaml

# 3. 安装 systemd 单元
cp deploy/server.service /etc/systemd/system/server-monitor.service
cp deploy/admin.service /etc/systemd/system/admin-monitor.service

# 4. 启动
systemctl daemon-reload
systemctl enable --now server-monitor
systemctl enable --now admin-monitor

# 5. 查看日志
journalctl -u server-monitor -f
journalctl -u admin-monitor -f
```

### 开发热调试

模板和静态资源支持磁盘目录优先加载——在项目目录下运行时会读取 `./templates/` 和 `./static/`，修改后刷新页面即可生效，无需重新编译。

## gRPC Proto

接口定义在 `internal/protocol/pb/monitor.proto`，修改后重新生成：

```bash
make proto
```

生成代码位于同目录下的 `monitor.pb.go` 和 `monitor_grpc.pb.go`。

主要 RPC：

| 服务 | 方法 | 方向 | 说明 |
|------|------|------|------|
| AdminService | Register | 服务端→管理端 | 服务器注册 |
| AdminService | ReportError | 服务端→管理端 | 异常上报 |
| ServerService | HealthCheck | 管理端→服务端 | 健康检查 |
| ServerService | GetSystemMetrics | 管理端→服务端 | 系统指标（含磁盘/进程详情） |
| ServerService | GetSystemHistory | 管理端→服务端 | 趋势数据 |
| ServerService | GetDockerMetrics | 管理端→服务端 | Docker 容器列表 |
| ServerService | GetDockerList | 管理端→服务端 | Docker 列表（含可用性状态） |
| ServerService | GetDockerContainerDetail | 管理端→服务端 | 单容器实时详情 |

## 容器操作（FR-13）

写操作 **默认关闭**，需显式开启：

```yaml
docker:
  operations:
    enabled: true
    allow_labels: ["app=cashloan"]
    deny_names: ["mysql-prod"]
```

- 开启后服务端 HTTP 页面显示操作按钮（管理端始终只读）。
- 所有操作写入审计日志（时间、IP、容器、动作、结果）。
- 生产环境建议仅本机监听 + Nginx 反向代理鉴权。

### 重建（redeploy）

`POST /api/docker/{id}/redeploy` 执行：停止 → 删除容器 → 删除镜像 → 拉取新镜像 → 启动。

基于 `docker compose` 实现，自动从容器标签推断 compose 文件和 service 名称。

## 测试

```bash
GOTOOLCHAIN=go1.24.2 go test ./...
```

覆盖模块：配置加载/环境变量覆盖、环形缓存读写与回绕、格式化与趋势函数、Docker Engine API 客户端（httptest 模拟 daemon）、写操作开关与白/黑名单。

## 前置条件

- **Linux**：`/proc`、`/sys` 可读；Docker Engine 运行且 `docker.sock` 可访问
- **macOS**：仅作为开发验证环境
- **管理端**：需能访问所有服务端的 gRPC 端口（默认 8081）
- **服务端**：需配置 `admin_url` 指向管理端 gRPC 地址（默认 9091）

## 相关文档

- [需求说明书](docs/需求说明书.md) — 功能需求、指标定义、技术方案
- [生产排期计划](docs/生产排期计划.md) — 里程碑与交付计划
