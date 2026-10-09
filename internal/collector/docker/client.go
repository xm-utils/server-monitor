package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client 是一个极简 Docker Engine API 客户端，通过 unix socket 或 tcp 访问。
// 之所以不使用 github.com/docker/docker/client：其依赖链会引入需要 Go >= 1.26 的模块，
// 与当前 Go 1.24 不兼容；这里以标准库实现所需的最小接口集合。
type Client struct {
	hc         *http.Client
	base       string // 形如 http://localhost 或 http://127.0.0.1:2375
	apiVersion string // 形如 v1.43
	enabled    bool
}

// NewClient 依据 endpoint 创建客户端。endpoint 支持 unix:///path 或 tcp://host:port。
func NewClient(endpoint, apiVersion string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	c := &Client{apiVersion: apiVersion}

	switch {
	case strings.HasPrefix(endpoint, "unix://"):
		socket := strings.TrimPrefix(endpoint, "unix://")
		c.hc = &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "unix", socket)
				},
			},
		}
		c.base = "http://docker"
	case strings.HasPrefix(endpoint, "tcp://"):
		u, err := url.Parse(endpoint)
		if err == nil {
			c.base = "http://" + u.Host
			c.hc = &http.Client{Timeout: timeout}
		}
	case strings.HasPrefix(endpoint, "http://") || strings.HasPrefix(endpoint, "https://"):
		c.base = strings.TrimRight(endpoint, "/")
		c.hc = &http.Client{Timeout: timeout}
	default:
		// 视为本地 unix socket 路径
		socket := endpoint
		c.hc = &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "unix", socket)
				},
			},
		}
		c.base = "http://docker"
	}

	if c.hc == nil {
		return nil
	}
	c.enabled = true
	return c
}

func (c *Client) url(path string, query url.Values) string {
	u := c.base
	if c.apiVersion != "" {
		u += "/" + c.apiVersion
	}
	u += path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	return u
}

// do 发起请求并返回响应体；非 2xx 转为错误。
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body io.Reader) ([]byte, error) {
	if !c.enabled {
		return nil, fmt.Errorf("docker client 未初始化")
	}
	req, err := http.NewRequestWithContext(ctx, method, c.url(path, query), body)
	if err != nil {
		return nil, err
	}
	if method != http.MethodGet && body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return data, &APIError{Status: resp.StatusCode, Body: strings.TrimSpace(string(data))}
	}
	return data, nil
}

// APIError 表示 Docker API 返回的非 2xx。
type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("docker api 返回 %d: %s", e.Status, e.Body)
}

func (c *Client) get(ctx context.Context, path string, query url.Values, out interface{}) error {
	data, err := c.do(ctx, http.MethodGet, path, query, nil)
	if err != nil {
		return err
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

// Ping 检查 Docker 是否可用（读取 /_ping）。
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.do(ctx, http.MethodGet, "/_ping", nil, nil)
	return err
}

// Version 返回 Docker API 版本信息。
func (c *Client) Version(ctx context.Context) (map[string]interface{}, error) {
	var out map[string]interface{}
	err := c.get(ctx, "/version", nil, &out)
	return out, err
}

// ListContainers 列出容器（all=true 含已停止）。
func (c *Client) ListContainers(ctx context.Context, all bool) ([]containerSummary, error) {
	q := url.Values{}
	if all {
		q.Set("all", "1")
	}
	var out []containerSummary
	err := c.get(ctx, "/containers/json", q, &out)
	return out, err
}

// InspectContainer 返回容器详细信息。
func (c *Client) InspectContainer(ctx context.Context, id string) (*containerInspect, error) {
	var out containerInspect
	err := c.get(ctx, "/containers/"+url.PathEscape(id)+"/json", nil, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ContainerStats 返回单容器一次统计快照（stream=false）。
func (c *Client) ContainerStats(ctx context.Context, id string) (*rawStats, error) {
	q := url.Values{}
	q.Set("stream", "false")
	var out rawStats
	err := c.get(ctx, "/containers/"+url.PathEscape(id)+"/stats", q, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// StartContainer 启动容器（幂等）。
func (c *Client) StartContainer(ctx context.Context, id string) error {
	_, err := c.do(ctx, http.MethodPost, "/containers/"+url.PathEscape(id)+"/start", nil, nil)
	return err
}

// StopContainer 停止容器，grace 为宽限秒数。
func (c *Client) StopContainer(ctx context.Context, id string, grace int) error {
	q := url.Values{}
	q.Set("t", fmt.Sprintf("%d", grace))
	_, err := c.do(ctx, http.MethodPost, "/containers/"+url.PathEscape(id)+"/stop", q, nil)
	return err
}

// RestartContainer 重启容器，grace 为宽限秒数。
func (c *Client) RestartContainer(ctx context.Context, id string, grace int) error {
	q := url.Values{}
	q.Set("t", fmt.Sprintf("%d", grace))
	_, err := c.do(ctx, http.MethodPost, "/containers/"+url.PathEscape(id)+"/restart", q, nil)
	return err
}
