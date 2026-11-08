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

	"github.com/RenJieWeia/iot-admin/internal/broker"
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

// bindingDuplicateTarget 是「目标已有绑定」的唯一文案：
// 校验阶段先查到、和 create 时 DB 唯一索引抛 23505，两条来源都必须落成这一句。
const bindingDuplicateTarget = "该目标已有一条绑定，请编辑那一条"

// bindingTarget 是解析出的绑定作用域（以及用于物模型校验的实际类型）。
type bindingTarget struct {
	deviceTypeID *int64
	terminalID   *string
	typeID       int64
	label        string
}

// validateBindingBody 校验绑定的写入体，create 与 update 共用同一套规则：
//
//   - currentID 非空表示更新：先读库里的现有行，body 里缺的 device_type_id / terminal_id /
//     item_codes 用它补齐再校验（部分更新只传 item_codes 也必须能过）；
//   - 作用域二选一、item_codes 去重且非空、每个 code 必须在目标类型的物模型里（缺失的全部列出）；
//   - 目标重复 → 统一文案 bindingDuplicateTarget。
//
// 通过时把去重后的 item_codes 写回 body，写库用的是同一份规范化结果。
func (a *API) validateBindingBody(ctx context.Context, e Entity, body map[string]any, currentID string) error {
	if e.Name != "snapshot-bindings" {
		return nil
	}

	merged := map[string]any{}
	if currentID != "" {
		var (
			typeID     *int64
			terminalID *string
			raw        []byte
		)
		if err := a.Pool.QueryRow(ctx,
			`select device_type_id, terminal_id, item_codes from snapshot_binding where id::text = $1`,
			currentID).Scan(&typeID, &terminalID, &raw); err != nil {
			return err // pgx.ErrNoRows 由 handler 转 404
		}
		if typeID != nil {
			merged["device_type_id"] = *typeID
		}
		if terminalID != nil {
			merged["terminal_id"] = *terminalID
		}
		var codes []string
		if err := json.Unmarshal(raw, &codes); err == nil {
			merged["item_codes"] = codes
		}
	}
	for k, v := range body {
		merged[k] = v
	}

	target, err := a.resolveBindingTarget(ctx, merged)
	if err != nil {
		return err
	}

	codes, err := bindingCodes(merged["item_codes"], currentID == "")
	if err != nil {
		return err
	}

	var typeCode string
	if err := a.Pool.QueryRow(ctx, `select code from device_type where id = $1`, target.typeID).Scan(&typeCode); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return invalidQuery("设备类型 #%d 不存在，无法绑定", target.typeID)
		}
		return err
	}
	missing, err := a.missingItemCodes(ctx, target.typeID, codes)
	if err != nil {
		return err
	}
	if len(missing) > 0 {
		quoted, _ := json.Marshal(missing)
		return invalidQuery("%s（%s）的物模型里没有这些数据项：%s，请先在该类型的物模型里声明它们",
			target.label, typeCode, string(quoted))
	}

	// 目标唯一：同一类型 / 同一终端只能有一条绑定（与 0014 的部分唯一索引同口径）。
	var (
		dupID int64
		q     string
		arg   any
	)
	if target.deviceTypeID != nil {
		q, arg = `select id from snapshot_binding where device_type_id = $1`, *target.deviceTypeID
	} else {
		q, arg = `select id from snapshot_binding where terminal_id = $1`, *target.terminalID
	}
	err = a.Pool.QueryRow(ctx, q, arg).Scan(&dupID)
	switch {
	case err == nil:
		if currentID == "" || strconv.FormatInt(dupID, 10) != currentID {
			return invalidQuery("%s", bindingDuplicateTarget)
		}
	case errors.Is(err, pgx.ErrNoRows):
	default:
		return err
	}

	body["item_codes"] = codes
	return nil
}

// resolveBindingTarget 解析作用域：类型级直接用 device_type_id，实例级先查终端的 type_id。
func (a *API) resolveBindingTarget(ctx context.Context, merged map[string]any) (bindingTarget, error) {
	typeRaw, hasType := merged["device_type_id"]
	termRaw, hasTerminal := merged["terminal_id"]
	typeSet := hasType && typeRaw != nil
	terminalSet := hasTerminal && termRaw != nil
	if typeSet == terminalSet {
		return bindingTarget{}, invalidQuery("绑定作用域二选一：device_type_id（类型级）与 terminal_id（实例级）必须且只能给一个")
	}
	if typeSet {
		id, ok := toInt(typeRaw)
		if !ok {
			return bindingTarget{}, invalidQuery("device_type_id 不是整数: %v", typeRaw)
		}
		return bindingTarget{deviceTypeID: &id, typeID: id, label: "设备类型"}, nil
	}
	id, ok := termRaw.(string)
	if !ok || id == "" {
		return bindingTarget{}, invalidQuery("terminal_id 必须是非空字符串: %v", termRaw)
	}
	var typeID int64
	if err := a.Pool.QueryRow(ctx, `select type_id from terminal where id = $1`, id).Scan(&typeID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return bindingTarget{}, invalidQuery("终端 %q 不存在，无法绑定", id)
		}
		return bindingTarget{}, err
	}
	return bindingTarget{terminalID: &id, typeID: typeID, label: "终端 " + id + " 所属类型"}, nil
}

// bindingCodes 规整 item_codes：接受 JSON 数组（[]any / []string），去重保序，拒绝空串与空数组。
// create 时缺失也算错（required=true）；update 时缺失会先用库里现有值补齐，走不到这里。
func bindingCodes(raw any, required bool) ([]string, error) {
	if raw == nil {
		if required {
			return nil, invalidQuery("item_codes 必填：至少绑定一个数据项")
		}
		return nil, nil
	}
	items, ok := raw.([]any)
	if !ok {
		if strs, isStrs := raw.([]string); isStrs {
			items = make([]any, len(strs))
			for i, s := range strs {
				items[i] = s
			}
		} else {
			return nil, invalidQuery("item_codes 必须是字符串数组: %v", raw)
		}
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, it := range items {
		code, isStr := it.(string)
		if !isStr {
			return nil, invalidQuery("item_codes 必须是字符串数组，里面混入了非字符串元素: %v", it)
		}
		code = strings.TrimSpace(code)
		if code == "" {
			return nil, invalidQuery("item_codes 不能包含空字符串")
		}
		if _, dup := seen[code]; dup {
			continue
		}
		seen[code] = struct{}{}
		out = append(out, code)
	}
	if len(out) == 0 {
		return nil, invalidQuery("item_codes 不能为空：至少绑定一个数据项")
	}
	return out, nil
}

// missingItemCodes 返回 codes 中不在该类型物模型里的那些（一次列全，别让用户改一个提交一次）。
func (a *API) missingItemCodes(ctx context.Context, typeID int64, codes []string) ([]string, error) {
	rows, err := a.Pool.Query(ctx,
		`select item_code from thing_model_property where type_id = $1 and item_code = any($2)`, typeID, codes)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	present := make(map[string]struct{}, len(codes))
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, err
		}
		present[code] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var missing []string
	for _, code := range codes {
		if _, ok := present[code]; !ok {
			missing = append(missing, code)
		}
	}
	return missing, nil
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

// writeEntityDBError 把绑定表的唯一索引冲突（23505）落成与校验阶段同一句话，
// 避免用户看到两种说法；其余交给 writeDBError。
func writeEntityDBError(w http.ResponseWriter, e Entity, err error) {
	var pgErr *pgconn.PgError
	if e.Name == "snapshot-bindings" && errors.As(err, &pgErr) && pgErr.Code == "23505" {
		writeError(w, http.StatusBadRequest, bindingDuplicateTarget)
		return
	}
	writeDBError(w, err)
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
	// 终端类型：列表带两个派生计数，让"类型绑定字段 / 类型下有几台终端"一眼可见。
	{Name: "thing-models", Table: "device_type", Cols: []string{"code", "name", "domain_id"},
		ExtraSelect: []string{
			"(select count(*) from thing_model_property p where p.type_id = device_type.id) as properties_count",
			"(select count(*) from terminal t where t.type_id = device_type.id) as terminals_count",
		}},
	{Name: "thing-model-properties", Hidden: true, Table: "thing_model_property", Cols: []string{
		"type_id", "item_code", "name", "value_type", "unit", "precision", "required", "min_value", "max_value"}},
	{Name: "terminals", Table: "terminal", Cols: []string{"id", "type_id", "gateway_id", "name", "enabled"}},
	// 边缘网关注册表：接入面按 gateway_id 鉴权（未注册/停用直接 403），主键不是 id。
	{Name: "gateways", Table: "gateway", PKey: "gateway_id",
		Cols: []string{"gateway_id", "name", "enabled", "remark"}},
	{Name: "quality-rules", Table: "quality_rule", Cols: []string{
		"terminal_id", "item_code", "rule_type", "params", "enabled"}, JSONB: []string{"params"}},
	// 全局六档配置：六行固定（granularity 主键），只允许改 enabled / step。
	// 档位本身不可新增（行集固定），步长白名单与 0013 的 CHECK 同源。
	{Name: "snapshot-slots", FixedRows: true, PKey: "granularity",
		Table: "snapshot_slot_config", Cols: []string{"enabled", "step"}},
	// 快照查询模板：服务端共享，保存「终端类型 + 字段」的展示配置（档位/时间在查询时选）。
	{Name: "snapshot-queries", Table: "snapshot_query_template",
		Cols: []string{"name", "description", "device_type_ids", "item_codes"},
		JSONB: []string{"device_type_ids", "item_codes"}},
	// 绑定：一条 = 一个作用域目标 + 一个数据项数组（jsonb）；作用域二选一（类型级或实例级）。
	// item_codes 走已有的 ::jsonb 转换；列表额外下发派生列「字段数」供前端显示。
	{Name: "snapshot-bindings", Hidden: true, Table: "snapshot_binding",
		Cols:        []string{"device_type_id", "terminal_id", "item_codes"},
		JSONB:       []string{"item_codes"},
		ExtraSelect: []string{"jsonb_array_length(item_codes) as fields_count"}},
	// 表级保留期：三行由迁移预置，只允许改 retention_days（禁增删），不进导航。
	{Name: "snapshot-settings", Hidden: true, FixedRows: true, PKey: "table_name",
		Table: "snapshot_settings", Cols: []string{"retention_days"}},
}

// BrokerAdmin 是网关注册/注销所需的 Broker 管理接口（HTTP 客户端或测试替身）。
type BrokerAdmin interface {
	// Register 在 Broker 建号授权，返回一次性密码（只在创建响应里回给调用方）。
	Register(ctx context.Context, gw broker.Gateway) (string, error)
	Unregister(ctx context.Context, gatewayID string) error
}

// API 是管理接口集合。
type API struct {
	Pool *pgxpool.Pool
	// Broker 非空时，网关的增删会同步到 Broker（注册/注销）；nil 表示未配置，只写本地注册表。
	Broker BrokerAdmin
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
	// 快照查询：按模板 + 档位 + 时间范围透视出「终端 × 字段」的最新值。
	mux.HandleFunc("GET /admin/v1/snapshot-query", a.snapshotQuery)
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
	// 兼容旧 URL：device-types 是 thing-models 的历史名字（表名仍是 device_type）。
	if name == "device-types" {
		name = "thing-models"
	}
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
	if err := a.validateBindingBody(r.Context(), e, body, ""); err != nil {
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
	// 只有"启用"的网关才在 Broker 注册；新增时先建号，失败就不写本地行。
	var brokerPassword string
	if e.Name == "gateways" && a.Broker != nil && gatewayEnabled(body) {
		password, err := a.Broker.Register(r.Context(), gatewayFromBody(body))
		if err != nil {
			writeBrokerError(w, "broker_register_failed", err)
			return
		}
		brokerPassword = password
	}
	placeholders := make([]string, len(cols))
	for i, c := range cols {
		placeholders[i] = e.placeholder(i+1, c)
	}
	row, err := a.queryOne(r, e, fmt.Sprintf("insert into %s (%s) values (%s) returning *",
		e.Table, strings.Join(cols, ", "), strings.Join(placeholders, ", ")), vals...)
	if err != nil {
		// 本地写入失败时补偿注销，避免 Broker 留下没有本地记录的账号。
		if e.Name == "gateways" && a.Broker != nil {
			_ = a.Broker.Unregister(r.Context(), gatewayFromBody(body).ID)
		}
		writeEntityDBError(w, e, err)
		return
	}
	if brokerPassword != "" && row != nil {
		// 密码只在创建时回一次（Broker 侧只存哈希），提示调用方立即交付给网关。
		row["broker_password"] = brokerPassword
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
	// 绑定的部分更新：校验函数自己读库补齐缺的字段（只传 item_codes 也能过）。
	if err := a.validateBindingBody(r.Context(), e, body, r.PathValue("id")); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
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
	// 网关：启用状态切换时同步 Broker（开→注册，关→注销），其余字段照常只改本地。
	var brokerPassword string
	if e.Name == "gateways" && a.Broker != nil {
		if enabled, ok := body["enabled"].(bool); ok {
			current, name, err := a.gatewayState(r.Context(), r.PathValue("id"))
			switch {
			case err != nil && !errors.Is(err, pgx.ErrNoRows):
				writeDBError(w, err)
				return
			case err == nil && !current && enabled:
				if raw, ok := body["name"].(string); ok && raw != "" {
					name = raw
				}
				password, err := a.Broker.Register(r.Context(), broker.Gateway{ID: r.PathValue("id"), Name: name})
				if err != nil {
					writeBrokerError(w, "broker_register_failed", err)
					return
				}
				brokerPassword = password
			case err == nil && current && !enabled:
				if err := a.Broker.Unregister(r.Context(), r.PathValue("id")); err != nil {
					writeBrokerError(w, "broker_unregister_failed", err)
					return
				}
			}
		}
	}
	sets := make([]string, len(cols))
	for i, c := range cols {
		sets[i] = c + " = " + e.placeholder(i+1, c)
	}
	// 快照计划每次保存都要刷新「更新时间」，否则页面在保存后仍显示旧时间，像没存上。
	if e.Name == "snapshot-slots" {
		sets = append(sets, "updated_at = now()")
	}
	// 停用终端即视为离线，不必等下一批报文。
	if e.Name == "terminals" {
		if enabled, ok := body["enabled"].(bool); ok && !enabled {
			sets = append(sets, "online = false")
		}
	}
	vals = append(vals, r.PathValue("id"))
	row, err := a.queryOne(r, e, updateSQL(e, sets, len(cols)+1), vals...)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		writeEntityDBError(w, e, err)
		return
	}
	if brokerPassword != "" && row != nil {
		row["broker_password"] = brokerPassword
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
	// 网关删除先回收 Broker 侧账号：回滚比"Broker 还留着能连的账号"安全。
	if e.Name == "gateways" && a.Broker != nil {
		if err := a.Broker.Unregister(r.Context(), r.PathValue("id")); err != nil {
			writeBrokerError(w, "broker_unregister_failed", err)
			return
		}
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

// gatewayFromBody 把创建网关的请求体映射成 Broker 注册参数。
func gatewayFromBody(body map[string]any) broker.Gateway {
	id, _ := body["gateway_id"].(string)
	name, _ := body["name"].(string)
	return broker.Gateway{ID: id, Name: name}
}

// writeBrokerError 把 Broker 侧失败落成 502：本平台不是网关配置的权威来源，
// 不能假装成功，也不能把它说成数据错误（400）。
func writeBrokerError(w http.ResponseWriter, code string, err error) {
	writeError(w, http.StatusBadGateway, code+": "+err.Error())
}

// gatewayEnabled 读创建请求里的启用状态；缺省按库里默认值 true 处理。
func gatewayEnabled(body map[string]any) bool {
	if raw, ok := body["enabled"].(bool); ok {
		return raw
	}
	return true
}

// gatewayState 读网关当前的启用状态与名称（启用切换时判断是否需要同步 Broker）。
func (a *API) gatewayState(ctx context.Context, id string) (bool, string, error) {
	var (
		enabled bool
		name    string
	)
	err := a.Pool.QueryRow(ctx, `select enabled, name from gateway where gateway_id = $1`, id).Scan(&enabled, &name)
	return enabled, name, err
}
