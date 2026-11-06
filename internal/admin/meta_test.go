package admin

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/RenJieWeia/iot-admin/migrations"
)

// metaFieldKeys 是 MetaField 的 JSON 契约全字段（前端 TS 类型的唯一依据）。
var metaFieldKeys = []string{
	"key", "label", "type", "visible", "sortable", "filterable", "filterType",
	"operators", "operatorLabels", "optionsSource", "options", "advancedOnly", "widthStrategy",
	"editable", "required",
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

// 验收：_meta 返回 5 个导航实体 + 1 个 subresource（thing-model-properties）+ 1 个异常只读页。
func TestMetaSections(t *testing.T) {
	data := decodeMeta(t)

	var entities []MetaEntity
	if err := json.Unmarshal(data["entities"], &entities); err != nil {
		t.Fatalf("entities 解析失败: %v", err)
	}
	// 导航实体 = Entities 里没有 Hidden 的那些（顺序保持一致）。
	navigable := make([]Entity, 0, len(Entities))
	for _, e := range Entities {
		if !e.Hidden {
			navigable = append(navigable, e)
		}
	}
	if len(entities) != len(navigable) || len(entities) != 5 {
		t.Fatalf("entities = %d 段，期望 5（可导航实体；可 CRUD 仍是 %d）", len(entities), len(Entities))
	}
	for i, e := range entities {
		if e.Name != navigable[i].Name {
			t.Fatalf("entities[%d] = %q，期望 %q（顺序即导航顺序）", i, e.Name, navigable[i].Name)
		}
		if e.Title == "" || e.Title == e.Name {
			t.Fatalf("实体 %q 缺少中文标题（键名 title）", e.Name)
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

	// 不可导航实体不下发导航，但字段元数据必须还在 subresources 里（详情页要用）。
	var subresources []MetaEntity
	if err := json.Unmarshal(data["subresources"], &subresources); err != nil {
		t.Fatalf("subresources 解析失败: %v", err)
	}
	if len(subresources) != 1 || subresources[0].Name != "thing-model-properties" {
		t.Fatalf("subresources = %+v，期望只有 thing-model-properties", subresources)
	}
	if subresources[0].Title != "终端属性" {
		t.Fatalf("subresources 标题 = %q，期望 终端属性", subresources[0].Title)
	}
	if len(subresources[0].Fields) == 0 {
		t.Fatal("subresources 里的 thing-model-properties 没有字段元数据")
	}
	if subresources[0].Route != "/admin/v1/thing-model-properties" {
		t.Fatalf("subresources 的 route = %q", subresources[0].Route)
	}

	var exceptions []MetaException
	if err := json.Unmarshal(data["exceptions"], &exceptions); err != nil {
		t.Fatalf("exceptions 解析失败: %v", err)
	}
	if len(exceptions) != 1 {
		t.Fatalf("exceptions = %d 段，期望 1（异常区间已下线）", len(exceptions))
	}
	wantSlots := []string{"events"}
	wantNames := []string{"exception-events"}
	wantLabels := []string{"异常事件"}
	for i, ex := range exceptions {
		if ex.Slot != wantSlots[i] || ex.Name != wantNames[i] || ex.Title != wantLabels[i] {
			t.Fatalf("exceptions[%d] = %+v，期望 slot/name/title = %s/%s/%s",
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

	gotDataKeys := make([]string, 0, len(data))
	for k := range data {
		gotDataKeys = append(gotDataKeys, k)
	}
	sort.Strings(gotDataKeys)
	wantDataKeys := []string{"booleans", "entities", "enums", "exceptions", "pagination", "readonly", "subresources"}
	if strings.Join(gotDataKeys, ",") != strings.Join(wantDataKeys, ",") {
		t.Fatalf("data 顶层键 = %v，期望 %v", gotDataKeys, wantDataKeys)
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
			if k != "relation" && k != "ref" {
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
	// subresources 与 entities 同形，一并按同一套契约检查。
	for _, name := range []string{"entities", "subresources", "exceptions", "readonly"} {
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
			if name == "entities" || name == "subresources" {
				wantKeys = []string{"name", "title", "route", "readOnly", "defaultSort", "defaultOrder", "fields"}
			} else {
				// exceptions 与 readonly 同形（exceptions 是 readonly 的特例）；
				// readonly 额外带 pagination（offset|cursor）。
				wantKeys = []string{"slot", "name", "title", "route", "readOnly", "defaultSort", "defaultOrder", "fields"}
				if name == "readonly" {
					wantKeys = []string{"slot", "name", "title", "route", "readOnly", "defaultSort",
						"defaultOrder", "pagination", "fields"}
				}
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
	if len(sections) != 10 {
		t.Fatalf("元数据段数 = %d，期望 5 导航实体 + 1 子资源 + 1 异常页 + 3 只读源", len(sections))
	}
}

// 验收：枚举字段带中文标签与 Tone 闭集；关系字段带目标实体。
func TestMetaEnumsRelations(t *testing.T) {
	payload := BuildMeta()
	known := map[string]bool{}
	for _, e := range Entities {
		known[e.Name] = true
	}
	for _, page := range ExceptionPages() {
		known[page.Entity.Name] = true
	}
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
				if f.Ref == nil {
					t.Fatalf("%s 关系字段 %q 缺少 ref 契约", where, f.Key)
				}
				if f.Ref.Entity != f.Relation || f.Ref.ValueKey != "id" {
					t.Fatalf("%s 关系字段 %q 的 ref 不符: %+v", where, f.Key, *f.Ref)
				}
				if f.Ref.LabelKey != "name" && f.Ref.LabelKey != "code" && f.Ref.LabelKey != "id" {
					t.Fatalf("%s 关系字段 %q 的 ref.labelKey = %q", where, f.Key, f.Ref.LabelKey)
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

// 实体中文标签集中在此，前端零硬编码；标签集合必须覆盖 6 实体 + 1 异常页。
func TestEntityLabelsCoverAllSections(t *testing.T) {
	want := map[string]string{
		"data-domains":           "数据域",
		"device-types":           "终端类型",
		"thing-model-properties": "终端属性",
		"terminals":              "终端",
		"quality-rules":          "质量规则",
		"settle-configs":         "结算窗口",
		"exception-events":       "异常事件",
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

// 枚举与布尔字典是前端筛选/状态文案的直接来源（task-004 §4：enums / booleans）。
func TestMetaDictionaries(t *testing.T) {
	data := decodeMeta(t)

	var enums map[string]map[string]string
	if err := json.Unmarshal(data["enums"], &enums); err != nil {
		t.Fatalf("enums 解析失败: %v", err)
	}
	wantEnums := map[string][]Option{"value_type": valueTypeOptions, "rule_type": ruleTypeOptions}
	if len(enums) != len(wantEnums) {
		t.Fatalf("enums 字典 = %v，期望只有 %d 张", enums, len(wantEnums))
	}
	for key, options := range wantEnums {
		dict, ok := enums[key]
		if !ok {
			t.Fatalf("enums 缺少 %q 字典", key)
		}
		if len(dict) != len(options) {
			t.Fatalf("enums[%q] 取值数 = %d，期望 %d", key, len(dict), len(options))
		}
		for _, o := range options {
			if got := dict[o.Value]; got != o.Label {
				t.Fatalf("enums[%q][%q] = %q，期望 %q", key, o.Value, got, o.Label)
			}
		}
	}

	var booleans map[string]string
	if err := json.Unmarshal(data["booleans"], &booleans); err != nil {
		t.Fatalf("booleans 解析失败: %v", err)
	}
	if len(booleans) != 2 || booleans["true"] != "是" || booleans["false"] != "否" {
		t.Fatalf("booleans 字典 = %v，期望 true=是 false=否", booleans)
	}
}

// editable 与 Entity.Cols（唯一可写白名单）逐实体对齐：不在 fields.go 里另存一份。
func TestEditableMatchesEntityCols(t *testing.T) {
	payload := BuildMeta()
	// 可编辑性按实体名对齐：导航实体在 entities、不可导航的在 subresources，两段合起来覆盖全部 6 个。
	all := make(map[string]MetaEntity, len(payload.Entities)+len(payload.Subresources))
	for _, meta := range payload.Entities {
		all[meta.Name] = meta
	}
	for _, meta := range payload.Subresources {
		all[meta.Name] = meta
	}
	if len(all) != len(Entities) {
		t.Fatalf("_meta 覆盖实体 = %d，期望 %d（entities+subresources）", len(all), len(Entities))
	}
	for _, e := range Entities {
		meta, ok := all[e.Name]
		if !ok {
			t.Fatalf("%s 在 _meta 的 entities/subresources 里都不存在", e.Name)
		}
		want := make(map[string]bool, len(e.Cols))
		for _, c := range e.Cols {
			want[c] = true
		}
		got := map[string]bool{}
		for _, f := range meta.Fields {
			if f.Editable {
				got[f.Key] = true
			}
			if f.Editable != want[f.Key] {
				t.Fatalf("%s.%s 的 editable = %v，期望 %v（Entity.Cols 白名单）",
					e.Name, f.Key, f.Editable, want[f.Key])
			}
		}
		if len(got) != len(want) {
			t.Fatalf("%s 的可编辑列 = %v，期望 %v", e.Name, got, want)
		}
	}

	// 异常只读页没有写路径，editable 恒为 false。
	for _, ex := range payload.Exceptions {
		for _, f := range ex.Fields {
			if f.Editable {
				t.Fatalf("异常只读页 %s.%s 不应可编辑", ex.Slot, f.Key)
			}
		}
	}
}

// 两条锚点用例（任务书点名）：terminals.id 可编辑且必填；data-domains.id 既不可写也不必填。
func TestEditableRequiredAnchors(t *testing.T) {
	payload := BuildMeta()
	find := func(entity, key string) MetaField {
		t.Helper()
		for _, e := range payload.Entities {
			if e.Name != entity {
				continue
			}
			for _, f := range e.Fields {
				if f.Key == key {
					return f
				}
			}
		}
		t.Fatalf("找不到字段 %s.%s", entity, key)
		return MetaField{}
	}

	if f := find("terminals", "id"); !f.Editable || !f.Required {
		t.Fatalf("terminals.id editable=%v required=%v，期望 true/true", f.Editable, f.Required)
	}
	if f := find("data-domains", "id"); f.Editable || f.Required {
		t.Fatalf("data-domains.id editable=%v required=%v，期望 false/false", f.Editable, f.Required)
	}
}

// required 与 DDL 直接对照：解析 migrations/0001 的 not null / primary key / default，
// 逐列比对，避免手工抄写与迁移漂移。
func TestRequiredMatchesDDL(t *testing.T) {
	ddl := string(mustReadMigrations(t, "0001_datasource.sql"))
	table := ddlColumns(t, ddl)

	payload := BuildMeta()
	// 按实体名对齐：导航实体在 entities、不可导航的在 subresources，两段合起来覆盖全部实体。
	all := make(map[string]MetaEntity, len(payload.Entities)+len(payload.Subresources))
	for _, meta := range payload.Entities {
		all[meta.Name] = meta
	}
	for _, meta := range payload.Subresources {
		all[meta.Name] = meta
	}
	checked := 0
	for _, e := range Entities {
		meta, ok := all[e.Name]
		if !ok {
			t.Fatalf("%s 在 _meta 的 entities/subresources 里都不存在", e.Name)
		}
		cols, ok := table[e.Table]
		if !ok {
			t.Fatalf("DDL 里找不到表 %q", e.Table)
		}
		for _, f := range meta.Fields {
			info, ok := cols[f.Key]
			if !ok {
				t.Fatalf("DDL 里找不到列 %s.%s", e.Table, f.Key)
			}
			if f.Required != info.notNull {
				t.Fatalf("%s.%s required = %v，DDL 为 not null=%v default=%v",
					e.Name, f.Key, f.Required, info.notNull, info.hasDefault)
			}
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("没有比对任何列")
	}
}

// _meta 不依赖数据库：nil pool 也必须 200。
func TestMetaWorksWithoutPool(t *testing.T) {
	rec := httptest.NewRecorder()
	New(nil).Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/v1/_meta", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("nil pool 下 _meta = %d，期望 200: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			Entities []struct {
				Fields []struct {
					Key      string `json:"key"`
					Editable bool   `json:"editable"`
					Required bool   `json:"required"`
				} `json:"fields"`
			} `json:"entities"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	seenEditable, seenRequired := false, false
	for _, e := range resp.Data.Entities {
		for _, f := range e.Fields {
			seenEditable = seenEditable || f.Editable
			seenRequired = seenRequired || f.Required
		}
	}
	if !seenEditable || !seenRequired {
		t.Fatalf("响应里没有 editable/required 取值: editable=%v required=%v", seenEditable, seenRequired)
	}
}

// ddlColumn 是一列在 DDL 里的写入约束。
type ddlColumn struct {
	notNull    bool // not null 或 primary key，且没有 default
	hasDefault bool
}

// ddlColumns 解析 create table 块，返回 表名 → 列名 → 约束。
// 只认 0001 里出现的行内约束（not null / primary key / default），不追求完整 SQL 解析。
func ddlColumns(t *testing.T, ddl string) map[string]map[string]ddlColumn {
	t.Helper()
	out := map[string]map[string]ddlColumn{}
	tableRe := regexp.MustCompile(`(?ms)create table if not exists (\w+) \((.*?)\n\);`)
	columnRe := regexp.MustCompile(`(?m)^\s{4}([a-z_]+)\s+(.+?),?$`)
	skip := map[string]bool{"unique": true, "constraint": true, "primary": true, "check": true, "foreign": true, "exclude": true}

	for _, m := range tableRe.FindAllStringSubmatch(ddl, -1) {
		name, body := m[1], m[2]
		cols := map[string]ddlColumn{}
		for _, c := range columnRe.FindAllStringSubmatch(body, -1) {
			col, rest := c[1], strings.ToLower(c[2])
			if skip[col] {
				continue
			}
			hasDefault := strings.Contains(rest, "default")
			notNull := (strings.Contains(rest, "not null") || strings.Contains(rest, "primary key")) && !hasDefault
			cols[col] = ddlColumn{notNull: notNull, hasDefault: hasDefault}
		}
		if len(cols) == 0 {
			t.Fatalf("表 %q 没解析出列", name)
		}
		out[name] = cols
	}
	if len(out) == 0 {
		t.Fatal("没有解析到任何 create table 块")
	}
	return out
}

func mustReadMigrations(t *testing.T, name string) []byte {
	t.Helper()
	body, err := fs.ReadFile(migrations.FS, name)
	if err != nil {
		t.Fatalf("读取迁移 %s: %v", name, err)
	}
	return body
}
