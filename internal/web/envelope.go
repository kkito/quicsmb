// Package web 提供标准库 HTTP 路由：绑定校验 + 委托 + 信封，判定下沉。
package web

import (
	"encoding/json"
	"net/http"
	"os"

	"quicsmb/internal/webdist"
)

// WebRoot 为空时使用内嵌产物；非空时（dev 模式 --web-root）从目录读文件。
var WebRoot = ""

// serveFile 优先从 WebRoot 读文件，回退到内嵌产物。
func serveFile(w http.ResponseWriter, r *http.Request, name, contentType string) {
	if WebRoot != "" {
		if b, err := os.ReadFile(WebRoot + "/" + name); err == nil {
			w.Header().Set("Content-Type", contentType)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(b)
			return
		}
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(webdist.MustAsset(name))
}

// ok 返回统一成功信封 {ok:true,data}。
func ok(w http.ResponseWriter, data any) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": data})
}

// fail 返回统一失败信封 {ok:false,error:{code,message}}。
func fail(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]any{
		"ok":    false,
		"error": map[string]any{"code": code, "message": msg},
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// decodeJSON 严格解析请求体 JSON，失败返回 error。
func decodeJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}
