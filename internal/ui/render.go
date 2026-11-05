package ui

import (
	"embed"
	"html/template"
	"io/fs"
	"net/http"
	"strings"

	"github.com/RenJieWeia/iot-admin/internal/admin"
)

// 模板与静态资源随二进制走（R-008：单二进制、无构建链、无 CDN）。
//
//go:embed templates/*.gohtml
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

func loadTemplates() (*template.Template, error) {
	return template.New("ui").Funcs(template.FuncMap{
		"contains": func(list []string, want string) bool {
			for _, s := range list {
				if s == want {
					return true
				}
			}
			return false
		},
		"sortMark": func(key, sort, order string) string {
			if key != sort {
				return ""
			}
			if order == "asc" {
				return " ↑"
			}
			return " ↓"
		},
		"selected": func(have, want string) string {
			if have == want {
				return "selected"
			}
			return ""
		},
		"optionLabel": admin.OptionLabel,
		"fieldMeta": func(entity, key string) admin.FieldMeta {
			f, _ := admin.FieldMetaOf(entity, key)
			return f
		},
	}).ParseFS(templateFS, "templates/*.gohtml")
}

// render 写出整页或局部片段；片段名与模板 define 名一致（listRegion 用于就地刷新）。
func (h *Handler) render(w http.ResponseWriter, status int, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := h.tpl.ExecuteTemplate(w, name, data); err != nil {
		h.log.Error("模板渲染失败", "template", name, "err", err)
	}
}

// staticHandler 提供 /ui/static/ 下的 embed 资源。
func staticHandler() (http.Handler, error) {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		return nil, err
	}
	return http.StripPrefix("/ui/static/", http.FileServer(http.FS(sub))), nil
}

// partialRequested 判断请求是否只要列表区域（前端就地刷新）。
func partialRequested(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("X-Partial"), "list")
}
