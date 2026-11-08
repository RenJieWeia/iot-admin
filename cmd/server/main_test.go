package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 单二进制交付：/ 跳前端，/ui/ 由 embed 提供 SPA，API 仍只挂在各自前缀下。
func TestRoutes(t *testing.T) {
	adminHit := false
	admin := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		adminHit = true
		w.WriteHeader(http.StatusNoContent)
	})
	h := withSecurityHeaders(newMux(nil, admin))

	// 根路径 302 到 /ui/。
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("GET / → %d，期望 302", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/ui/" {
		t.Fatalf("GET / Location = %q，期望 /ui/", got)
	}

	// 前端入口 200，且必须能被浏览器挂载；入口每次回源，不吃长缓存。
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ui/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /ui/ → %d，期望 200", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, `id="root"`) {
		t.Fatalf("GET /ui/ 响应体 = %q，期望包含 id=\"root\"", body)
	}
	if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "no-cache") {
		t.Fatalf("GET /ui/ Cache-Control = %q，期望 no-cache", got)
	}

	// 深链回退到 index.html（前端路由自己解析）。
	for _, target := range []string{"/ui/e/terminals", "/ui/r/snapshot-second", "/ui/x/exception-events"} {
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s → %d，期望 200", target, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `id="root"`) {
			t.Fatalf("GET %s 未回退到 index.html", target)
		}
	}

	// favicon 与哈希资源缺失按 404 处理，不能回退成 HTML。
	for _, target := range []string{"/ui/favicon.ico", "/ui/assets/missing.js"} {
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("GET %s → %d，期望 404", target, rec.Code)
		}
		if strings.Contains(rec.Body.String(), `id="root"`) {
			t.Fatalf("GET %s 不应回退到 index.html", target)
		}
	}

	// 管理 API 挂在 /admin/v1/ 前缀下。
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/v1/meta", nil))
	if rec.Code != http.StatusNoContent || !adminHit {
		t.Fatalf("GET /admin/v1/meta → %d，未进入管理处理器", rec.Code)
	}

	// 接入端点只接受 POST，且路径准确。
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ingest/v1/messages", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /ingest/v1/messages → %d，期望 405", rec.Code)
	}

	// 安全头对 API 与页面同样生效；Radix 注入内联 style，故 style-src 放开 unsafe-inline。
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/v1/meta", nil))
	csp := rec.Header().Get("Content-Security-Policy")
	for _, want := range []string{
		"default-src 'self'",
		"script-src 'self'",
		"style-src 'self' 'unsafe-inline'",
		"frame-ancestors 'none'",
	} {
		if !strings.Contains(csp, want) {
			t.Fatalf("CSP = %q，期望包含 %q", csp, want)
		}
	}
	if strings.Contains(csp, "script-src 'self' 'unsafe-inline'") {
		t.Fatalf("CSP = %q，脚本不允许放开 unsafe-inline", csp)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q，期望 nosniff", got)
	}
	if got := rec.Header().Get("Referrer-Policy"); got != "same-origin" {
		t.Fatalf("Referrer-Policy = %q，期望 same-origin", got)
	}
}
