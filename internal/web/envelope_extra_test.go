package web

import (
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestServeFileWebRootAndDecodeJSON(t *testing.T) {
	// WebRoot 模式：从目录读
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/index.html", []byte("<h1>dev</h1>"), 0644); err != nil {
		t.Fatal(err)
	}
	old := WebRoot
	WebRoot = dir
	defer func() { WebRoot = old }()
	w := httptest.NewRecorder()
	serveFile(w, httptest.NewRequest("GET", "/", nil), "index.html", "text/html")
	if !strings.Contains(w.Body.String(), "dev") {
		t.Fatalf("webroot = %q", w.Body.String())
	}
	// decodeJSON：合法 + 未知字段拒绝
	var v struct {
		Name string `json:"name"`
	}
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{"name":"a"}`))
	if err := decodeJSON(r, &v); err != nil || v.Name != "a" {
		t.Fatalf("decode = %v %+v", v, err)
	}
	r2 := httptest.NewRequest("POST", "/", strings.NewReader(`{"bogus":1}`))
	if err := decodeJSON(r2, &v); err == nil {
		t.Fatal("want error on unknown field")
	}
	// fail 信封形状
	w2 := httptest.NewRecorder()
	fail(w2, 400, "BAD", "msg")
	if w2.Code != 400 || !strings.Contains(w2.Body.String(), "BAD") {
		t.Fatalf("fail = %d %q", w2.Code, w2.Body.String())
	}
}
