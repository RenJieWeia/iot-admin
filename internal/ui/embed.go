// Package ui 把管理后台前端产物编进二进制：go:embed 的 dist 目录由 `cd web && npm run build` 生成。
package ui

import (
	"embed"
	"io/fs"
	"net/http"
)

// dist 是 vite 的构建产物目录。仓库内保留占位 index.html，未构建前端时也能编译；
// 真实产物由 make web-build 覆盖，构建后随 go build 一起打进单二进制。
//
//go:embed all:dist
var dist embed.FS

// Handler 提供 /ui/ 下的 SPA，挂载方式：http.StripPrefix("/ui/", ui.Handler())。
func Handler() http.Handler {
	root, err := fs.Sub(dist, "dist")
	if err != nil {
		// dist 是编译期存在的目录，Sub 失败说明 embed 声明被改坏（开发期错误，直接暴露）。
		panic(err)
	}
	return NewSPAHandler(root)
}
