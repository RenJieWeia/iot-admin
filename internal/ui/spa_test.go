package ui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func fixtureFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte(`<!doctype html><html><body><div id="root"></div></body></html>`),
		},
		"assets/app-abc123.js": &fstest.MapFile{Data: []byte(`console.log(1)`)},
	}
}

func TestSPAHandler(t *testing.T) {
	h := NewSPAHandler(fixtureFS())

	cases := []struct {
		name       string
		method     string
		target     string
		wantStatus int
		wantBody   string
		wantCache  string
		wantType   string
	}{
		{name: "根路径出入口", method: http.MethodGet, target: "/", wantStatus: http.StatusOK, wantBody: `id="root"`, wantCache: "no-cache", wantType: "text/html; charset=utf-8"},
		{name: "入口文件", method: http.MethodGet, target: "/index.html", wantStatus: http.StatusOK, wantBody: `id="root"`, wantCache: "no-cache", wantType: "text/html; charset=utf-8"},
		{name: "深链回退", method: http.MethodGet, target: "/e/terminals", wantStatus: http.StatusOK, wantBody: `id="root"`, wantCache: "no-cache", wantType: "text/html; charset=utf-8"},
		{name: "哈希资源长缓存", method: http.MethodGet, target: "/assets/app-abc123.js", wantStatus: http.StatusOK, wantBody: "console.log(1)", wantCache: "public, max-age=31536000, immutable"},
		{name: "缺失资源不回退", method: http.MethodGet, target: "/assets/missing.js", wantStatus: http.StatusNotFound},
		{name: "favicon 缺失", method: http.MethodGet, target: "/favicon.ico", wantStatus: http.StatusNotFound},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.target, nil))
			if rec.Code != tc.wantStatus {
				t.Fatalf("GET %s → %d，期望 %d", tc.target, rec.Code, tc.wantStatus)
			}
			if tc.wantBody != "" && !strings.Contains(rec.Body.String(), tc.wantBody) {
				t.Fatalf("GET %s 响应体 = %q，期望包含 %q", tc.target, rec.Body.String(), tc.wantBody)
			}
			if tc.wantCache != "" {
				if got := rec.Header().Get("Cache-Control"); got != tc.wantCache {
					t.Fatalf("GET %s Cache-Control = %q，期望 %q", tc.target, got, tc.wantCache)
				}
			}
			if tc.wantType != "" {
				if got := rec.Header().Get("Content-Type"); got != tc.wantType {
					t.Fatalf("GET %s Content-Type = %q，期望 %q", tc.target, got, tc.wantType)
				}
			}
		})
	}
}

func TestSPAHandlerHeadAndMethod(t *testing.T) {
	h := NewSPAHandler(fixtureFS())

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodHead, "/", nil))
	if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
		t.Fatalf("HEAD / → %d，body %d 字节，期望 200 且无 body", rec.Code, rec.Body.Len())
	}
	if got := rec.Header().Get("Content-Length"); got == "" {
		t.Fatal("HEAD / 缺少 Content-Length")
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST / → %d，期望 405", rec.Code)
	}
	if got := rec.Header().Get("Allow"); got != "GET, HEAD" {
		t.Fatalf("POST / Allow = %q，期望 GET, HEAD", got)
	}
}

func TestSPAHandlerMissingIndex(t *testing.T) {
	h := NewSPAHandler(fstest.MapFS{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/e/terminals", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("缺 index.html 时 GET /e/terminals → %d，期望 500", rec.Code)
	}
}

// embed 里的 dist 必须能编译进二进制，且入口可出页面（占位或真实构建产物都含 id="root"）。
func TestEmbeddedHandler(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / → %d，期望 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `id="root"`) {
		t.Fatalf("嵌入的 index.html 不含 id=\"root\"：%q", rec.Body.String())
	}
}
