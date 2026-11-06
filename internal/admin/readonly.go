package admin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// 只读数据源页（R-022 读路径 / R-030 可见面）：与异常两页同一套机制——不进 admin.Entities
// （管理实体仍 6），走独立的 /admin/v1/readonly/{slot} 路由。
//
// 本期三项：
//   - latest-values    latest_value     offset 分页（行数受「终端 × 数据项」约束）
//   - hourly-staging   hourly_staging   offset 分页（普通表，行数受未结算小时约束）
//   - hourly-snapshot  hourly_snapshot  cursor 分页（分区表随时间增长 → 不给 total）
//
// 分页方式由 Pagination 显式声明并随 _meta.readonly[].pagination 下发，前端按它分支：
//   - "offset"：page/pageSize + meta{total,page,pageSize,sort,order}
//   - "cursor"：limit/cursor + meta{hasMore,nextCursor,limit}（没有 total）
type ReadonlyPage struct {
	Slot       string
	Entity     Entity
	Pagination string
	// DefaultSort 覆盖由字段推导出的默认排序列；空表示按 fieldSets 推导。
	DefaultSort string
}

// 分页方式闭集。
const (
	PaginationOffset = "offset"
	PaginationCursor = "cursor"
)

var readonlyPages = []ReadonlyPage{
	{
		Slot: "latest-values",
		Entity: Entity{
			Name:  "latest-values",
			Table: "latest_value",
			Cols: []string{
				"terminal_id", "item_code", "value_num", "value_text",
				"unit", "value_time", "quality", "quality_reason",
			},
		},
		Pagination: PaginationOffset,
	},
	{
		Slot: "hourly-staging",
		Entity: Entity{
			Name:  "hourly-staging",
			Table: "hourly_staging",
			Cols: []string{
				"terminal_id", "item_code", "hour_start", "value_num", "value_text",
				"unit", "value_time", "quality", "quality_reason",
			},
		},
		Pagination: PaginationOffset,
	},
	{
		Slot: "hourly-snapshot",
		Entity: Entity{
			Name:  "hourly-snapshot",
			Table: "hourly_snapshot",
			Cols: []string{
				"terminal_id", "item_code", "hour_start", "value_num", "value_text",
				"unit", "value_time", "quality", "quality_reason", "revision", "settled_at",
			},
		},
		Pagination: PaginationCursor,
		// 命中 hourly_snapshot_settled_idx；tie-breaker 见 snapshotOrderClause。
		DefaultSort: "settled_at",
	},
}

// ReadonlyPages 返回只读页的固定顺序定义，供 _meta 与上层复用。
func ReadonlyPages() []ReadonlyPage {
	out := make([]ReadonlyPage, len(readonlyPages))
	copy(out, readonlyPages)
	return out
}

// ReadonlyBySlot 按 URL 段取只读实体；未知 slot 返回 false（调用方据此 404）。
func ReadonlyBySlot(slot string) (Entity, bool) {
	for _, page := range readonlyPages {
		if page.Slot == slot {
			return page.Entity, true
		}
	}
	return Entity{}, false
}

// ReadonlyPageBySlot 按 URL 段取整页定义（含分页方式）。
func ReadonlyPageBySlot(slot string) (ReadonlyPage, bool) {
	for _, page := range readonlyPages {
		if page.Slot == slot {
			return page, true
		}
	}
	return ReadonlyPage{}, false
}

// readonlyList 按分页方式分发；未知 slot → 404。
func (a *API) readonlyList(w http.ResponseWriter, r *http.Request) {
	page, ok := ReadonlyPageBySlot(r.PathValue("slot"))
	if !ok {
		writeError(w, http.StatusNotFound, "unknown_slot")
		return
	}
	if page.Pagination == PaginationCursor {
		a.readonlyCursor(w, r, page)
		return
	}
	a.writeList(w, r, page.Entity)
}

// ---------- 游标分页（快照页） ----------

// snapshotOrderClause 是快照页固定排序：settled_at 倒序 + 主键升序 tie-breaker。
// 游标条件必须与它严格对应，否则会重/漏行。
const snapshotOrderClause = "settled_at desc, terminal_id asc, item_code asc, hour_start asc"

// readonlyCursorQuery 是游标页解析后的参数。
type readonlyCursorQuery struct {
	State  QueryState
	Limit  int
	Cursor *changesCursor
}

// parseReadonlyCursorQuery 解析游标页参数：filters、q 走既有白名单，分页走 limit/cursor。
func parseReadonlyCursorQuery(e Entity, v url.Values) (readonlyCursorQuery, error) {
	q := readonlyCursorQuery{Limit: ChangesDefaultLimit}
	qs, err := ParseQueryState(e, v)
	if err != nil {
		return q, err
	}
	// 顺序固定（与游标键一致）：不接受 sort/order，否则排序键与游标键不一致会重/漏行。
	if strings.TrimSpace(v.Get("sort")) != "" || strings.TrimSpace(v.Get("order")) != "" {
		return q, invalidQuery("%s 页按 settled_at 固定排序（游标分页），不支持 sort/order", e.Name)
	}
	q.State = qs
	if raw := strings.TrimSpace(v.Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return q, invalidQuery("limit 不是整数: %q", raw)
		}
		if n < 1 {
			n = ChangesDefaultLimit
		}
		if n > ChangesMaxLimit {
			n = ChangesMaxLimit
		}
		q.Limit = n
	}
	if raw := strings.TrimSpace(v.Get("cursor")); raw != "" {
		cur, err := decodeChangesCursor(raw)
		if err != nil {
			return q, invalidQuery("cursor 非法: %v", err)
		}
		q.Cursor = &cur
	}
	return q, nil
}

// CursorListMeta 是游标分页的 meta：只给 hasMore/nextCursor/limit，不给 total。
type CursorListMeta struct {
	HasMore    bool   `json:"hasMore"`
	NextCursor string `json:"nextCursor,omitempty"`
	Limit      int    `json:"limit"`
}

// CursorListResponse 是游标分页的响应信封。
type CursorListResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    any             `json:"data"`
	Meta    *CursorListMeta `json:"meta,omitempty"`
}

// readonlyCursor 执行一次游标分页查询。
func (a *API) readonlyCursor(w http.ResponseWriter, r *http.Request, page ReadonlyPage) {
	q, err := parseReadonlyCursorQuery(page.Entity, r.URL.Query())
	if err != nil {
		var qerr *QueryError
		if errors.As(err, &qerr) {
			writeError(w, http.StatusBadRequest, qerr.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	rows, meta, err := listCursor(r.Context(), a.Pool, page.Entity, q)
	if err != nil {
		var qerr *QueryError
		if errors.As(err, &qerr) {
			writeError(w, http.StatusBadRequest, qerr.Error())
			return
		}
		writeDBError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, CursorListResponse{Code: 0, Message: "ok", Data: rows, Meta: meta})
}

// listCursor 是快照页的游标查询：where 复用 buildWhere（白名单），顺序固定，多取一行判 hasMore。
func listCursor(ctx context.Context, pool *pgxpool.Pool, e Entity, q readonlyCursorQuery) ([]map[string]any, *CursorListMeta, error) {
	where, args, err := buildWhere(e, q.State)
	if err != nil {
		return nil, nil, err
	}
	var sb strings.Builder
	sb.WriteString("select * from ")
	sb.WriteString(e.Table)
	if where != "" {
		sb.WriteString(" where ")
		sb.WriteString(where)
	}
	if q.Cursor != nil {
		args = append(args, q.Cursor.SettledAt, q.Cursor.TerminalID, q.Cursor.ItemCode, q.Cursor.HourStart)
		base := len(args) - 4
		if where != "" {
			sb.WriteString(" and ")
		} else {
			sb.WriteString(" where ")
		}
		// (settled_at desc, 主键 asc) 上的"下一页"条件。
		fmt.Fprintf(&sb, "(settled_at < $%d or (settled_at = $%d and (terminal_id, item_code, hour_start) > ($%d, $%d, $%d)))",
			base+1, base+1, base+2, base+3, base+4)
	}
	args = append(args, q.Limit+1)
	fmt.Fprintf(&sb, " order by %s limit $%d", snapshotOrderClause, len(args))

	rows, err := pool.Query(ctx, sb.String(), args...)
	if err != nil {
		return nil, nil, err
	}
	items, err := collect(rows)
	if err != nil {
		return nil, nil, err
	}
	meta := &CursorListMeta{Limit: q.Limit}
	if len(items) > q.Limit {
		meta.HasMore = true
		items = items[:q.Limit]
		cursor, err := cursorFromRow(items[len(items)-1])
		if err != nil {
			return nil, nil, err
		}
		meta.NextCursor = cursor
	}
	if items == nil {
		items = []map[string]any{}
	}
	return items, meta, nil
}

// cursorFromRow 从结果行拼出下一页游标（编码见 cursor.go，与游标参数共用同一实现）。
func cursorFromRow(row map[string]any) (string, error) {
	settledRaw, ok := row["settled_at"].(string)
	if !ok {
		return "", invalidQuery("快照行缺少 settled_at")
	}
	settledAt, err := time.Parse(time.RFC3339Nano, settledRaw)
	if err != nil {
		return "", invalidQuery("settled_at 格式非法: %v", err)
	}
	hourRaw, ok := row["hour_start"].(string)
	if !ok {
		return "", invalidQuery("快照行缺少 hour_start")
	}
	hour, err := time.Parse(time.RFC3339Nano, hourRaw)
	if err != nil {
		return "", invalidQuery("hour_start 格式非法: %v", err)
	}
	terminal, _ := row["terminal_id"].(string)
	item, _ := row["item_code"].(string)
	return encodeChangesCursor(changesCursor{
		SettledAt: settledAt, TerminalID: terminal, ItemCode: item, HourStart: hour,
	}), nil
}
