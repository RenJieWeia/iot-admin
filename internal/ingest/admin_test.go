package ingest_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RenJieWeia/iot-admin/internal/admin"
)

// 管理 API 冒烟：数据源/质量规则/结算窗口 CRUD + 统一返回结构 + rule_type 闭集校验。
func TestAdminAPICRUD(t *testing.T) {
	e := newEnv(t)
	handler := admin.New(e.pool).Handler()

	do := func(method, path, body string) (int, map[string]any) {
		t.Helper()
		var req *http.Request
		if body == "" {
			req = httptest.NewRequest(method, path, nil)
		} else {
			req = httptest.NewRequest(method, path, strings.NewReader(body))
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		var resp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("%s %s 返回不是 JSON: %s", method, path, rec.Body.String())
		}
		if _, ok := resp["code"]; !ok {
			t.Fatalf("返回结构缺少 code: %v", resp)
		}
		if _, ok := resp["message"]; !ok {
			t.Fatalf("返回结构缺少 message: %v", resp)
		}
		return rec.Code, resp
	}

	status, resp := do("POST", "/admin/v1/data-domains", `{"code":"waste","name":"污水"}`)
	if status != 200 {
		t.Fatalf("建数据域失败: %d %v", status, resp)
	}
	domain := resp["data"].(map[string]any)
	domainID := domain["id"].(float64)

	status, resp = do("POST", "/admin/v1/device-types",
		`{"code":"pump","name":"泵站","domain_id":`+jsonNum(domainID)+`}`)
	if status != 200 {
		t.Fatalf("建终端类型失败: %d %v", status, resp)
	}
	typeID := resp["data"].(map[string]any)["id"].(float64)

	status, resp = do("POST", "/admin/v1/thing-model-properties",
		`{"type_id":`+jsonNum(typeID)+`,"item_code":"level","name":"液位","value_type":"number","unit":"m","precision":2,"required":true}`)
	if status != 200 {
		t.Fatalf("建物模型属性失败: %d %v", status, resp)
	}

	status, resp = do("POST", "/admin/v1/terminals",
		`{"id":"T9","type_id":`+jsonNum(typeID)+`,"gateway_id":"GW9","name":"九号泵","enabled":true}`)
	if status != 200 {
		t.Fatalf("建终端实例失败: %d %v", status, resp)
	}

	status, resp = do("POST", "/admin/v1/quality-rules",
		`{"terminal_id":"T9","item_code":"level","rule_type":"range","params":{"min":0,"max":10}}`)
	if status != 200 {
		t.Fatalf("建质量规则失败: %d %v", status, resp)
	}

	// 非闭集 rule_type 必须被拒
	status, _ = do("POST", "/admin/v1/quality-rules",
		`{"terminal_id":"T9","item_code":"level","rule_type":"rate_of_change","params":{}}`)
	if status != http.StatusBadRequest {
		t.Fatalf("非闭集 rule_type 应返回 400，实际 %d", status)
	}

	status, resp = do("POST", "/admin/v1/settle-configs", `{"type_id":`+jsonNum(typeID)+`,"window_seconds":600}`)
	if status != 200 {
		t.Fatalf("建结算窗口失败: %d %v", status, resp)
	}

	status, resp = do("GET", "/admin/v1/quality-rules", "")
	if status != 200 {
		t.Fatalf("列表失败: %d %v", status, resp)
	}
	if items, _ := resp["data"].([]any); len(items) != 1 {
		t.Fatalf("质量规则列表长度 = %d，期望 1", len(items))
	}

	status, resp = do("PUT", "/admin/v1/quality-rules/1", `{"enabled":false}`)
	if status != 200 {
		t.Fatalf("更新失败: %d %v", status, resp)
	}
	if enabled, _ := resp["data"].(map[string]any)["enabled"].(bool); enabled {
		t.Fatal("更新未生效")
	}

	status, _ = do("DELETE", "/admin/v1/quality-rules/1", "")
	if status != 200 {
		t.Fatalf("删除失败: %d", status)
	}
	status, _ = do("GET", "/admin/v1/quality-rules/1", "")
	if status != http.StatusNotFound {
		t.Fatalf("删除后 GET 应 404，实际 %d", status)
	}

	// 结算窗口按类型精确匹配优先于全局默认
	window, err := windowForType(e, typeID)
	if err != nil {
		t.Fatalf("解析结算窗口失败: %v", err)
	}
	if window != 600 {
		t.Fatalf("type_id 精确匹配的窗口 = %d，期望 600", window)
	}
}

func jsonNum(v float64) string {
	b, _ := json.Marshal(v)
	return string(b)
}
