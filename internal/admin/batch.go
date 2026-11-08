package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/RenJieWeia/iot-admin/internal/broker"
	"github.com/jackc/pgx/v5"
)

// 批量操作上限：范围过大时让用户缩小筛选，避免一次请求跑太久。
const batchMaxTargets = 10000

type batchRequest struct {
	Action string     `json:"action"`
	Scope  batchScope `json:"scope"`
}

type batchScope struct {
	Mode    string        `json:"mode"` // ids | filter
	IDs     []string      `json:"ids"`
	Q       string        `json:"q"`
	Filters []batchFilter `json:"filters"`
}

type batchFilter struct {
	Field  string   `json:"field"`
	Op     string   `json:"op"`
	Values []string `json:"values"`
}

type batchFailure struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

type batchCredential struct {
	ID       string `json:"id"`
	Password string `json:"password"`
}

type batchResponse struct {
	Action      string            `json:"action"`
	Requested   int               `json:"requested"`
	Succeeded   int               `json:"succeeded"`
	Failed      []batchFailure    `json:"failed"`
	Credentials []batchCredential `json:"credentials,omitempty"`
}

// batch 是业务组列表的批量端点：按显式 ID 或当前筛选条件执行启用/停用/删除。
// 逐项复用单条语义（终端软删除、引用阻断、网关 Broker 同步），返回逐项结果。
func (a *API) batch(w http.ResponseWriter, r *http.Request) {
	e, ok := lookup(r.PathValue("entity"))
	if !ok {
		writeError(w, http.StatusNotFound, "unknown_entity")
		return
	}
	var req batchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json: "+err.Error())
		return
	}
	if err := validateBatchAction(e, req.Action); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ids, err := a.batchTargets(r.Context(), e, req.Scope)
	if err != nil {
		writeValidateError(w, err)
		return
	}
	if len(ids) == 0 {
		writeJSON(w, http.StatusOK, Response{Code: 0, Message: "ok", Data: batchResponse{Action: req.Action, Failed: []batchFailure{}}})
		return
	}

	resp := batchResponse{Action: req.Action, Requested: len(ids), Failed: []batchFailure{}}
	changed := false
	for _, id := range ids {
		var ferr error
		var password string
		switch req.Action {
		case "enable", "disable":
			password, ferr = a.batchSetEnabled(r.Context(), e, id, req.Action == "enable")
			if password != "" {
				resp.Credentials = append(resp.Credentials, batchCredential{ID: id, Password: password})
			}
		case "delete":
			ferr = a.batchDeleteOne(r.Context(), e, id)
		}
		if ferr != nil {
			resp.Failed = append(resp.Failed, batchFailure{ID: id, Reason: batchReason(ferr)})
			continue
		}
		resp.Succeeded++
		changed = true
	}
	if changed {
		a.invalidate()
	}
	writeJSON(w, http.StatusOK, Response{Code: 0, Message: "ok", Data: resp})
}

func validateBatchAction(e Entity, action string) error {
	switch action {
	case "enable", "disable":
		switch e.Name {
		case "terminals", "gateways", "quality-rules":
			return nil
		}
		return fmt.Errorf("实体 %s 不支持批量启停", e.Name)
	case "delete":
		switch e.Name {
		case "data-domains", "thing-models", "terminals", "gateways", "quality-rules":
			return nil
		}
		return fmt.Errorf("实体 %s 不支持批量删除", e.Name)
	default:
		return fmt.Errorf("未知批量动作: %s", action)
	}
}

// batchTargets 把 scope 解析成主键列表：ids 直接用，filter 复用列表筛选构造 SQL。
func (a *API) batchTargets(ctx context.Context, e Entity, scope batchScope) ([]string, error) {
	if scope.Mode == "ids" {
		out := make([]string, 0, len(scope.IDs))
		seen := make(map[string]struct{}, len(scope.IDs))
		for _, id := range scope.IDs {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
			if len(out) > batchMaxTargets {
				return nil, invalidQuery("一次最多操作 %d 项，请分批执行", batchMaxTargets)
			}
		}
		return out, nil
	}
	if scope.Mode != "filter" {
		return nil, invalidQuery("scope.mode 只支持 ids 或 filter")
	}
	values := url.Values{}
	if scope.Q != "" {
		values.Set("q", scope.Q)
	}
	for _, f := range scope.Filters {
		if f.Field == "" || len(f.Values) == 0 {
			continue
		}
		key := "filter." + f.Field
		if f.Op != "" {
			key += "." + f.Op
		}
		values.Set(key, strings.Join(f.Values, ","))
	}
	filters, err := parseFilters(e, values)
	if err != nil {
		return nil, err
	}
	where, args, err := buildWhere(e, QueryState{Q: scope.Q, Filters: filters})
	if err != nil {
		return nil, err
	}
	sql := fmt.Sprintf("select %s::text from %s where %s limit %d", e.pk(), e.Table, where, batchMaxTargets+1)
	rows, err := a.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) > batchMaxTargets {
		return nil, invalidQuery("筛选结果超过 %d 项，请缩小范围后重试", batchMaxTargets)
	}
	return out, nil
}

// batchSetEnabled 逐项启停；网关启用时同步 Broker，返回一次性密码。
func (a *API) batchSetEnabled(ctx context.Context, e Entity, id string, enabled bool) (string, error) {
	switch e.Name {
	case "terminals":
		tag, err := a.Pool.Exec(ctx,
			`update terminal set enabled = $1, online = case when $1 then online else false end
			 where id = $2 and deleted_at is null`, enabled, id)
		if err != nil {
			return "", err
		}
		if tag.RowsAffected() == 0 {
			return "", pgx.ErrNoRows
		}
		return "", nil
	case "quality-rules":
		tag, err := a.Pool.Exec(ctx, `update quality_rule set enabled = $1 where id::text = $2`, enabled, id)
		if err != nil {
			return "", err
		}
		if tag.RowsAffected() == 0 {
			return "", pgx.ErrNoRows
		}
		return "", nil
	case "gateways":
		current, name, err := a.gatewayState(ctx, id)
		if err != nil {
			return "", err
		}
		password := ""
		if current != enabled && a.Broker != nil {
			if enabled {
				password, err = a.Broker.Register(ctx, broker.Gateway{ID: id, Name: name})
				if err != nil {
					return "", fmt.Errorf("broker_register_failed: %w", err)
				}
			} else if err := a.Broker.Unregister(ctx, id); err != nil {
				return "", fmt.Errorf("broker_unregister_failed: %w", err)
			}
		}
		if _, err := a.Pool.Exec(ctx, `update gateway set enabled = $1 where gateway_id = $2`, enabled, id); err != nil {
			return "", err
		}
		return password, nil
	}
	return "", fmt.Errorf("实体 %s 不支持启停", e.Name)
}

// batchDeleteOne 逐项删除，复用单条的引用阻断与终端软删除。
func (a *API) batchDeleteOne(ctx context.Context, e Entity, id string) error {
	if e.Name == "gateways" && a.Broker != nil {
		if err := a.Broker.Unregister(ctx, id); err != nil {
			return fmt.Errorf("broker_unregister_failed: %w", err)
		}
	}
	if msg, blocked, err := a.deleteBlocked(ctx, e, id); err != nil {
		return err
	} else if blocked {
		return errors.New(msg)
	}
	if e.Name == "terminals" {
		tag, err := a.Pool.Exec(ctx,
			`update terminal set deleted_at = now(), enabled = false, online = false
			 where id = $1 and deleted_at is null`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		return nil
	}
	tag, err := a.Pool.Exec(ctx, fmt.Sprintf("delete from %s where %s::text = $1", e.Table, e.pk()), id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func batchReason(err error) string {
	if errors.Is(err, pgx.ErrNoRows) {
		return "对象不存在或已删除"
	}
	return err.Error()
}
