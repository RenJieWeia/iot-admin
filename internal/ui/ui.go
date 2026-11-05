// Package ui 是管理后台的单二进制 SSR 界面：6 个数据源实体的增删改查 + 异常表只读浏览。
// 查询（关键词/筛选/排序/分页）全部由服务端执行，URL 是唯一查询事实源。
package ui

import (
	"context"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/RenJieWeia/iot-admin/internal/admin"
)

// Deps 是装配依赖；测试只给 Source，不碰数据库。
type Deps struct {
	Source Source
	Log    *slog.Logger
	Now    func() time.Time
}

// Handler 是管理后台的 HTTP 入口。
type Handler struct {
	source Source
	log    *slog.Logger
	now    func() time.Time
	tpl    *template.Template
	static http.Handler
}

// New 构造 Handler；模板为 embed 资源，加载失败直接 panic（启动即暴露）。
func New(d Deps) *Handler {
	tpl, err := loadTemplates()
	if err != nil {
		panic("ui: 模板加载失败: " + err.Error())
	}
	static, err := staticHandler()
	if err != nil {
		panic("ui: 静态资源加载失败: " + err.Error())
	}
	h := &Handler{source: d.Source, log: d.Log, now: d.Now, tpl: tpl, static: static}
	if h.source == nil {
		h.source = &PoolSource{}
	}
	if h.log == nil {
		h.log = slog.Default()
	}
	if h.now == nil {
		h.now = time.Now
	}
	return h
}

// NewWithPool 是 cmd/server 的便捷装配。
func NewWithPool(pool *pgxpool.Pool, log *slog.Logger, now func() time.Time) *Handler {
	return New(Deps{Source: NewPoolSource(pool), Log: log, Now: now})
}

// Handler 注册 UI 路由。
func (h *Handler) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ui/{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ui/data-domains", http.StatusFound)
	})
	mux.Handle("GET /ui/static/", h.static)
	mux.HandleFunc("GET /ui/{entity}", h.list)
	return mux
}

// ---------- 实体与导航 ----------

var entityLabels = map[string]string{
	"data-domains":           "数据域",
	"device-types":           "终端类型",
	"thing-model-properties": "物模型属性",
	"terminals":              "终端",
	"quality-rules":          "质量规则",
	"settle-configs":         "结算窗口",
}

// entityByName 只在 6 个数据源实体里解析，未知实体一律 404。
func entityByName(name string) (admin.Entity, bool) {
	for _, e := range admin.Entities {
		if e.Name == name {
			return e, true
		}
	}
	return admin.Entity{}, false
}

type navItem struct {
	Label  string
	Href   string
	Active bool
}

func navItems(active string) []navItem {
	out := make([]navItem, 0, len(admin.Entities)+2)
	for _, e := range admin.Entities {
		out = append(out, navItem{Label: entityLabels[e.Name], Href: "/ui/" + e.Name, Active: e.Name == active})
	}
	return out
}

// ---------- 视图模型 ----------

type cell struct {
	Text string
	Full string
	Kind string // text | tag | mono | muted | json
	Tone string // neutral | positive | negative | warning
}

type row struct {
	ID        string
	Cells     []cell
	EditURL   string
	DeleteURL string
}

type filterTag struct {
	Label     string
	OpLabel   string
	Value     string
	RemoveURL string
}

type filterInput struct {
	F       admin.FieldMeta
	Name    string
	Value   string
	Values  []string
	Min     string
	Max     string
	Options []admin.Option
}

type formField struct {
	F       admin.FieldMeta
	Value   string
	Options []admin.Option
}

type drawerData struct {
	Mode      string // new | edit
	ID        string
	Title     string
	Entity    string
	Action    string
	CloseURL  string
	Forms     []formField
	Error     string
	CanDelete bool
	DeleteURL string
}

type flashMsg struct {
	Kind string // ok | error
	Text string
}

type pageData struct {
	Title       string
	Entity      string
	EntityLabel string
	Nav         []navItem

	Q     string
	Sort  string
	Order string

	Fields   []admin.FieldMeta
	Rows     []row
	Meta     admin.ListMeta
	Tags     []filterTag
	Quick    []filterInput
	Advanced []filterInput

	PageSizeOptions []int
	RangeText       string
	PrevURL         string
	NextURL         string
	HasPrev         bool
	HasNext         bool
	OutOfRange      bool
	SortURLs        map[string]string
	BaseURL         string
	PageSizeURLs    map[int]string

	Drawer *drawerData
	Flash  *flashMsg

	State          string // ready | empty | nomatch | error
	StateTitle     string
	StateText      string
	StateAction    string
	StateActionURL string

	CanWrite bool
	Now      string
}

// ---------- 列表 ----------

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("entity")
	e, ok := entityByName(name)
	if !ok {
		h.renderError(w, r, http.StatusNotFound, "页面不存在",
			"地址里的实体名不是已知的数据源，请从左侧导航重新进入。")
		return
	}
	qs, err := admin.ParseQueryState(e, r.URL.Query())
	if err != nil {
		h.renderError(w, r, http.StatusBadRequest, "查询参数无效",
			err.Error()+"。请清除筛选条件或返回默认列表后重试。")
		return
	}
	page := h.buildPage(r, e, qs)
	if partialRequested(r) {
		h.render(w, http.StatusOK, "listRegion", page)
		return
	}
	h.render(w, http.StatusOK, "list", page)
}

func (h *Handler) buildPage(r *http.Request, e admin.Entity, qs admin.QueryState) *pageData {
	raw := r.URL.Query()
	_, writable := h.source.(mutator)
	page := &pageData{
		Title:       entityLabels[e.Name],
		Entity:      e.Name,
		EntityLabel: entityLabels[e.Name],
		Nav:         navItems(e.Name),
		Q:           qs.Q,
		Sort:        qs.Sort,
		Order:       qs.Order,
		Meta:        admin.ListMeta{Page: qs.Page, PageSize: qs.PageSize, Sort: qs.Sort, Order: qs.Order},
		CanWrite:    writable,
		Now:         h.now().Format("2006-01-02 15:04:05 -07:00"),
		BaseURL:     "/ui/" + e.Name,
	}
	page.Fields = admin.VisibleFields(e.Name)

	rel := h.relationLabels(r.Context(), e.Name)
	page.Quick = h.filterInputs(admin.QuickFilters(e.Name), raw, rel)
	page.Advanced = h.filterInputs(admin.AdvancedFilters(e.Name), raw, rel)

	res, err := h.source.List(r.Context(), e.Name, qs)
	if err != nil {
		var qerr *admin.QueryError
		if asQueryError(err, &qerr) {
			page.State = "error"
			page.StateTitle = "查询条件无效"
			page.StateText = qerr.Error() + "。请清除筛选条件后重试。"
			page.StateAction = "返回默认列表"
			page.StateActionURL = "/ui/" + e.Name
			return page
		}
		h.log.Error("列表查询失败", "entity", e.Name, "err", err)
		page.State = "error"
		page.StateTitle = "加载失败"
		page.StateText = "列表加载失败：" + err.Error() + "。数据未丢失，可稍后重试。"
		page.StateAction = "重试"
		page.StateActionURL = withQuery("/ui/"+e.Name, raw)
		return page
	}
	page.Meta = res.Meta
	page.Rows = h.buildRows(e.Name, page.Fields, res.Data, rel)
	page.Tags = buildTags(e.Name, qs, raw)
	page.SortURLs = sortURLs("/ui/"+e.Name, e.Name, qs, raw)
	page.PageSizeURLs = pageSizeURLs("/ui/"+e.Name, raw)
	page.PageSizeOptions = []int{10, 20, 50}

	total := int(res.Meta.Total)
	if total == 0 {
		page.RangeText = "共 0 条"
	} else {
		start := (res.Meta.Page-1)*res.Meta.PageSize + 1
		end := start + len(res.Data) - 1
		if len(res.Data) == 0 {
			page.RangeText = fmt.Sprintf("第 %d 页无数据，共 %d 条", res.Meta.Page, total)
		} else {
			page.RangeText = fmt.Sprintf("第 %d–%d 条，共 %d 条", start, end, total)
		}
	}
	page.HasPrev = res.Meta.Page > 1 && total > 0
	page.HasNext = int64(res.Meta.Page)*int64(res.Meta.PageSize) < res.Meta.Total
	page.PrevURL = withPage("/ui/"+e.Name, raw, res.Meta.Page-1)
	page.NextURL = withPage("/ui/"+e.Name, raw, res.Meta.Page+1)
	page.OutOfRange = total > 0 && len(res.Data) == 0

	switch {
	case total == 0 && (qs.Q != "" || len(qs.Filters) > 0):
		page.State = "nomatch"
		page.StateTitle = "没有匹配的记录"
		page.StateText = "当前筛选条件排除了全部结果，换个条件或清除筛选再看。"
		page.StateAction = "清除筛选"
		page.StateActionURL = "/ui/" + e.Name
	case total == 0:
		page.State = "empty"
		page.StateTitle = "还没有" + entityLabels[e.Name]
		page.StateText = "该实体下暂无记录，可先用管理接口或右上角「新建」录入第一条。"
	default:
		page.State = "ready"
	}

	if raw.Get("drawer") != "" {
		page.Drawer = h.buildDrawer(r, e, raw.Get("drawer"), rel)
	}
	page.Flash = flashFrom(raw)
	return page
}

// buildRows 按字段元数据把行渲染成单元格（中文枚举/状态标签/关系名称/绝对时间）。
func (h *Handler) buildRows(entity string, fields []admin.FieldMeta, data []map[string]any, rel map[string]map[string]string) []row {
	out := make([]row, 0, len(data))
	for _, item := range data {
		rw := row{ID: fmt.Sprint(item["id"])}
		for _, f := range fields {
			rw.Cells = append(rw.Cells, formatCell(entity, f, item[f.Key], rel[f.Key]))
		}
		if _, ok := item["id"]; ok {
			rw.EditURL = "?drawer=" + url.QueryEscape(rw.ID)
			rw.DeleteURL = "/ui/" + entity + "/" + url.PathEscape(rw.ID) + "/delete"
		}
		out = append(out, rw)
	}
	return out
}

func formatCell(entity string, f admin.FieldMeta, v any, rel map[string]string) cell {
	if v == nil {
		return cell{Text: "—", Kind: "muted", Tone: "neutral"}
	}
	switch f.Type {
	case "boolean":
		s := fmt.Sprint(v)
		if b, ok := v.(bool); ok {
			s = strconv.FormatBool(b)
		}
		label := admin.OptionLabel(entity, f.Key, s)
		return cell{Text: label, Full: label, Kind: "tag", Tone: admin.OptionTone(entity, f.Key, s)}
	case "enum":
		label := admin.OptionLabel(entity, f.Key, fmt.Sprint(v))
		return cell{Text: label, Full: label, Kind: "tag", Tone: "neutral"}
	case "relation":
		id := fmt.Sprint(v)
		if name, ok := rel[id]; ok && name != "" {
			text := name + " (" + id + ")"
			return cell{Text: text, Full: text, Kind: "text"}
		}
		return cell{Text: id, Full: id, Kind: "mono"}
	case "number":
		text := formatNumber(v)
		return cell{Text: text, Full: text, Kind: "mono"}
	case "datetime":
		text := formatTime(fmt.Sprint(v))
		return cell{Text: text, Full: text, Kind: "mono"}
	case "json":
		text := compactJSON(fmt.Sprint(v))
		return cell{Text: text, Full: text, Kind: "json"}
	case "id":
		id := fmt.Sprint(v)
		return cell{Text: id, Full: id, Kind: "mono"}
	default:
		text := fmt.Sprint(v)
		return cell{Text: text, Full: text, Kind: "text"}
	}
}

func formatNumber(v any) string {
	switch n := v.(type) {
	case float64:
		return strconv.FormatFloat(n, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(n), 'f', -1, 64)
	default:
		return fmt.Sprint(v)
	}
}

var shanghai = time.FixedZone("+08:00", 8*3600)

// formatTime 一律显示绝对时间并带偏移，不用相对时间。
func formatTime(raw string) string {
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return raw
	}
	return t.In(shanghai).Format("2006-01-02 15:04:05 -07:00")
}

func compactJSON(raw string) string {
	s := strings.Join(strings.Fields(raw), " ")
	if len(s) > 60 {
		return s[:60] + "…"
	}
	return s
}

// relationLabels 为关系列做 id→名称 回填：目标实体取前 50 行，超出的退化为只显示 id。
func (h *Handler) relationLabels(ctx context.Context, entity string) map[string]map[string]string {
	all, ok := admin.FieldMetas(entity)
	if !ok {
		return nil
	}
	out := map[string]map[string]string{}
	for _, f := range all {
		target, ok := admin.RelationTarget(entity, f.Key)
		if !ok {
			continue
		}
		if _, ok := entityByName(target); !ok {
			continue
		}
		res, err := h.source.List(ctx, target, admin.QueryState{
			Page: 1, PageSize: admin.MaxPageSize, Sort: "id", Order: "asc",
		})
		if err != nil {
			h.log.Warn("关系选项加载失败", "entity", target, "err", err)
			continue
		}
		m := make(map[string]string, len(res.Data))
		for _, row := range res.Data {
			id := fmt.Sprint(row["id"])
			label := id
			if v, ok := row["name"]; ok && fmt.Sprint(v) != "" {
				label = fmt.Sprint(v)
			} else if v, ok := row["code"]; ok && fmt.Sprint(v) != "" {
				label = fmt.Sprint(v)
			}
			m[id] = label
		}
		out[f.Key] = m
	}
	return out
}

func (h *Handler) filterInputs(fields []admin.FieldMeta, raw url.Values, rel map[string]map[string]string) []filterInput {
	out := make([]filterInput, 0, len(fields))
	for _, f := range fields {
		in := filterInput{F: f, Name: "filter." + f.Key}
		in.Value = raw.Get(in.Name)
		in.Values = raw[in.Name]
		in.Min = raw.Get(in.Name + ".gte")
		in.Max = raw.Get(in.Name + ".lte")
		switch f.FilterType {
		case "boolean", "multiSelect":
			in.Options = f.Options
		case "relation", "remoteSelect":
			in.Options = relationOptions(rel[f.Key])
		}
		out = append(out, in)
	}
	return out
}

func relationOptions(labels map[string]string) []admin.Option {
	if len(labels) == 0 {
		return nil
	}
	ids := make([]string, 0, len(labels))
	for id := range labels {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		ni, errI := strconv.ParseInt(ids[i], 10, 64)
		nj, errJ := strconv.ParseInt(ids[j], 10, 64)
		if errI == nil && errJ == nil {
			return ni < nj
		}
		return ids[i] < ids[j]
	})
	out := make([]admin.Option, 0, len(ids))
	for _, id := range ids {
		out = append(out, admin.Option{Value: id, Label: labels[id] + " (" + id + ")"})
	}
	return out
}

// ---------- 抽屉表单字段 ----------

// formFields 返回抽屉里可编辑的列：列白名单 ∩ 写入类型表（自增主键不入表单）。
func formFields(entity string) []admin.FieldMeta {
	all, ok := admin.FieldMetas(entity)
	if !ok {
		return nil
	}
	kinds := columnKinds[entity]
	out := make([]admin.FieldMeta, 0, len(all))
	for _, f := range all {
		if _, ok := kinds[f.Key]; ok {
			out = append(out, f)
		}
	}
	return out
}

// ---------- URL 构造（URL 是唯一查询事实源） ----------

func cloneValues(v url.Values) url.Values {
	out := make(url.Values, len(v))
	for k, vals := range v {
		out[k] = append([]string(nil), vals...)
	}
	return out
}

func withQuery(path string, v url.Values) string {
	if q := v.Encode(); q != "" {
		return path + "?" + q
	}
	return path
}

func withPage(path string, raw url.Values, page int) string {
	if page < 1 {
		page = 1
	}
	v := cloneValues(raw)
	v.Del("drawer")
	v.Set("page", strconv.Itoa(page))
	return withQuery(path, v)
}

func pageSizeURLs(path string, raw url.Values) map[int]string {
	out := map[int]string{}
	for _, size := range []int{10, 20, 50} {
		v := cloneValues(raw)
		v.Del("drawer")
		v.Del("page")
		v.Set("pageSize", strconv.Itoa(size))
		out[size] = withQuery(path, v)
	}
	return out
}

// sortURLs 给每个可排序表头生成链接（同列再点即反转方向），切换排序回第一页。
func sortURLs(path, entity string, qs admin.QueryState, raw url.Values) map[string]string {
	out := map[string]string{}
	for _, f := range admin.VisibleFields(entity) {
		if !f.Sortable {
			continue
		}
		dir := "asc"
		if qs.Sort == f.Key && qs.Order == "asc" {
			dir = "desc"
		}
		v := cloneValues(raw)
		v.Del("drawer")
		v.Del("page")
		v.Set("sort", f.Key)
		v.Set("order", dir)
		out[f.Key] = withQuery(path, v)
	}
	return out
}

// buildTags 把已生效条件外显成标签（可单删），条件不允许只存在于「更多筛选」里。
func buildTags(entity string, qs admin.QueryState, raw url.Values) []filterTag {
	var tags []filterTag
	keys := make([]string, 0, len(raw))
	for k := range raw {
		if strings.HasPrefix(k, "filter.") {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		vals := raw[key]
		rest := strings.TrimPrefix(key, "filter.")
		col, op := rest, ""
		if i := strings.LastIndex(rest, "."); i >= 0 {
			col, op = rest[:i], rest[i+1:]
		}
		f, ok := admin.FieldMetaOf(entity, col)
		if !ok {
			continue
		}
		if op == "" && len(f.Operators) > 0 {
			op = f.Operators[0]
		}
		var shown []string
		for _, val := range vals {
			if strings.TrimSpace(val) == "" {
				continue
			}
			shown = append(shown, optionText(entity, f, val))
		}
		if len(shown) == 0 {
			continue
		}
		v := cloneValues(raw)
		v.Del(key)
		v.Del("page")
		v.Del("drawer")
		tags = append(tags, filterTag{
			Label:     f.Label,
			OpLabel:   admin.OperatorLabel(op),
			Value:     strings.Join(shown, "、"),
			RemoveURL: withQuery("/ui/"+entity, v),
		})
	}
	if q := strings.TrimSpace(qs.Q); q != "" {
		v := cloneValues(raw)
		v.Del("q")
		v.Del("page")
		v.Del("drawer")
		tags = append(tags, filterTag{Label: "关键词", OpLabel: "包含", Value: q, RemoveURL: withQuery("/ui/"+entity, v)})
	}
	return tags
}

func optionText(entity string, f admin.FieldMeta, val string) string {
	switch f.FilterType {
	case "boolean", "multiSelect":
		return admin.OptionLabel(entity, f.Key, val)
	default:
		return val
	}
}

// ---------- 抽屉 ----------

func (h *Handler) buildDrawer(r *http.Request, e admin.Entity, rawID string, rel map[string]map[string]string) *drawerData {
	d := &drawerData{
		Entity:   e.Name,
		CloseURL: closeURL(e.Name, rawID),
	}
	fields := formFields(e.Name)
	row := map[string]any{}
	for _, f := range fields {
		ff := formField{F: f}
		switch f.FilterType {
		case "boolean", "multiSelect":
			ff.Options = f.Options
		case "relation", "remoteSelect":
			ff.Options = relationOptions(rel[f.Key])
		}
		d.Forms = append(d.Forms, ff)
	}
	if rawID == "new" {
		d.Mode = "new"
		d.Title = "新建" + entityLabels[e.Name]
		d.Action = "/ui/" + e.Name
		return d
	}
	d.Mode = "edit"
	d.ID = rawID
	d.Title = "编辑" + entityLabels[e.Name]
	d.Action = "/ui/" + e.Name + "/" + url.PathEscape(rawID)
	d.CanDelete = true
	d.DeleteURL = "/ui/" + e.Name + "/" + url.PathEscape(rawID) + "/delete"
	got, err := h.source.Get(r.Context(), e.Name, rawID)
	if err != nil {
		d.Error = "读取该行失败：" + err.Error() + "。可关闭抽屉后重试。"
		return d
	}
	row = got
	for i := range d.Forms {
		if v, ok := row[d.Forms[i].F.Key]; ok && v != nil {
			d.Forms[i].Value = fmt.Sprint(v)
		}
	}
	return d
}

// closeURL 关闭抽屉回到同一筛选/排序/分页的列表（URL 是唯一查询事实源）。
func closeURL(entity, rawID string) string {
	v := url.Values{}
	if rawID != "new" {
		// 保持列表现状：由 JS 用 history.back() 之外的回跳地址兜底。
		return "/ui/" + entity
	}
	return withQuery("/ui/"+entity, v)
}

// ---------- flash ----------

func flashFrom(raw url.Values) *flashMsg {
	kind := raw.Get("flash")
	if kind == "" {
		return nil
	}
	text := raw.Get("flashText")
	if text == "" {
		if kind == "ok" {
			text = "已保存"
		} else {
			text = "操作失败"
		}
	}
	return &flashMsg{Kind: kind, Text: text}
}

// ---------- 错误页 ----------

func (h *Handler) renderError(w http.ResponseWriter, r *http.Request, status int, title, text string) {
	page := &pageData{
		Title:          title,
		Nav:            navItems(""),
		State:          "error",
		StateTitle:     title,
		StateText:      text,
		StateAction:    "返回数据域列表",
		StateActionURL: "/ui/data-domains",
	}
	h.render(w, status, "error", page)
}

func asQueryError(err error, target **admin.QueryError) bool {
	qe, ok := err.(*admin.QueryError)
	if ok {
		*target = qe
	}
	return ok
}
