package admin

import (
	"crypto/sha256"
	"fmt"
	"sync"
	"time"

	pb "server_monitor_service/internal/protocol/pb"
)

// ServerInfo 服务器信息。
type ServerInfo struct {
	ID           string    `json:"id"`
	Hostname     string    `json:"hostname"`
	IP           string    `json:"ip"`
	Port         int32     `json:"port"`
	GrpcPort     int32     `json:"grpcPort"`
	OS           string    `json:"os"`
	Arch         string    `json:"arch"`
	Capabilities []string  `json:"capabilities"`
	Status       string    `json:"status"` // online, offline
	LastSeen     time.Time `json:"lastSeen"`
	RegisteredAt time.Time `json:"registeredAt"`
}

// Store 内存存储，缓存服务器列表与异常。
type Store struct {
	mu      sync.RWMutex
	servers map[string]*ServerInfo
	errors  map[string][]*pb.ErrorReport
	maxErrors int
}

// NewStore 创建存储。
func NewStore() *Store {
	return &Store{
		servers:   make(map[string]*ServerInfo),
		errors:    make(map[string][]*pb.ErrorReport),
		maxErrors: 10,
	}
}

// Register 注册/更新服务器。
func (s *Store) Register(req *pb.RegisterRequest) *ServerInfo {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := generateID(req.Hostname, int(req.GrpcPort))

	info, exists := s.servers[id]
	if !exists {
		info = &ServerInfo{
			ID:           id,
			RegisteredAt: time.Now(),
		}
		s.servers[id] = info
	}

	info.Hostname = req.Hostname
	info.IP = req.Ip
	info.Port = req.Port
	info.GrpcPort = req.GrpcPort
	info.OS = req.OsName
	info.Arch = req.Arch
	info.Capabilities = req.Capabilities
	info.Status = "online"
	info.LastSeen = time.Now()

	return info
}

// AddError 添加异常报告。
func (s *Store) AddError(serverID string, report *pb.ErrorReport) {
	s.mu.Lock()
	defer s.mu.Unlock()

	errors := s.errors[serverID]
	errors = append(errors, report)
	if len(errors) > s.maxErrors {
		errors = errors[len(errors)-s.maxErrors:]
	}
	s.errors[serverID] = errors
}

// GetServer 获取服务器信息。
func (s *Store) GetServer(id string) *ServerInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.servers[id]
}

// ListServers 获取所有服务器。
func (s *Store) ListServers() []*ServerInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var list []*ServerInfo
	for _, info := range s.servers {
		list = append(list, info)
	}
	return list
}

// GetErrors 获取服务器异常列表。
func (s *Store) GetErrors(serverID string) []*pb.ErrorReport {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.errors[serverID]
}

// SetOffline 标记服务器离线。
func (s *Store) SetOffline(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if info, exists := s.servers[id]; exists {
		info.Status = "offline"
	}
}

// RemoveServer 移除服务器。
func (s *Store) RemoveServer(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.servers, id)
	delete(s.errors, id)
}

// generateID 生成服务器 ID（SHA256 前 12 位）。
func generateID(hostname string, grpcPort int) string {
	key := fmt.Sprintf("%s:%d", hostname, grpcPort)
	hash := sha256.Sum256([]byte(key))
	return fmt.Sprintf("%x", hash[:6])
}
