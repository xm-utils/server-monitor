package store

import (
	"sync"

	"server_monitor_service/internal/model"
)

// Store 保存系统指标与 Docker 指标的最近采样，供页面与 API 读取。
// 系统指标按整体快照缓存；Docker 指标按每次采集的容器列表快照缓存。
type Store struct {
	system *ring[model.SystemSnapshot]
	docker *ring[model.DockerListResult]
}

// New 创建 Store，size 为每类指标保留的最大采样条数（FR-04）。
func New(size int) *Store {
	if size <= 0 {
		size = 1
	}
	return &Store{
		system: newRing[model.SystemSnapshot](size),
		docker: newRing[model.DockerListResult](size),
	}
}

func (s *Store) PushSystem(v model.SystemSnapshot) { s.system.Push(v) }

func (s *Store) LatestSystem() (model.SystemSnapshot, bool) { return s.system.Latest() }

func (s *Store) SystemHistory(limit int) []model.SystemSnapshot { return s.system.History(limit) }

func (s *Store) PushDocker(v model.DockerListResult) { s.docker.Push(v) }

func (s *Store) LatestDocker() (model.DockerListResult, bool) { return s.docker.Latest() }

func (s *Store) DockerHistory(limit int) []model.DockerListResult { return s.docker.History(limit) }

// ring 是并发安全的固定容量环形缓冲区。
type ring[T any] struct {
	mu    sync.RWMutex
	buf   []T
	size  int
	next  int
	count int
}

func newRing[T any](size int) *ring[T] {
	return &ring[T]{buf: make([]T, size), size: size}
}

// Push 写入一条数据，超过容量后覆盖最旧数据。
func (r *ring[T]) Push(v T) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf[r.next] = v
	r.next = (r.next + 1) % r.size
	if r.count < r.size {
		r.count++
	}
}

// Latest 返回最近一次写入的数据。
func (r *ring[T]) Latest() (T, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var zero T
	if r.count == 0 {
		return zero, false
	}
	idx := (r.next - 1 + r.size) % r.size
	return r.buf[idx], true
}

// History 返回按时间升序排列的采样；limit<=0 表示全部。
func (r *ring[T]) History(limit int) []T {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.count == 0 {
		return nil
	}
	start := r.next - r.count
	if start < 0 {
		start += r.size
	}
	out := make([]T, 0, r.count)
	for i := 0; i < r.count; i++ {
		out = append(out, r.buf[(start+i)%r.size])
	}
	if limit > 0 && limit < len(out) {
		out = out[len(out)-limit:]
	}
	return out
}
