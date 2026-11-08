package broker

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRegisterAndUnregisterUseBrokerHTTPAPI(t *testing.T) {
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path+" "+r.Header.Get("Authorization"))
		if r.Method == http.MethodPost {
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), `"gatewayId":"GW-A"`) {
				t.Fatalf("注册体缺少 gatewayId: %s", body)
			}
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := New(srv.URL, "tk")
	if err := c.Register(context.Background(), Gateway{ID: "GW-A", Name: "网关 A", Enabled: true}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := c.Unregister(context.Background(), "GW-A"); err != nil {
		t.Fatalf("Unregister: %v", err)
	}
	want := []string{"POST /api/gateways Bearer tk", "DELETE /api/gateways/GW-A Bearer tk"}
	if strings.Join(calls, "|") != strings.Join(want, "|") {
		t.Fatalf("调用序列 = %v，期望 %v", calls, want)
	}
}

func TestUnregisterTreats404AsGone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()
	if err := New(srv.URL, "").Unregister(context.Background(), "GW-X"); err != nil {
		t.Fatalf("404 应视为已注销: %v", err)
	}
}

func TestBrokerErrorCarriesStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	err := New(srv.URL, "").Register(context.Background(), Gateway{ID: "GW-A"})
	var brokerErr *Error
	if !errors.As(err, &brokerErr) || brokerErr.Status != http.StatusInternalServerError {
		t.Fatalf("应返回带状态码的 Broker Error，实际 %v", err)
	}
}
