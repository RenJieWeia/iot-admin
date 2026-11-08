package broker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeBroMQ 按真实 BroMQ 的路径与响应形状模拟：登录 → 建用户 → 建 ACL → 列表 → 删除。
func fakeBroMQ(t *testing.T, calls *[]string) *httptest.Server {
	t.Helper()
	nextID := int64(10)
	users := map[int64]string{5: "GW-A"}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls = append(*calls, r.Method+" "+r.URL.Path)
		if r.URL.Path == "/api/auth/login" {
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["username"] != "renjie" || body["password"] != "pw" {
				http.Error(w, `{"error":"invalid credentials"}`, http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"token": "tk"})
			return
		}
		if r.Header.Get("Authorization") != "Bearer tk" {
			http.Error(w, `{"error":"missing authorization header"}`, http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/mqtt/users":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["username"] != "GW-A" || body["password"] == "" || body["password"] == nil {
				t.Errorf("建用户请求体不符: %v", body)
			}
			nextID++
			users[nextID] = body["username"].(string)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": nextID, "username": body["username"]})
		case r.Method == http.MethodPost && r.URL.Path == "/api/acl":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["topic"] != "iot/ingest/GW-A" || body["permission"] != "pub" {
				t.Errorf("ACL 请求体不符: %v", body)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 1})
		case r.Method == http.MethodGet && r.URL.Path == "/api/mqtt/users":
			data := make([]map[string]any, 0, len(users))
			for id, name := range users {
				data = append(data, map[string]any{"id": id, "username": name})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/mqtt/users/"):
			var id int64
			_, _ = fmt.Sscanf(r.URL.Path, "/api/mqtt/users/%d", &id)
			delete(users, id)
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestRegisterCreatesUserAndACL(t *testing.T) {
	var calls []string
	srv := fakeBroMQ(t, &calls)
	defer srv.Close()

	password, err := New(srv.URL, "renjie", "pw").Register(context.Background(), Gateway{ID: "GW-A", Name: "一号网关"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if len(password) < 32 {
		t.Fatalf("应返回一次性密码，实际 %q", password)
	}
	want := []string{"POST /api/auth/login", "POST /api/mqtt/users", "POST /api/acl"}
	if strings.Join(calls, "|") != strings.Join(want, "|") {
		t.Fatalf("调用序列 = %v，期望 %v", calls, want)
	}
}

func TestUnregisterDeletesUserThenIsIdempotent(t *testing.T) {
	var calls []string
	srv := fakeBroMQ(t, &calls)
	defer srv.Close()
	c := New(srv.URL, "renjie", "pw")

	if err := c.Unregister(context.Background(), "GW-A"); err != nil {
		t.Fatalf("Unregister: %v", err)
	}
	// 第二次：列表里已没有 GW-A，不再发 DELETE，也不报错。
	calls = nil
	if err := c.Unregister(context.Background(), "GW-A"); err != nil {
		t.Fatalf("二次 Unregister 应幂等: %v", err)
	}
	for _, call := range calls {
		if strings.HasPrefix(call, "DELETE") {
			t.Fatalf("不存在时不应发 DELETE，实际 %v", calls)
		}
	}
}

func TestBrokerErrorCarriesStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	_, err := New(srv.URL, "", "").Register(context.Background(), Gateway{ID: "GW-A"})
	if err == nil {
		t.Fatal("未配置账号应报错")
	}
	var brokerErr *Error
	if errors.As(err, &brokerErr) && brokerErr.Status != http.StatusInternalServerError {
		t.Fatalf("状态码 = %d", brokerErr.Status)
	}
}
