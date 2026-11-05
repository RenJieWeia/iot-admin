package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RenJieWeia/iot-admin/internal/ui"
)

// 验收 §9：GET / 302 → /ui/；GET /ui/ 302 → /ui/data-domains；静态资源 200。
func TestRoutes(t *testing.T) {
	h := newMux(nil, http.NotFoundHandler(), ui.New(ui.Deps{}).Handler())

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/ui/" {
		t.Fatalf("GET / → %d %q，期望 302 /ui/", rec.Code, rec.Header().Get("Location"))
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ui/", nil))
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/ui/data-domains" {
		t.Fatalf("GET /ui/ → %d %q，期望 302 /ui/data-domains", rec.Code, rec.Header().Get("Location"))
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ui/static/app.css", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /ui/static/app.css → %d，期望 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "css") {
		t.Fatalf("Content-Type = %q，期望 css", ct)
	}
}
