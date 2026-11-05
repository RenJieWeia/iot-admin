package ui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// SPA 处理器：入口、深链 fallback、资源命中、方法限制。
func TestSPAHandler(t *testing.T) {
	h := Handler()

	cases := []struct {
		name   string
		method string
		path   string
		status int
	}{
		{name: "入口", method: http.MethodGet, path: "/", status: http.StatusOK},
		{name: "深链 fallback", method: http.MethodGet, path: "/terminals", status: http.StatusOK},
		{name: "异常页 fallback", method: http.MethodGet, path: "/exceptions/intervals", status: http.StatusOK},
		{name: "HEAD 允许", method: http.MethodHead, path: "/", status: http.StatusOK},
		{name: "POST 拒绝", method: http.MethodPost, path: "/terminals", status: http.StatusMethodNotAllowed},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
			if rec.Code != tc.status {
				t.Fatalf("%s %s → %d，期望 %d", tc.method, tc.path, rec.Code, tc.status)
			}
			if tc.method == http.MethodPost && rec.Header().Get("Allow") == "" {
				t.Fatalf("405 响应缺少 Allow 头")
			}
		})
	}
}

// 入口 HTML 不缓存，避免发布后仍拿旧 index；哈希资源长缓存。
func TestSPACacheControl(t *testing.T) {
	h := Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("入口 Cache-Control = %q，期望 no-cache", cc)
	}

	index := rec.Body.String()
	start := strings.Index(index, "assets/")
	if start < 0 {
		t.Fatalf("入口 HTML 未引用构建资源")
	}
	rest := index[start:]
	end := strings.IndexAny(rest, `"`)
	if end < 0 {
		t.Fatalf("入口 HTML 资源引用格式异常")
	}
	asset := "/" + rest[:end]

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, asset, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s → %d，期望 200", asset, rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Fatalf("%s Cache-Control = %q，期望 immutable", asset, cc)
	}
}

// 产物必须真的含入口文件，否则 embed 常量或构建目录写错。
func TestEmbeddedDistHasIndex(t *testing.T) {
	if _, err := distFS.ReadFile("dist/index.html"); err != nil {
		t.Fatalf("dist/index.html 未入仓: %v", err)
	}
}
