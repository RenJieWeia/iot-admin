package ui

import (
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
)

// SPAHandler 从 fsys 根提供静态文件：命中直出，未命中的非资源路径回退 index.html。
// 资源类路径（assets/ 下或带扩展名）缺失时直接 404，避免前端拿到一页 HTML 当 JS/图片。
type SPAHandler struct {
	fsys fs.FS
}

// NewSPAHandler 用任意 fs.FS 构造 SPA 处理器（用例里用 fstest.MapFS 驱动）。
func NewSPAHandler(fsys fs.FS) http.Handler {
	return &SPAHandler{fsys: fsys}
}

func (h *SPAHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method_not_allowed", http.StatusMethodNotAllowed)
		return
	}

	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name == "" {
		name = "index.html"
	}
	if data, err := fs.ReadFile(h.fsys, name); err == nil {
		h.write(w, r, name, data)
		return
	}
	if strings.HasPrefix(name, "assets/") || path.Ext(name) != "" {
		http.NotFound(w, r)
		return
	}

	index, err := fs.ReadFile(h.fsys, "index.html")
	if err != nil {
		http.Error(w, "前端产物缺失：先 cd web && npm run build", http.StatusInternalServerError)
		return
	}
	h.write(w, r, "index.html", index)
}

// write 统一打缓存头：哈希资源长缓存 immutable，入口与其它的静态文件每次回源。
func (h *SPAHandler) write(w http.ResponseWriter, r *http.Request, name string, data []byte) {
	header := w.Header()
	if path.Ext(name) == ".html" {
		header.Set("Content-Type", "text/html; charset=utf-8")
	} else if contentType := mime.TypeByExtension(path.Ext(name)); contentType != "" {
		header.Set("Content-Type", contentType)
	} else {
		header.Set("Content-Type", "application/octet-stream")
	}
	if strings.HasPrefix(name, "assets/") {
		header.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		header.Set("Cache-Control", "no-cache")
	}
	header.Set("Content-Length", strconv.Itoa(len(data)))
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	_, _ = w.Write(data)
}
