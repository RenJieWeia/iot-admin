package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
)

// metaFieldKeys 是 MetaField 的 JSON 契约全字段（前端 TS 类型的唯一依据）。
var metaFieldKeys = []string{
	"key", "label", "type", "visible", "sortable", "filterable", "filterType",
	"operators", "operatorLabels", "optionsSource", "options", "advancedOnly", "widthStrategy",
}

// toneClosedSet 是状态色语义闭集（颜色不是唯一载体，文字始终显示）。
var toneClosedSet = map[string]bool{"neutral": true, "positive": true, "negative": true, "warning": true}

func decodeMeta(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	rec := httptest.NewRecorder()
	New(nil).Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/v1/_meta", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("_meta 状态码 = %d，期望 200（nil pool 也必须可用）: %s", rec.Code, rec.Body.String())
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("_meta 不是合法 JSON: %v", err)
	}
	for _, key := range []string{"code", "message", "data"} {
		if _, ok := envelope[key]; !ok {
			t.Fatalf("_meta 响应缺少字段 %q", key)
		}
	}
	if got := string(envelope["code"]); got != "0" {
		t.Fatalf("code = %s，期望 0", got)
	}
	var data map[string]json.RawMessage
	if err := json.Unmarshal(envelope["data"], &data); err != nil {
		t.Fatalf("data 不是对象: %v", err)
	}
	return data
}

// 验收：_meta 返回 6 实体 + 2 异常只读页，顺序与 admin.Entities / 异常页定义一致。
func TestMetaSections(t *testing.T) {
	data := decodeMeta(t)

	var entities []MetaEntity
	if err := json.Unmarshal(data["entities"], &entities); err != nil {
		t.Fatalf("entities 解析失败: %v", err)
	}
	if len(entities) != len(Entities) || len(entities) != 6 {
		t.Fatalf("entities = %d 段，期望 6（与 admin.Entities 同数）", len(entities))
	}
	for i, e := range entities {
		if e.Name != Entities[i].Name {
			t.Fatalf("entities[%d] = %q，期望 %q（顺序即导航顺序）", i, e.Name, Entities[i].Name)
		}
		if e.Label == "" || e.Label == e.Name {
			t.Fatalf("实体 %q 缺少中文标签", e.Name)
		}
		if e.Route != "/admin/v1/"+e.Name {
			t.Fatalf("实体 %q 的 route = %q", e.Name, e.Route)
		}
		if e.ReadOnly {
			t.Fatalf("实体 %q 不应是只读", e.Name)
		}
		if e.DefaultOrder != "desc" || e.DefaultSort == "" {
			t.Fatalf("实体 %q 默认排序不符: %q %q", e.Name, e.DefaultSort, e.DefaultOrder)
		}
		if len(e.Fields) == 0 {
			t.Fatalf("实体 %q 没有字段元数据", e.Name)
		}
	}

	var exceptions []MetaException
	if err := json.Unmarshal(data["exceptions"], &exceptions); err != nil {
		t.Fatalf("exceptions 解析失败: %v", err)
	}
	if len(exceptions) != 2 {
		t.Fatalf("exceptions = %d 段，期望 2", len(exceptions))
	}
	wantSlots := []string{"events", "intervals"}
	wantNames := []string{"exception-events", "exception-intervals"}
	wantLabels := []string{"异常事件", "异常区间"}
	for i, ex := range exceptions {
		if ex.Slot != wantSlots[i] || ex.Name != wantNames[i] || ex.Label != wantLabels[i] {
			t.Fatalf("exceptions[%d] = %+v，期望 slot/name/label = %s/%s/%s",
				i, ex, wantSlots[i], wantNames[i], wantLabels[i])
		}
		if ex.Route != "/admin/v1/exceptions/"+ex.Slot {
			t.Fatalf("异常页 %q 的 route = %q", ex.Slot, ex.Route)
		}
		if !ex.ReadOnly {
			t.Fatalf("异常页 %q 必须标记只读", ex.Slot)
		}
		if len(ex.Fields) == 0 {
			t.Fatalf("异常页 %q 没有字段元数据", ex.Slot)
		}
	}

	var pagination MetaPagination
	if err := json.Unmarshal(data["pagination"], &pagination); err != nil {
		t.Fatalf("pagination 解析失败: %v", err)
	}
	if pagination.Default != DefaultPageSize || pagination.Max != MaxPageSize {
		t.Fatalf("pagination = %+v，期望 default=%d max=%d", pagination, DefaultPageSize, MaxPageSize)
	}
	if len(pagination.Options) != 3 || pagination.Options[0] != 10 || pagination.Options[2] != 50 {
		t.Fatalf("分页档位 = %v，期望 [10 20 50]", pagination.Options)
	}
}

// 验收：每个字段的 JSON 键集合与前端 TS 类型逐字一致；operators/options 不出现 null。
func TestMetaFieldShape(t *testing.T) {
	data := decodeMeta(t)
	want := append([]string{}, metaFieldKeys...)
	sort.Strings(want)

	checkField := func(where, key string, raw json.RawMessage) {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("%s 字段 %q 不是对象: %v", where, key, err)
		}
		got := make([]string, 0, len(m))
		for k := range m {
			got = append(got, k)
		}
		sort.Strings(got)
		// relation 允许缺省（omitempty），其余键必须齐全。
		filtered := got[:0]
		for _, k := range got {
			if k != "relation" {
				filtered = append(filtered, k)
			}
		}
		if strings.Join(filtered, ",") != strings.Join(want, ",") {
			t.Fatalf("%s 字段 %q 的键 = %v，期望 %v", where, key, filtered, want)
		}
		for _, arr := range []string{"operators", "options"} {
			if string(m[arr]) == "null" {
				t.Fatalf("%s 字段 %q 的 %s 是 null，契约要求空数组", where, key, arr)
			}
		}
		if string(m["operatorLabels"]) == "null" {
			t.Fatalf("%s 字段 %q 的 operatorLabels 是 null，契约要求对象", where, key)
		}
	}

	var sections []json.RawMessage
	for _, name := range []string{"entities", "exceptions"} {
		var list []json.RawMessage
		if err := json.Unmarshal(data[name], &list); err != nil {
			t.Fatalf("%s 解析失败: %v", name, err)
		}
		for _, item := range list {
			var head struct {
				Name   string            `json:"name"`
				Fields []json.RawMessage `json:"fields"`
			}
			var raw map[string]json.RawMessage
			if err := json.Unmarshal(item, &raw); err != nil {
				t.Fatalf("%s 段解析失败: %v", name, err)
			}
			if err := json.Unmarshal(item, &head); err != nil {
				t.Fatalf("%s 段解析失败: %v", name, err)
			}
			// 每个段的键集合也是契约：实体与异常页各自的字段名固定。
			var wantKeys []string
			if name == "entities" {
				wantKeys = []string{"name", "label", "route", "readOnly", "defaultSort", "defaultOrder", "fields"}
			} else {
				wantKeys = []string{"slot", "name", "label", "route", "readOnly", "defaultSort", "defaultOrder", "fields"}
			}
			sort.Strings(wantKeys)
			got := make([]string, 0, len(raw))
			for k := range raw {
				got = append(got, k)
			}
			sort.Strings(got)
			if strings.Join(got, ",") != strings.Join(wantKeys, ",") {
				t.Fatalf("%s[%s] 的键 = %v，期望 %v", name, head.Name, got, wantKeys)
			}
			for _, f := range head.Fields {
				var key struct {
					Key string `json:"key"`
				}
				_ = json.Unmarshal(f, &key)
				checkField(name+"/"+head.Name, key.Key, f)
			}
			sections = append(sections, item)
		}
	}
	if len(sections) != 8 {
		t.Fatalf("元数据段数 = %d，期望 6 实体 + 2 异常页", len(sections))
	}
}

// 验收：枚举字段带中文标签与 Tone 闭集；关系字段带目标实体；period 不可筛选/不可排序。
func TestMetaEnumsRelationsAndPeriod(t *testing.T) {
	payload := BuildMeta()
	known := map[string]bool{}
	for _, e := range Entities {
		known[e.Name] = true
	}
	for _, page := range ExceptionPages() {
		known[page.Entity.Name] = true
	}
	var periodChecked bool

	check := func(where string, fields []MetaField) {
		for _, f := range fields {
			switch f.Type {
			case "enum":
				if len(f.Options) == 0 {
					t.Fatalf("%s 枚举字段 %q 没有取值", where, f.Key)
				}
				seen := map[string]bool{}
				for _, o := range f.Options {
					if o.Value == "" || o.Label == "" {
						t.Fatalf("%s 枚举字段 %q 的取值 %+v 缺少中文标签", where, f.Key, o)
					}
					if !toneClosedSet[o.Tone] {
						t.Fatalf("%s 枚举字段 %q 的 Tone = %q 不在闭集内", where, f.Key, o.Tone)
					}
					if seen[o.Value] {
						t.Fatalf("%s 枚举字段 %q 的取值 %q 重复", where, f.Key, o.Value)
					}
					seen[o.Value] = true
				}
			case "relation":
				if f.Relation == "" || !known[f.Relation] {
					t.Fatalf("%s 关系字段 %q 的目标实体 = %q，未知", where, f.Key, f.Relation)
				}
			}
			for _, op := range f.Operators {
				if f.OperatorLabels[op] == "" || f.OperatorLabels[op] == op {
					t.Fatalf("%s 字段 %q 的操作符 %q 缺少中文说明", where, f.Key, op)
				}
			}
		}
	}
	for _, e := range payload.Entities {
		check(e.Name, e.Fields)
	}
	for _, ex := range payload.Exceptions {
		check(ex.Slot, ex.Fields)
		if ex.Slot != "intervals" {
			continue
		}
		for _, f := range ex.Fields {
			if f.Key != "period" {
				continue
			}
			periodChecked = true
			if f.Filterable || f.Sortable {
				t.Fatalf("period 必须不可筛选且不可排序（tstzrange 语义歧义）: filterable=%v sortable=%v",
					f.Filterable, f.Sortable)
			}
			if f.FilterType != "none" || len(f.Operators) != 0 {
				t.Fatalf("period 的 filterType/operators 应为 none/空: %q %v", f.FilterType, f.Operators)
			}
		}
	}
	if !periodChecked {
		t.Fatal("exceptions[intervals] 缺少 period 字段")
	}
}

// _meta 是字面量路由，不能被 {entity} / {entity}/{id} 抢走；POST 仍落回实体白名单（404）。
func TestMetaRoutePrecedence(t *testing.T) {
	handler := New(nil).Handler()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/admin/v1/_meta", nil))
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "unknown_entity") {
		t.Fatalf("POST /admin/v1/_meta = %d %s，期望 404 unknown_entity", rec.Code, rec.Body.String())
	}

	// GET /admin/v1/exceptions 只有一段，走 {entity} 白名单 → 404；不影响 /exceptions/{slot}。
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/v1/exceptions", nil))
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "unknown_entity") {
		t.Fatalf("GET /admin/v1/exceptions = %d %s，期望 404 unknown_entity", rec.Code, rec.Body.String())
	}
}

// 实体中文标签集中在此，前端零硬编码；标签集合必须覆盖 6 实体 + 2 异常页。
func TestEntityLabelsCoverAllSections(t *testing.T) {
	want := map[string]string{
		"data-domains":           "数据域",
		"device-types":           "终端类型",
		"thing-model-properties": "物模型属性",
		"terminals":              "终端",
		"quality-rules":          "质量规则",
		"settle-configs":         "结算窗口",
		"exception-events":       "异常事件",
		"exception-intervals":    "异常区间",
	}
	for name, label := range want {
		if got := EntityLabel(name); got != label {
			t.Fatalf("EntityLabel(%q) = %q，期望 %q", name, got, label)
		}
	}
	if got := EntityLabel("nope"); got != "nope" {
		t.Fatalf("未知实体应原样返回名字，实际 %q", got)
	}
}
