package ui_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RenJieWeia/iot-admin/internal/admin"
	"github.com/RenJieWeia/iot-admin/internal/ui"
)

// fakeSource 是内存里的假数据源：所有查询都由它按 QueryState 分页，页面无需数据库。
type fakeSource struct {
	rows map[string][]map[string]any
	err  error
}

func (f *fakeSource) List(_ context.Context, entity string, qs admin.QueryState) (admin.ListResult, error) {
	if f.err != nil {
		return admin.ListResult{}, f.err
	}
	all := f.rows[entity]
	total := int64(len(all))
	start := (qs.Page - 1) * qs.PageSize
	if start > len(all) {
		start = len(all)
	}
	end := start + qs.PageSize
	if end > len(all) {
		end = len(all)
	}
	page := make([]map[string]any, 0, end-start)
	page = append(page, all[start:end]...)
	return admin.ListResult{
		Data: page,
		Meta: admin.ListMeta{Total: total, Page: qs.Page, PageSize: qs.PageSize, Sort: qs.Sort, Order: qs.Order},
	}, nil
}

func (f *fakeSource) Get(_ context.Context, entity, id string) (map[string]any, error) {
	for _, row := range f.rows[entity] {
		if row["id"] == id {
			return row, nil
		}
	}
	return nil, errors.New("not found")
}

func newFakeSource(total int) *fakeSource {
	rows := make([]map[string]any, 0, total)
	for i := 1; i <= total; i++ {
		rows = append(rows, map[string]any{
			"id": "T" + itoa(i), "type_id": int64(3), "gateway_id": "GW1",
			"name": "一号终端", "enabled": i%2 == 1,
		})
	}
	return &fakeSource{rows: map[string][]map[string]any{
		"terminals":    rows,
		"device-types": {{"id": int64(3), "code": "meter", "name": "电表", "domain_id": int64(1)}},
	}}
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

func get(t *testing.T, h http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// 验收 §9：列表页含 <table>、筛选标签、分页器与总数。
func TestListPageRendersTableTagsAndPagination(t *testing.T) {
	src := newFakeSource(12)
	h := ui.New(ui.Deps{Source: src}).Handler()

	rec := get(t, h, "/ui/terminals?filter.enabled=false&q=GW&pageSize=10")
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"<table class=\"grid\">", `scope="col"`, "第 1–10 条，共 12 条",
		"上一页", "下一页", "已生效筛选条件", "关键词", "停用",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("列表页缺少 %q", want)
		}
	}
	if !strings.Contains(body, "电表 (3)") {
		t.Fatal("关系列应回填名称 + id")
	}
}

// 验收 §9：空 / 无匹配 / 失败三态文案互不相同。
func TestThreeStatesAreDistinct(t *testing.T) {
	empty := ui.New(ui.Deps{Source: newFakeSource(0)}).Handler()
	emptyBody := get(t, empty, "/ui/terminals").Body.String()
	if !strings.Contains(emptyBody, "还没有终端") {
		t.Fatalf("空态文案不符: %s", head(emptyBody))
	}

	nomatch := ui.New(ui.Deps{Source: newFakeSource(0)}).Handler()
	nomatchBody := get(t, nomatch, "/ui/terminals?q=GW").Body.String()
	if !strings.Contains(nomatchBody, "没有匹配的记录") {
		t.Fatalf("无匹配文案不符: %s", head(nomatchBody))
	}

	failing := ui.New(ui.Deps{Source: &fakeSource{err: errors.New("db offline")}}).Handler()
	failBody := get(t, failing, "/ui/terminals").Body.String()
	if !strings.Contains(failBody, "加载失败") || !strings.Contains(failBody, "db offline") {
		t.Fatalf("失败态应含原因: %s", head(failBody))
	}

	if emptyBody == nomatchBody || nomatchBody == failBody {
		t.Fatal("三态文案不得复用")
	}
}

// 验收 §9：?drawer=new 渲染抽屉壳（role=dialog/标题/取消/保存）。
func TestDrawerNewRendersDialogShell(t *testing.T) {
	h := ui.New(ui.Deps{Source: newFakeSource(3)}).Handler()
	body := get(t, h, "/ui/terminals?drawer=new").Body.String()
	for _, want := range []string{
		`role="dialog"`, `aria-modal="true"`, "新建终端", "取消", "保存", `id="drawer-title"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("抽屉缺少 %q", want)
		}
	}
}

// 验收 §9：?drawer=<id> 渲染该行数据。
func TestDrawerEditRendersRowValues(t *testing.T) {
	h := ui.New(ui.Deps{Source: newFakeSource(3)}).Handler()
	body := get(t, h, "/ui/terminals?drawer=T2").Body.String()
	if !strings.Contains(body, "编辑终端") {
		t.Fatal("抽屉标题应为编辑终端")
	}
	if !strings.Contains(body, `value="T2"`) {
		t.Fatalf("抽屉未渲染该行 id: %s", head(body))
	}
	if !strings.Contains(body, `value="GW1"`) {
		t.Fatal("抽屉未渲染该行 gateway_id")
	}
}

// 未知实体 404、非法查询 400（不静默忽略）。
func TestUnknownEntityAndInvalidQuery(t *testing.T) {
	h := ui.New(ui.Deps{Source: newFakeSource(1)}).Handler()
	if rec := get(t, h, "/ui/nope"); rec.Code != http.StatusNotFound {
		t.Fatalf("未知实体状态码 = %d，期望 404", rec.Code)
	}
	rec := get(t, h, "/ui/terminals?filter.nope=x")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法查询状态码 = %d，期望 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "查询参数无效") {
		t.Fatal("400 页面应说明原因与恢复路径")
	}
}

// 只读 Source（不实现 mutator）时不渲染写操作入口。
func TestReadOnlySourceHidesWriteActions(t *testing.T) {
	h := ui.New(ui.Deps{Source: newFakeSource(2)}).Handler()
	body := get(t, h, "/ui/terminals").Body.String()
	if strings.Contains(body, "btn-new") || strings.Contains(body, "data-delete-url") {
		t.Fatal("只读数据源不应出现新建/删除入口")
	}
}

// 排序表头只在可排序列上出现入口，且带方向标记。
func TestSortableHeadersOnly(t *testing.T) {
	h := ui.New(ui.Deps{Source: newFakeSource(2)}).Handler()
	body := get(t, h, "/ui/terminals?sort=name&order=asc").Body.String()
	if !strings.Contains(body, "名称 ↑") {
		t.Fatalf("当前排序列应显示方向: %s", head(body))
	}
	if !strings.Contains(body, "sort=name") || !strings.Contains(body, "order=desc") {
		t.Fatalf("同列再次点击应给出反方向链接: %s", head(body))
	}
}

func head(s string) string {
	if len(s) > 400 {
		return s[:400]
	}
	return s
}
