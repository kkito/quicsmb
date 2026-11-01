// Package webdist 内嵌前端打包产物（scripts/build_web.cjs 生成）。
//
//go:generate node ../../scripts/build_web.cjs
package webdist

import (
	"embed"
	"fmt"
)

//go:embed quicsmb-web.js index.html style.css
var dist embed.FS

// MustAsset 返回内嵌产物；缺失时返回最小占位，保证未打包也能启动。
func MustAsset(name string) []byte {
	if b, err := dist.ReadFile(name); err == nil {
		return b
	}
	return []byte(fmt.Sprintf("<!-- missing embed asset: %s (run scripts/build_web.cjs) -->", name))
}
