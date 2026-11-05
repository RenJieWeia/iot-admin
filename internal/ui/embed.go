// Package ui 只负责把前端构建产物（React SPA）作为静态资源提供给浏览器。
// 产物入仓（R-016）：go build 不依赖 node；运行时不需要任何模板或 SSR。
package ui

import "embed"

// distFS 是 Vite 产物（web/ 构建输出到 internal/ui/dist）。
// all: 前缀保证以 _ 或 . 开头的文件也被嵌入。
//
//go:embed all:dist
var distFS embed.FS
