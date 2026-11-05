package ui

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// spaHandler 服务 /ui/*：命中真实文件就原样返回，未命中的路径一律回退 index.html，
// 交给前端路由（/ui/terminals、/ui/exceptions/events 等深链刷新都能用）。
type spaHandler struct {
	dist  fs.FS
	files http.Handler
}

// Handler 返回管理后台 SPA 的静态处理器（挂到 /ui/ 下，调用方负责 StripPrefix）。
func Handler() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		// dist 由 //go:embed 固定，取不到说明产物缺失或常量写错：这是构建期错误，直接暴露。
		panic("ui: 读取嵌入的 SPA 产物失败: " + err.Error())
	}
	return &spaHandler{dist: sub, files: http.FileServer(http.FS(sub))}
}

func (h *spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method_not_allowed", http.StatusMethodNotAllowed)
		return
	}

	cleaned := path.Clean("/" + r.URL.Path)
	if cleaned == "/" || cleaned == "/index.html" {
		h.serveIndex(w, r)
		return
	}
	if h.exists(cleaned) {
		// 带内容哈希的资源可长缓存；其余（如 favicon）交给 FileServer 的默认策略。
		if strings.HasPrefix(cleaned, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		h.files.ServeHTTP(w, r)
		return
	}
	h.serveIndex(w, r)
}

// exists 判断产物里是否有这个文件（目录不算命中，交给 fallback）。
func (h *spaHandler) exists(cleaned string) bool {
	f, err := h.dist.Open(strings.TrimPrefix(cleaned, "/"))
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	return err == nil && !info.IsDir()
}

// serveIndex 直接写入口 HTML：不走 FileServer，避免它对 "/index.html" 做目录重定向（301）。
// 入口不放缓存，保证发布后立即生效。
func (h *spaHandler) serveIndex(w http.ResponseWriter, r *http.Request) {
	page, err := fs.ReadFile(h.dist, "index.html")
	if err != nil {
		http.Error(w, "spa_index_missing", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(page)
}
