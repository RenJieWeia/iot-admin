package admin

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// GET /admin/v1/snapshot/changes?since=<RFC3339>&limit=&cursor=
//
// 只读：返回 settled_at > since 的快照行，按 (settled_at, terminal_id, item_code, hour_start)
// 升序分页。这是"历史被改写可查询"的唯一入口（R-032）：revision > 0 的行就是被修正过的格子。
//
// 约束：
//   - since 必填且必须是 RFC3339（含偏移）；
//   - limit 默认 200、上限 1000；
//   - cursor 不透明串（上一页最后一行的 settled_at + 主键的 base64），只能原样回传；
//   - 与列表端点不同，这里**不**接受 filter/sort/order（顺序固定，分页靠游标保证不重不漏）。

// 游标分页的限额与编解码在 cursor.go（只读页与 changes 端点共用同一份实现）。

// ChangeRow 是一行快照变更（只读投影，不暴露 value 之外的内部列）。
type ChangeRow struct {
	TerminalID    string    `json:"terminalId"`
	ItemCode      string    `json:"itemCode"`
	HourStart     time.Time `json:"hourStart"`
	ValueNum      *float64  `json:"valueNum"`
	ValueText     *string   `json:"valueText"`
	Unit          string    `json:"unit"`
	ValueTime     time.Time `json:"valueTime"`
	Revision      int       `json:"revision"`
	SettledAt     time.Time `json:"settledAt"`
	Quality       int16     `json:"quality"`
	QualityReason string    `json:"qualityReason"`
}

// ChangesPage 是 changes 端点的 data：行 + 分页信息（cursor 为空表示没有下一页）。
type ChangesPage struct {
	Rows    []ChangeRow `json:"rows"`
	Limit   int         `json:"limit"`
	HasMore bool        `json:"hasMore"`
	Cursor  string      `json:"cursor,omitempty"`
}

// changesQuery 是解析后的游标分页参数。
type changesQuery struct {
	Since  time.Time
	Limit  int
	Cursor *changesCursor
}

// parseChangesQuery 解析并校验查询参数；任何非法值都返回 *QueryError（400 invalid_query）。
func parseChangesQuery(v url.Values) (changesQuery, error) {
	q := changesQuery{Limit: ChangesDefaultLimit}
	rawSince := strings.TrimSpace(v.Get("since"))
	if rawSince == "" {
		return q, invalidQuery("since 必填（RFC3339）")
	}
	since, err := time.Parse(time.RFC3339Nano, rawSince)
	if err != nil {
		return q, invalidQuery("since 不是 RFC3339 时间: %q", rawSince)
	}
	q.Since = since

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

// changesSQL 是唯一查询：since 之后、游标之后，按四列升序，多取一行用于判断 hasMore。
const changesSQL = `
	select terminal_id, item_code, hour_start, value_num, value_text, coalesce(unit, ''),
	       value_time, revision, settled_at, quality, coalesce(quality_reason, '')
	from hourly_snapshot
	where settled_at > $1
	  and ($2::timestamptz is null
	       or (settled_at, terminal_id, item_code, hour_start) > ($2, $3, $4, $5))
	order by settled_at asc, terminal_id asc, item_code asc, hour_start asc
	limit $6`

// ListChanges 执行一次游标分页查询。
func ListChanges(ctx context.Context, pool *pgxpool.Pool, q changesQuery) (ChangesPage, error) {
	var (
		cursorAt                   *time.Time
		cursorTerminal, cursorItem string
		cursorHour                 *time.Time
	)
	if q.Cursor != nil {
		settled := q.Cursor.SettledAt
		hour := q.Cursor.HourStart
		cursorAt, cursorHour = &settled, &hour
		cursorTerminal, cursorItem = q.Cursor.TerminalID, q.Cursor.ItemCode
	}
	rows, err := pool.Query(ctx, changesSQL,
		q.Since, cursorAt, cursorTerminal, cursorItem, cursorHour, q.Limit+1)
	if err != nil {
		return ChangesPage{}, err
	}
	defer rows.Close()

	items := make([]ChangeRow, 0, q.Limit+1)
	for rows.Next() {
		var r ChangeRow
		if err := rows.Scan(&r.TerminalID, &r.ItemCode, &r.HourStart, &r.ValueNum, &r.ValueText,
			&r.Unit, &r.ValueTime, &r.Revision, &r.SettledAt, &r.Quality, &r.QualityReason); err != nil {
			return ChangesPage{}, err
		}
		items = append(items, r)
	}
	if err := rows.Err(); err != nil {
		return ChangesPage{}, err
	}

	page := ChangesPage{Rows: items, Limit: q.Limit}
	if len(items) > q.Limit {
		page.HasMore = true
		page.Rows = items[:q.Limit]
		last := page.Rows[len(page.Rows)-1]
		page.Cursor = encodeChangesCursor(changesCursor{
			SettledAt: last.SettledAt, TerminalID: last.TerminalID,
			ItemCode: last.ItemCode, HourStart: last.HourStart,
		})
	}
	if page.Rows == nil {
		page.Rows = []ChangeRow{}
	}
	return page, nil
}

// changes 处理 GET /admin/v1/snapshot/changes。
func (a *API) changes(w http.ResponseWriter, r *http.Request) {
	q, err := parseChangesQuery(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	page, err := ListChanges(r.Context(), a.Pool, q)
	if err != nil {
		writeDBError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, Response{Code: 0, Message: "ok", Data: page})
}
