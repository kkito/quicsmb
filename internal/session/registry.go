// Package session 管理隧道会话表：多路复用计数、环形日志、统计快照。
// 对应 C 版 main.c 的 g_sessions + web 面板需要的数据面。
package session

import (
	"sync"
	"time"

	"quicsmb/internal/tunnel"
)

// Entry 是单条会话记录。
type Entry struct {
	ID      uint64
	Mode    string // client | server
	Started time.Time
	Peer    string
	Stats   tunnel.Stats
}

// LogEntry 是环形日志一条。
type LogEntry struct {
	Time    string
	Dir     string
	Summary string
}

// Registry 是线程安全的会话注册表 + 环形日志。
type Registry struct {
	mu       sync.Mutex
	max      int
	sessions map[uint64]*tunnel.Session
	modes    map[uint64]string
	peers    map[uint64]string
	started  map[uint64]time.Time
	logs     []LogEntry
	rxBytes  uint64
	txBytes  uint64
}

const maxLogs = 200

// New 创建注册表；max<=0 表示不限会话数。
func New(max int) *Registry {
	return &Registry{
		max:      max,
		sessions: map[uint64]*tunnel.Session{},
		modes:    map[uint64]string{},
		peers:    map[uint64]string{},
		started:  map[uint64]time.Time{},
	}
}

// Add 注册 session，超限返回 false。
func (r *Registry) Add(s *tunnel.Session, mode, peer string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.max > 0 && len(r.sessions) >= r.max {
		return false
	}
	r.sessions[s.ID()] = s
	r.modes[s.ID()] = mode
	r.peers[s.ID()] = peer
	r.started[s.ID()] = time.Now()
	return true
}

// Remove 注销 session。
func (r *Registry) Remove(id uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sessions, id)
	delete(r.modes, id)
	delete(r.peers, id)
	delete(r.started, id)
}

// Count 返回当前会话数。
func (r *Registry) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.sessions)
}

// List 返回会话快照。
func (r *Registry) List() []Entry {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Entry, 0, len(r.sessions))
	for id, s := range r.sessions {
		out = append(out, Entry{
			ID:      id,
			Mode:    r.modes[id],
			Peer:    r.peers[id],
			Started: r.started[id],
			Stats:   s.Stats(),
		})
	}
	return out
}

// PushLog 追加环形日志。
func (r *Registry) PushLog(dir, summary string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.logs = append(r.logs, LogEntry{Time: time.Now().Format(time.RFC3339), Dir: dir, Summary: summary})
	if len(r.logs) > maxLogs {
		r.logs = append([]LogEntry(nil), r.logs[len(r.logs)-maxLogs:]...)
	}
}

// Logs 返回最近 limit 条（limit<=0 返回全部）。
func (r *Registry) Logs(limit int) []LogEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	if limit <= 0 || limit > len(r.logs) {
		limit = len(r.logs)
	}
	out := make([]LogEntry, limit)
	copy(out, r.logs[len(r.logs)-limit:])
	return out
}

// AddCounters 累加传输字节（session 结束时调用）。
func (r *Registry) AddCounters(up, down uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.txBytes += up
	r.rxBytes += down
}

// Totals 返回累计字节。
func (r *Registry) Totals() (rx, tx uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rxBytes, r.txBytes
}
