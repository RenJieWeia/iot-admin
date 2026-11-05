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
}

// Entities 是本 API 对外暴露的全部实体，与"非目标"清单保持一致（无消费方/权限/命令/报表）。
var Entities = []Entity{
	{Name: "data-domains", Table: "data_domain", Cols: []string{"code", "name"}},
	{Name: "device-types", Table: "device_type", Cols: []string{"code", "name", "domain_id"}},
	{Name: "thing-model-properties", Table: "thing_model_property", Cols: []string{
		"type_id", "item_code", "name", "value_type", "unit", "precision", "required", "min_value", "max_value"}},
	{Name: "terminals", Table: "terminal", Cols: []string{"id", "type_id", "gateway_id", "name", "enabled"}},
	{Name: "quality-rules", Table: "quality_rule", Cols: []string{
		"terminal_id", "item_code", "rule_type", "params", "enabled"}, JSONB: []string{"params"}},
	{Name: "settle-configs", Table: "settle_config", Cols: []string{"domain_id", "type_id", "window_seconds"}},
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

// Response 是统一返回结构。
type Response struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Handler 注册全部管理路由。
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
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
	limit := clampInt(r.URL.Query().Get("limit"), 200, 1, 1000)
	offset := clampInt(r.URL.Query().Get("offset"), 0, 0, 1<<30)
	rows, err := a.Pool.Query(r.Context(),
		fmt.Sprintf("select * from %s order by id limit $1 offset $2", e.Table), limit, offset)
	if err != nil {
		writeDBError(w, err)
		return
	}
	items, err := collect(rows)
	if err != nil {
		writeDBError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, Response{Code: 0, Message: "ok", Data: items})
}

func (a *API) get(w http.ResponseWriter, r *http.Request) {
	e, ok := lookup(r.PathValue("entity"))
	if !ok {
		writeError(w, http.StatusNotFound, "unknown_entity")
		return
	}
	row, err := a.queryOne(r, e, fmt.Sprintf("select * from %s where id::text = $1", e.Table), r.PathValue("id"))
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
	body, err := decodeBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json: "+err.Error())
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
	row, err := a.queryOne(r, e, fmt.Sprintf("update %s set %s where id::text = $%d returning *",
		e.Table, strings.Join(sets, ", "), len(cols)+1), vals...)
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

func (a *API) delete(w http.ResponseWriter, r *http.Request) {
	e, ok := lookup(r.PathValue("entity"))
	if !ok {
		writeError(w, http.StatusNotFound, "unknown_entity")
		return
	}
	tag, err := a.Pool.Exec(r.Context(), fmt.Sprintf("delete from %s where id::text = $1", e.Table), r.PathValue("id"))
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
