// Package admin 提供最小管理 API：数据源、质量规则、结算窗口的 CRUD。
// 本期没有权限模型，默认只监听 127.0.0.1，鉴权中间件留空实现占位（见 auth.go）。
package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/RenJieWeia/iot-admin/internal/clean"
)

// Entity 描述一个可 CRUD 的数据源实体。id 一律用 id::text 匹配，兼容 bigint 与 text 主键。
type Entity struct {
	Name  string   // URL 段
	Table string   // 表名
	Cols  []string // 可写列白名单
	JSONB []string // 需要 ::jsonb 转换的列
	// Hidden 表示「可 CRUD 但不可导航」：实体仍留在 Entities 里（路由与写白名单一字不变），
	// 只是 _meta.entities 不再下发它，改由 _meta.subresources 下发同一份字段元数据，
	// 供详情页取列定义/筛选能力/必填（§1.1）。
	// 零值 false = 可导航，保证现有实体的对外行为不变（若反过来用 Navigable 字段，零值会把现有实体全变成不可导航）。
	Hidden bool
	// PKey 是主键列名；空表示 id。表级配置这类非 id 主键（snapshot_settings.table_name）用它，
	// get/update/delete 的 where 条件统一按它拼（列名只来自代码，不进 SQL 参数）。
	PKey string
	// FixedRows 表示行集固定：允许列表/查看/更新，但禁止新增与删除（handler 直接 400 fixed_rows）。
	// 用于 snapshot_settings 这类「三行表级配置」——行由迁移预置，用户只能改值不能改行数。
	FixedRows bool
	// ExtraSelect 是列表查询追加的派生列表达式（如 concat_ws(...) as enabled_steps）。
	// 只进 SELECT，不参与排序/筛选/写入：FieldMeta 里把这类字段的 Sortable/Filterable 显式置 false。
	ExtraSelect []string
}

// pk 返回 get/update/delete 使用的主键列名。
func (e Entity) pk() string {
	if e.PKey != "" {
		return e.PKey
	}
	return "id"
}

// rejectFixedRows 对「行集固定」的实体拒绝新增/删除：400，而不是静默 404/500。
func rejectFixedRows(w http.ResponseWriter, e Entity) bool {
	if !e.FixedRows {
		return false
	}
	writeError(w, http.StatusBadRequest,
		"fixed_rows: "+e.Name+" 的行集由系统预置，不允许新增或删除（只能修改允许的列）")
	return true
}

// validatePlanBody 只对快照计划生效：档位是列不是行，提交体里出现六档之外的键就是
// 「想新增档位/写派生列」，直接 400；步长越界与「启用却没步长」由 0010 的 CHECK 兜底
// （writeDBError 把 23xxx 映射成 400）。
func validatePlanBody(e Entity, body map[string]any) error {
	if e.Name != "snapshot-plans" {
		return nil
	}
	allowed := make(map[string]struct{}, len(e.Cols))
	for _, c := range e.Cols {
		allowed[c] = struct{}{}
	}
	for key := range body {
		if _, ok := allowed[key]; !ok {
			return invalidQuery("未知字段 %q：快照档位固定为六档（秒/分/时/日/月/年），不能新增档位", key)
		}
	}
	return nil
}

// Entities 是本 API 对外暴露的全部实体，与"非目标"清单保持一致（无消费方/权限/命令/报表）。
var Entities = []Entity{
	{Name: "data-domains", Table: "data_domain", Cols: []string{"code", "name"}},
	{Name: "device-types", Table: "device_type", Cols: []string{"code", "name", "domain_id"}},
	{Name: "thing-model-properties", Hidden: true, Table: "thing_model_property", Cols: []string{
		"type_id", "item_code", "name", "value_type", "unit", "precision", "required", "min_value", "max_value"}},
	{Name: "terminals", Table: "terminal", Cols: []string{"id", "type_id", "gateway_id", "name", "enabled"}},
	{Name: "quality-rules", Table: "quality_rule", Cols: []string{
		"terminal_id", "item_code", "rule_type", "params", "enabled"}, JSONB: []string{"params"}},
	// 快照计划：一行 = 一个序列（终端类型 + 数据项），六档各一组「启用 + 步长」。
	// 档位是列不是行，所以 UI 没有「新增档位」入口；步长白名单与 DB CHECK 同源（见 fields.go）。
	{Name: "snapshot-plans", Table: "snapshot_plan", Cols: []string{
		"device_type_id", "item_code",
		"second_enabled", "second_step", "minute_enabled", "minute_step",
		"hour_enabled", "hour_step", "day_enabled", "day_step",
		"month_enabled", "month_step", "year_enabled", "year_step"},
		ExtraSelect: []string{enabledStepsSelect}},
	// 表级保留期：三行由迁移预置，只允许改 retention_days（禁增删），不进导航。
	{Name: "snapshot-settings", Hidden: true, FixedRows: true, PKey: "table_name",
		Table: "snapshot_settings", Cols: []string{"retention_days"}},
}

// enabledStepsSelect 是快照计划列表的派生列：把六档启用情况拼成「秒30, 分5, 时1」。
// 纯展示列，不可排序/筛选（见 FieldMeta）。
const enabledStepsSelect = `concat_ws(', ',
	case when second_enabled then concat('秒', second_step) end,
	case when minute_enabled then concat('分', minute_step) end,
	case when hour_enabled then concat('时', hour_step) end,
	case when day_enabled then concat('日', day_step) end,
	case when month_enabled then concat('月', month_step) end,
	case when year_enabled then concat('年', year_step) end) as enabled_steps`

// API 是管理接口集合。
type API struct {
	Pool *pgxpool.Pool
	// Invalidate 在 create/update/delete 成功后调用，用于失效进程内元数据缓存。
	// 列表与详情是只读操作，不失效。为 nil 时忽略（无缓存装配的调用方不受影响）。
	Invalidate func()
}

func New(pool *pgxpool.Pool) *API { return &API{Pool: pool} }

// invalidate 安全调用失效钩子。
func (a *API) invalidate() {
	if a.Invalidate != nil {
		a.Invalidate()
	}
}

// Response 是统一返回结构。Meta 只出现在列表响应里，旧客户端忽略即可。
type Response struct {
	Code    int       `json:"code"`
	Message string    `json:"message"`
	Data    any       `json:"data,omitempty"`
	Meta    *ListMeta `json:"meta,omitempty"`
}

// Handler 注册全部管理路由。
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	// 字面量段优先于通配段：_meta 与 exceptions/* 不会被 {entity} / {entity}/{id} 抢走。
	mux.HandleFunc("GET /admin/v1/_meta", a.meta)
	mux.HandleFunc("GET /admin/v1/exceptions/{slot}", a.exceptionList)
	mux.HandleFunc("GET /admin/v1/readonly/{slot}", a.readonlyList)
	mux.HandleFunc("GET /admin/v1/options/{entity}", a.options)
	mux.HandleFunc("GET /admin/v1/{entity}", a.list)
	mux.HandleFunc("POST /admin/v1/{entity}", a.create)
	mux.HandleFunc("GET /admin/v1/{entity}/{id}", a.get)
	mux.HandleFunc("PUT /admin/v1/{entity}/{id}", a.update)
	mux.HandleFunc("DELETE /admin/v1/{entity}/{id}", a.delete)
	return mux
}

func lookup(name string) (Entity, bool) {
	for _, e := range Entities {
		if e.Name == name {
			return e, true
		}
	}
	return Entity{}, false
}

func (a *API) list(w http.ResponseWriter, r *http.Request) {
	e, ok := lookup(r.PathValue("entity"))
	if !ok {
		writeError(w, http.StatusNotFound, "unknown_entity")
		return
	}
	a.writeList(w, r, e)
}

// writeList 是实体列表与异常只读列表的共同实现：同一套 QueryState 校验、同一 meta 结构与错误口径。
func (a *API) writeList(w http.ResponseWriter, r *http.Request, e Entity) {
	qs, err := ParseQueryState(e, r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := ListRows(r.Context(), a.Pool, e, qs)
	if err != nil {
		var qerr *QueryError
		if errors.As(err, &qerr) {
			writeError(w, http.StatusBadRequest, qerr.Error())
			return
		}
		writeDBError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, Response{Code: 0, Message: "ok", Data: res.Data, Meta: &res.Meta})
}

func (a *API) get(w http.ResponseWriter, r *http.Request) {
	e, ok := lookup(r.PathValue("entity"))
	if !ok {
		writeError(w, http.StatusNotFound, "unknown_entity")
		return
	}
	row, err := a.queryOne(r, e, fmt.Sprintf("select * from %s where %s::text = $1", e.Table, e.pk()), r.PathValue("id"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		writeDBError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, Response{Code: 0, Message: "ok", Data: row})
}

func (a *API) create(w http.ResponseWriter, r *http.Request) {
	e, ok := lookup(r.PathValue("entity"))
	if !ok {
		writeError(w, http.StatusNotFound, "unknown_entity")
		return
	}
	if rejectFixedRows(w, e) {
		return
	}
	body, err := decodeBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json: "+err.Error())
		return
	}
	if err := validatePlanBody(e, body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	cols, vals, err := e.columns(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(cols) == 0 {
		writeError(w, http.StatusBadRequest, "empty_body")
		return
	}
	placeholders := make([]string, len(cols))
	for i, c := range cols {
		placeholders[i] = e.placeholder(i+1, c)
	}
	row, err := a.queryOne(r, e, fmt.Sprintf("insert into %s (%s) values (%s) returning *",
		e.Table, strings.Join(cols, ", "), strings.Join(placeholders, ", ")), vals...)
	if err != nil {
		writeDBError(w, err)
		return
	}
	a.invalidate()
	writeJSON(w, http.StatusOK, Response{Code: 0, Message: "ok", Data: row})
}

func (a *API) update(w http.ResponseWriter, r *http.Request) {
	e, ok := lookup(r.PathValue("entity"))
	if !ok {
		writeError(w, http.StatusNotFound, "unknown_entity")
		return
	}
	body, err := decodeBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json: "+err.Error())
		return
	}
	if err := validatePlanBody(e, body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	cols, vals, err := e.columns(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(cols) == 0 {
		writeError(w, http.StatusBadRequest, "empty_body")
		return
	}
	sets := make([]string, len(cols))
	for i, c := range cols {
		sets[i] = c + " = " + e.placeholder(i+1, c)
	}
	vals = append(vals, r.PathValue("id"))
	row, err := a.queryOne(r, e, updateSQL(e, sets, len(cols)+1), vals...)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		writeDBError(w, err)
		return
	}
	a.invalidate()
	writeJSON(w, http.StatusOK, Response{Code: 0, Message: "ok", Data: row})
}

// updateSQL 构造 UPDATE 语句。主键列统一走 e.pk()——snapshot_settings 这类非 id 主键
// （table_name）的实体，若这里写死 id 会 500 column "id" does not exist。
func updateSQL(e Entity, sets []string, idParam int) string {
	return fmt.Sprintf("update %s set %s where %s::text = $%d returning *",
		e.Table, strings.Join(sets, ", "), e.pk(), idParam)
}

func (a *API) delete(w http.ResponseWriter, r *http.Request) {
	e, ok := lookup(r.PathValue("entity"))
	if !ok {
		writeError(w, http.StatusNotFound, "unknown_entity")
		return
	}
	if rejectFixedRows(w, e) {
		return
	}
	tag, err := a.Pool.Exec(r.Context(), fmt.Sprintf("delete from %s where %s::text = $1", e.Table, e.pk()), r.PathValue("id"))
	if err != nil {
		writeDBError(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	a.invalidate()
	writeJSON(w, http.StatusOK, Response{Code: 0, Message: "ok", Data: map[string]any{"deleted": true}})
}

// columns 只接受白名单列，并把 jsonb 列序列化成字节。
func (e Entity) columns(body map[string]any) ([]string, []any, error) {
	cols := make([]string, 0, len(body))
	vals := make([]any, 0, len(body))
	for _, c := range e.Cols {
		v, ok := body[c]
		if !ok {
			continue
		}
		if e.isJSONB(c) {
			raw, err := json.Marshal(v)
			if err != nil {
				return nil, nil, fmt.Errorf("列 %s 不是合法 JSON: %w", c, err)
			}
			v = raw
		}
		if c == "rule_type" {
			s, _ := v.(string)
			if !clean.ValidRuleType(s) {
				return nil, nil, fmt.Errorf("rule_type 不在闭集内: %q", s)
			}
		}
		cols = append(cols, c)
		vals = append(vals, v)
	}
	return cols, vals, nil
}

func (e Entity) isJSONB(col string) bool {
	for _, c := range e.JSONB {
		if c == col {
			return true
		}
	}
	return false
}

func (e Entity) placeholder(n int, col string) string {
	if e.isJSONB(col) {
		return fmt.Sprintf("$%d::jsonb", n)
	}
	return fmt.Sprintf("$%d", n)
}

// queryOne 执行已拼好表名的 SQL 并返回首行（无行时返回 pgx.ErrNoRows）。
func (a *API) queryOne(r *http.Request, e Entity, query string, args ...any) (map[string]any, error) {
	rows, err := a.Pool.Query(r.Context(), query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items, err := collect(rows)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, pgx.ErrNoRows
	}
	return items[0], nil
}

func collect(rows pgx.Rows) ([]map[string]any, error) {
	defer rows.Close()
	fds := rows.FieldDescriptions()
	var out []map[string]any
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return nil, err
		}
		m := make(map[string]any, len(fds))
		for i, fd := range fds {
			m[fd.Name] = jsonValue(vals[i])
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func jsonValue(v any) any {
	switch x := v.(type) {
	case []byte:
		var parsed any
		if err := json.Unmarshal(x, &parsed); err == nil {
			return parsed
		}
		return string(x)
	case time.Time:
		return x.Format(time.RFC3339Nano)
	default:
		return v
	}
}

func decodeBody(r *http.Request) (map[string]any, error) {
	defer r.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return nil, err
	}
	return body, nil
}

func clampInt(s string, def, lo, hi int) int {
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, Response{Code: status, Message: msg})
}

// writeDBError 把约束类错误映射成 400，其余算平台故障。
func writeDBError(w http.ResponseWriter, err error) {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && (strings.HasPrefix(pgErr.Code, "23") || strings.HasPrefix(pgErr.Code, "22")) {
		writeError(w, http.StatusBadRequest, pgErr.Message)
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}
