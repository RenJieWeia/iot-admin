package main

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/RenJieWeia/iot-admin/internal/ui"
)

// 验收 task-004 §8：GET / 302 → /ui/；/ui/ 及深链返回 SPA 入口；产物资源可取；CSP 生效。
func TestRoutes(t *testing.T) {
	h := withSecurityHeaders(newMux(nil, http.NotFoundHandler(), ui.Handler()))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/ui/" {
		t.Fatalf("GET / → %d %q，期望 302 /ui/", rec.Code, rec.Header().Get("Location"))
	}

	// /ui/ 现在是 SPA 入口（R-017 删掉了 SSR 的 302 跳转），前端路由自己选第一个实体。
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ui/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /ui/ → %d，期望 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("GET /ui/ Content-Type = %q，期望 html", ct)
	}
	index := rec.Body.String()
	if !strings.Contains(index, `id="root"`) {
		t.Fatalf("GET /ui/ 未返回 SPA 入口: %q", index)
	}

	// 深链刷新走 index.html fallback，而不是 404。
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ui/exceptions/events", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `id="root"`) {
		t.Fatalf("GET /ui/exceptions/events → %d，期望 index.html fallback", rec.Code)
	}

	// 产物里的哈希资源必须真的能取到（否则 embed 路径写错）。
	asset := regexp.MustCompile(`assets/[A-Za-z0-9._-]+\.js`).FindString(index)
	if asset == "" {
		t.Fatalf("入口 HTML 未引用构建资源: %q", index)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ui/"+asset, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /ui/%s → %d，期望 200", asset, rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Fatalf("哈希资源 Cache-Control = %q，期望 immutable", cc)
	}

	// 管理面同源，CSP 不允许外源。
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ui/", nil))
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'self'") {
		t.Fatalf("CSP = %q，期望含 default-src 'self'", csp)
	}
}
