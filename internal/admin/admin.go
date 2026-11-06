// Package admin 提供最小管理 API：数据源、质量规则、结算窗口的 CRUD。
// 本期没有权限模型，默认只监听 127.0.0.1，鉴权中间件留空实现占位（见 auth.go）。
package admin

import (
	"context"
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
	"github.com/RenJieWeia/iot-admin/internal/snapshot"
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

// validateSlotBody 只对六档配置生效：行集固定为六档，提交体里出现 enabled/step 之外的键
// 就是「想新增档位/改主键」，直接 400，不静默丢字段。
func validateSlotBody(e Entity, body map[string]any) error {
	if e.Name != "snapshot-slots" {
		return nil
	}
	for key := range body {
		if key != "enabled" && key != "step" {
			return invalidQuery("未知字段 %q：六档是固定行，只允许改 enabled / step（档位本身不可新增）", key)
		}
	}
	return nil
}

// validateSlotStep 用 Go 侧白名单先拦一次非法步长，给出比 DB CHECK(23514) 更好读的原因。
// 档位取自 URL 的 {id}（snapshot_slot_config 的主键就是 granularity）。
func validateSlotStep(e Entity, granularity string, body map[string]any) error {
	if e.Name != "snapshot-slots" {
		return nil
	}
	raw, ok := body["step"]
	if !ok || raw == nil {
		return nil
	}
	step, ok := toInt(raw)
	if !ok {
		return invalidQuery("step 不是整数: %v", raw)
	}
	if err := snapshot.Validate(int(step), snapshot.Granularity(granularity)); err != nil {
		return invalidQuery("%v", err)
	}
	return nil
}

// validateBindingBody 校验绑定：作用域二选一 + item_code 必须属于目标的物模型。
//
// 为什么要连库校验：绑定是"纳入快照"的唯一开关，存进一条指向不存在数据项的绑定，
// 会让这台终端永远静默不产生快照——错误应该在被拒的那一次请求里说清楚。
func (a *API) validateBindingBody(ctx context.Context, e Entity, body map[string]any) error {
	if e.Name != "snapshot-bindings" {
		return nil
	}
	typeID, hasType := body["device_type_id"]
	terminalID, hasTerminal := body["terminal_id"]
	typeSet := hasType && typeID != nil
	terminalSet := hasTerminal && terminalID != nil
	if typeSet == terminalSet {
		return invalidQuery("绑定作用域二选一：device_type_id（类型级）与 terminal_id（实例级）必须且只能给一个")
	}

	var zone string
	var target int64
	if typeSet {
		id, ok := toInt(typeID)
		if !ok {
			return invalidQuery("device_type_id 不是整数: %v", typeID)
		}
		target = id
		zone = "设备类型"
	} else {
		id, ok := terminalID.(string)
		if !ok || id == "" {
			return invalidQuery("terminal_id 必须是非空字符串: %v", terminalID)
		}
		if err := a.Pool.QueryRow(ctx, `select type_id from terminal where id = $1`, id).Scan(&target); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return invalidQuery("终端 %q 不存在，无法绑定", id)
			}
			return err
		}
		zone = "终端 " + id + " 所属类型"
	}

	code, ok := body["item_code"].(string)
	if !ok || code == "" {
		return invalidQuery("item_code 必须是非空字符串: %v", body["item_code"])
	}
	var typeCode string
	if err := a.Pool.QueryRow(ctx, `select code from device_type where id = $1`, target).Scan(&typeCode); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return invalidQuery("设备类型 #%d 不存在，无法绑定", target)
		}
		return err
	}
	var exists bool
	if err := a.Pool.QueryRow(ctx,
		`select exists(select 1 from thing_model_property where type_id = $1 and item_code = $2)`,
		target, code).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return invalidQuery("%s（%s）的物模型里没有数据项 %q，请先在该类型的物模型里声明它",
			zone, typeCode, code)
	}
	return nil
}

// toInt 把 JSON 数字/字符串统一成 int64。
func toInt(v any) (int64, bool) {
	switch x := v.(type) {
	case float64:
		return int64(x), true
	case int64:
		return x, true
	case int:
		return int64(x), true
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		if err != nil {
			return 0, false
		}
		return n, true
	}
	return 0, false
}

// writeValidateError 把校验错误分流：QueryError → 400（原因写给前端），其余按平台故障。
func writeValidateError(w http.ResponseWriter, err error) {
	var qerr *QueryError
	if errors.As(err, &qerr) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeDBError(w, err)
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
	// 全局六档配置：六行固定（granularity 主键），只允许改 enabled / step。
	// 档位本身不可新增（行集固定），步长白名单与 0013 的 CHECK 同源。
	{Name: "snapshot-slots", FixedRows: true, PKey: "granularity",
		Table: "snapshot_slot_config", Cols: []string{"enabled", "step"}},
	// 绑定：一条 = 一个数据项被纳入快照；作用域二选一（类型级或实例级）。
	{Name: "snapshot-bindings", Hidden: true, Table: "snapshot_binding",
		Cols: []string{"device_type_id", "terminal_id", "item_code"}},
	// 表级保留期：三行由迁移预置，只允许改 retention_days（禁增删），不进导航。
	{Name: "snapshot-settings", Hidden: true, FixedRows: true, PKey: "table_name",
		Table: "snapshot_settings", Cols: []string{"retention_days"}},
}

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
	if err := validateSlotBody(e, body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// 绑定的物模型校验要连库，放在建行之前：不合法就 400，不存进去等运行时静默失效。
	if err := a.validateBindingBody(r.Context(), e, body); err != nil {
		writeValidateError(w, err)
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
	if err := validateSlotBody(e, body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// 步长白名单在 Go 侧先拦一次（DB CHECK 是最后一道，但报错信息不如这里好读）。
	if err := validateSlotStep(e, r.PathValue("id"), body); err != nil {
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
