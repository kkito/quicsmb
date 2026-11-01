// Package web 提供标准库 HTTP 路由：绑定校验 + 委托 + 信封，判定下沉。
// 对应 mytun internal/web，资源换成 tunnel 会话。
package web

import (
	"net/http"
	"strconv"

	"quicsmb/internal/session"
)

// Manager 是 handler 需要的能力子集，便于单测 fake。
type Manager interface {
	List() []session.Entry
	Logs(limit int) []session.LogEntry
	Count() int
	Totals() (rx, tx uint64)
}

// Server 持有 Manager 与版本信息。
type Server struct {
	m       Manager
	version string
	mode    string
}

// NewRouter 构造标准库 ServeMux。version 写入 status/version 字段。
func NewRouter(m Manager, version, mode string) *http.ServeMux {
	s := &Server{m: m, version: version, mode: mode}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		serveFile(w, r, "index.html", "text/html; charset=utf-8")
	})
	mux.HandleFunc("GET /quicsmb-web.js", func(w http.ResponseWriter, r *http.Request) {
		serveFile(w, r, "quicsmb-web.js", "application/javascript")
	})
	mux.HandleFunc("GET /style.css", func(w http.ResponseWriter, r *http.Request) {
		serveFile(w, r, "style.css", "text/css; charset=utf-8")
	})
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("GET /api/sessions", s.handleSessions)
	mux.HandleFunc("GET /api/logs", s.handleLogs)
	return mux
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	ok(w, map[string]any{"status": "ok"})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	rx, tx := s.m.Totals()
	ok(w, map[string]any{
		"mode":     s.mode,
		"sessions": s.m.Count(),
		"stats": map[string]any{
			"rxBytes": rx,
			"txBytes": tx,
		},
		"version": s.version,
	})
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	entries := s.m.List()
	out := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		out = append(out, map[string]any{
			"id":        e.ID,
			"mode":      e.Mode,
			"peer":      e.Peer,
			"started":   e.Started.Format("15:04:05"),
			"bytesUp":   e.Stats.BytesUp,
			"bytesDown": e.Stats.BytesDown,
			"command":   e.Stats.Command,
			"dialect":   e.Stats.Dialect,
		})
	}
	ok(w, map[string]any{"sessions": out})
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	limit := 0
	if str := r.URL.Query().Get("limit"); str != "" {
		n, err := strconv.Atoi(str)
		if err != nil || n < 0 {
			fail(w, http.StatusBadRequest, "BAD_LIMIT", "invalid limit: "+str)
			return
		}
		limit = n
	}
	ok(w, map[string]any{"logs": s.m.Logs(limit)})
}
