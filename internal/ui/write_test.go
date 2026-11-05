package ui_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/RenJieWeia/iot-admin/internal/ui"
)

// entityRows 给 6 个实体各准备一行，覆盖所有可见字段类型（文本/id/关系/枚举/布尔/数值）。
func entityRows() map[string][]map[string]any {
	return map[string][]map[string]any{
		"data-domains": {{"id": int64(1), "code": "energy", "name": "能源"}},
		"device-types": {{"id": int64(3), "code": "meter", "name": "电表", "domain_id": int64(1)}},
		"thing-model-properties": {{
			"id": int64(10), "type_id": int64(3), "item_code": "temp", "name": "温度",
			"value_type": "number", "unit": "C", "precision": int64(1), "required": true,
			"min_value": float64(0), "max_value": float64(100),
		}},
		"terminals":      {{"id": "T1", "type_id": int64(3), "gateway_id": "GW1", "name": "一号泵", "enabled": true}},
		"quality-rules":  {{"id": int64(5), "terminal_id": "T1", "item_code": "temp", "rule_type": "range", "enabled": true, "params": map[string]any{"min": float64(0), "max": float64(100)}}},
		"settle-configs": {{"id": int64(7), "domain_id": int64(1), "type_id": int64(3), "window_seconds": int64(900)}},
	}
}

func allEntitySource() *fakeSource {
	return &fakeSource{rows: entityRows()}
}

// 验收 §8 步骤 3：6 个实体各渲染一次列表页，不因字段类型差异报错。
func TestAllSixEntitiesRender(t *testing.T) {
	cases := map[string]string{
		"data-domains":           "数据域",
		"device-types":           "终端类型",
		"thing-model-properties": "物模型属性",
		"terminals":              "终端",
		"quality-rules":          "质量规则",
		"settle-configs":         "结算窗口",
	}
	for name, label := range cases {
		t.Run(name, func(t *testing.T) {
			h := ui.New(ui.Deps{Source: allEntitySource()}).Handler()
			rec := get(t, h, "/ui/"+name)
			if rec.Code != http.StatusOK {
				t.Fatalf("状态码 = %d，期望 200", rec.Code)
			}
			body := rec.Body.String()
			if !strings.Contains(body, `<table class="grid">`) {
				t.Fatalf("%s 列表缺少表格", name)
			}
			if !strings.Contains(body, label) {
				t.Fatalf("%s 缺少实体标题", name)
			}
			if !strings.Contains(body, `aria-label="分页"`) {
				t.Fatalf("%s 缺少分页器", name)
			}
		})
	}
}

// 关系列必须显示名称 + id，枚举显示中文标签，布尔显示状态文字。
func TestRelationAndEnumLabels(t *testing.T) {
	h := ui.New(ui.Deps{Source: allEntitySource()}).Handler()

	body := get(t, h, "/ui/device-types").Body.String()
	if !strings.Contains(body, "能源 (1)") {
		t.Fatalf("device-types.domain_id 应回填数据域名称: %s", head(body))
	}
	body = get(t, h, "/ui/thing-model-properties").Body.String()
	if !strings.Contains(body, "电表 (3)") {
		t.Fatal("thing-model-properties.type_id 应回填终端类型名称")
	}
	if !strings.Contains(body, "数值") {
		t.Fatal("value_type 枚举应显示中文标签")
	}
	if !strings.Contains(body, "必填") && !strings.Contains(body, "是") {
		t.Fatal("required 应显示中文状态标签")
	}
	body = get(t, h, "/ui/quality-rules").Body.String()
	if !strings.Contains(body, "范围") {
		t.Fatal("rule_type 枚举应显示中文标签")
	}
	if !strings.Contains(body, "启用") {
		t.Fatal("enabled 应显示启用/停用文字")
	}
}

// ---------- 写操作 ----------

type fakeWriter struct {
	*fakeSource
	created []url.Values
	updated []string
	deleted []string
	err     error
}

func (f *fakeWriter) Create(_ context.Context, entity string, form url.Values) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	f.created = append(f.created, form)
	return "T9", nil
}

func (f *fakeWriter) Update(_ context.Context, entity, id string, form url.Values) error {
	if f.err != nil {
		return f.err
	}
	f.updated = append(f.updated, entity+"/"+id)
	return nil
}

func (f *fakeWriter) Delete(_ context.Context, entity, id string) error {
	if f.err != nil {
		return f.err
	}
	f.deleted = append(f.deleted, entity+"/"+id)
	return nil
}

func post(t *testing.T, h http.Handler, target string, form url.Values, accept string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// 写成功后回跳同一查询状态（保留筛选），并带轻提示参数。
func TestCreateRedirectsWithFlash(t *testing.T) {
	w := &fakeWriter{fakeSource: allEntitySource()}
	h := ui.New(ui.Deps{Source: w}).Handler()

	rec := post(t, h, "/ui/terminals", url.Values{
		"id": {"T9"}, "type_id": {"3"}, "gateway_id": {"GW9"}, "name": {"九号泵"}, "enabled": {"true"},
		"returnTo": {"/ui/terminals?filter.enabled=true"},
	}, "")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("状态码 = %d，期望 303", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/ui/terminals?filter.enabled=true") || !strings.Contains(loc, "flash=ok") {
		t.Fatalf("回跳地址应保留筛选并带 flash: %s", loc)
	}
	if len(w.created) != 1 || w.created[0].Get("gateway_id") != "GW9" {
		t.Fatalf("写入表单未透传: %+v", w.created)
	}
}

// 带 Accept: application/json 时返回 {ok, redirect}，供前端就地刷新。
func TestCreateJSONResponse(t *testing.T) {
	w := &fakeWriter{fakeSource: allEntitySource()}
	h := ui.New(ui.Deps{Source: w}).Handler()

	rec := post(t, h, "/ui/terminals", url.Values{
		"id": {"T9"}, "type_id": {"3"}, "gateway_id": {"GW9"}, "enabled": {"false"},
	}, "application/json")
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200", rec.Code)
	}
	var body struct {
		OK       bool   `json:"ok"`
		Redirect string `json:"redirect"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是 JSON: %s", rec.Body.String())
	}
	if !body.OK || !strings.HasPrefix(body.Redirect, "/ui/terminals") {
		t.Fatalf("响应内容不符: %+v", body)
	}
}

// 写入失败：JSON 客户端拿到 400 + 原因；无 JS 客户端拿到回跳 + flash=error。
func TestWriteErrorPaths(t *testing.T) {
	w := &fakeWriter{fakeSource: allEntitySource(), err: errors.New("duplicate key")}
	h := ui.New(ui.Deps{Source: w}).Handler()

	rec := post(t, h, "/ui/terminals", url.Values{"id": {"T1"}}, "application/json")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("状态码 = %d，期望 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "duplicate key") {
		t.Fatalf("应带失败原因: %s", rec.Body.String())
	}

	rec = post(t, h, "/ui/terminals", url.Values{"id": {"T1"}, "returnTo": {"/ui/terminals?page=2"}}, "")
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "flash=error") {
		t.Fatalf("无 JS 路径应回跳并带错误提示: %s", rec.Header().Get("Location"))
	}
}

// 更新与删除：路径参数进 Source，回跳地址只接受站内列表地址。
func TestUpdateAndDelete(t *testing.T) {
	w := &fakeWriter{fakeSource: allEntitySource()}
	h := ui.New(ui.Deps{Source: w}).Handler()

	rec := post(t, h, "/ui/terminals/T1", url.Values{"enabled": {"false"}, "returnTo": {"/ui/terminals"}}, "")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("更新状态码 = %d，期望 303", rec.Code)
	}
	if len(w.updated) != 1 || w.updated[0] != "terminals/T1" {
		t.Fatalf("更新未透传到 Source: %+v", w.updated)
	}

	rec = post(t, h, "/ui/terminals/T1/delete", url.Values{"returnTo": {"/ui/terminals"}}, "application/json")
	if rec.Code != http.StatusOK {
		t.Fatalf("删除状态码 = %d，期望 200", rec.Code)
	}
	if len(w.deleted) != 1 || w.deleted[0] != "terminals/T1" {
		t.Fatalf("删除未透传到 Source: %+v", w.deleted)
	}

	// 外部地址不得作为回跳目标
	rec = post(t, h, "/ui/terminals", url.Values{"id": {"T9"}, "returnTo": {"https://evil.example/"}}, "")
	if loc := rec.Header().Get("Location"); strings.Contains(loc, "evil.example") {
		t.Fatalf("returnTo 未校验站内前缀: %s", loc)
	}
}

// 只读数据源拒绝写入（405），未知实体仍 404。
func TestWriteGuards(t *testing.T) {
	ro := ui.New(ui.Deps{Source: allEntitySource()}).Handler()
	if rec := post(t, ro, "/ui/terminals", url.Values{"id": {"T9"}}, ""); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("只读数据源写操作状态码 = %d，期望 405", rec.Code)
	}

	w := &fakeWriter{fakeSource: allEntitySource()}
	h := ui.New(ui.Deps{Source: w}).Handler()
	if rec := post(t, h, "/ui/nope", url.Values{}, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("未知实体状态码 = %d，期望 404", rec.Code)
	}
	if len(w.created) != 0 {
		t.Fatal("未知实体不应触发写入")
	}
}
