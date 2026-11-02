package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"quicsmb/internal/session"
	"quicsmb/internal/tunnel"
	"quicsmb/internal/webdist"
)

type fakeManager struct {
	entries []session.Entry
	logs    []session.LogEntry
	rx, tx  uint64
}

func (f *fakeManager) List() []session.Entry        { return f.entries }
func (f *fakeManager) Logs(limit int) []session.LogEntry {
	if limit <= 0 || limit > len(f.logs) {
		return f.logs
	}
	return f.logs[len(f.logs)-limit:]
}
func (f *fakeManager) Count() int             { return len(f.entries) }
func (f *fakeManager) Totals() (uint64, uint64) { return f.rx, f.tx }

func newRouter() *http.ServeMux {
	m := &fakeManager{
		entries: []session.Entry{{
			ID: 7, Mode: "client", Peer: "127.0.0.1:1",
			Started: time.Now(),
			Stats:   tunnel.Stats{ID: 7, BytesUp: 10, BytesDown: 20, Command: "READ", Dialect: "SMB 3.1.1"},
		}},
		logs:   []session.LogEntry{{Time: "t", Dir: "connect", Summary: "hi"}},
		rx:     20,
		tx:     10,
	}
	return NewRouter(m, "test", "client")
}

func decode(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("not JSON: %v %q", err, w.Body.String())
	}
	return v
}

func TestHealthz(t *testing.T) {
	r := newRouter()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/healthz", nil))
	v := decode(t, w)
	if v["ok"] != true {
		t.Fatalf("healthz = %v", v)
	}
}

func TestStatus(t *testing.T) {
	r := newRouter()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/status", nil))
	v := decode(t, w)
	d := v["data"].(map[string]any)
	if d["mode"] != "client" || d["version"] != "test" {
		t.Fatalf("status = %v", d)
	}
	if d["sessions"].(float64) != 1 {
		t.Fatalf("sessions = %v", d["sessions"])
	}
}

func TestSessions(t *testing.T) {
	r := newRouter()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/sessions", nil))
	v := decode(t, w)
	d := v["data"].(map[string]any)
	ss := d["sessions"].([]any)
	if len(ss) != 1 {
		t.Fatalf("sessions = %v", d)
	}
	row := ss[0].(map[string]any)
	if row["command"] != "READ" || row["dialect"] != "SMB 3.1.1" {
		t.Fatalf("row = %v", row)
	}
}

func TestLogs(t *testing.T) {
	r := newRouter()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/logs?limit=10", nil))
	v := decode(t, w)
	d := v["data"].(map[string]any)
	if len(d["logs"].([]any)) != 1 {
		t.Fatalf("logs = %v", d)
	}
}

func TestLogsBadLimit(t *testing.T) {
	r := newRouter()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/logs?limit=-1", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code = %d", w.Code)
	}
}

func TestIndexHTML(t *testing.T) {
	r := newRouter()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 200 {
		t.Fatalf("code = %d", w.Code)
	}
	_ = webdist.MustAsset
}

type qvfsFakeManager struct {
	fakeManager
}

func (f *qvfsFakeManager) Qvfs() map[string]any {
	return map[string]any{"dirty": 3, "hitRate": 0.5}
}

func TestStatusWithQvfs(t *testing.T) {
	m := &qvfsFakeManager{fakeManager: fakeManager{rx: 1, tx: 2}}
	r := NewRouter(m, "qvfs", "qvps")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/status", nil))
	v := decode(t, w)
	d := v["data"].(map[string]any)
	q, ok := d["qvfs"].(map[string]any)
	if !ok || q["dirty"].(float64) != 3 {
		t.Fatalf("qvfs = %v", d["qvfs"])
	}
}

func TestStatusWithoutQvfsStaysCompatible(t *testing.T) {
	r := newRouter()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/status", nil))
	v := decode(t, w)
	d := v["data"].(map[string]any)
	if _, ok := d["qvfs"]; ok {
		t.Fatalf("legacy manager must not have qvfs: %v", d)
	}
}
