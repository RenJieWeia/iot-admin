package admin

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// 快照只读页的游标分页：键 = (segment_start desc, terminal_id, item_code[, granularity])。
//
// 编码格式沿用已下线的 changes 端点（base64url 无填充 + `\x1f` 分隔 + RFC3339Nano/UTC）：
// 时间是固定纳秒精度，避免精度丢失导致跳行；游标是**不透明串**，调用方只能原样回传。
//
// 三个 snapshot-* slot 都是分区表，count(*) 会随存活分区数退化，因此只做游标分页、不报总数。

const (
	// SnapshotDefaultLimit 是游标页默认条数。
	SnapshotDefaultLimit = 200
	// SnapshotMaxLimit 是游标页上限（超出按上限截断）。
	SnapshotMaxLimit = 1000
)

// snapshotCursor 是游标解码后的位置（段起点 + 自然键）。
type snapshotCursor struct {
	SegmentStart time.Time
	TerminalID   string
	ItemCode     string
	Granularity  string // 只有 snapshot_coarse 使用
}

const snapshotCursorLayout = time.RFC3339Nano

func encodeSnapshotCursor(c snapshotCursor, withGranularity bool) string {
	parts := []string{
		c.SegmentStart.UTC().Format(snapshotCursorLayout),
		c.TerminalID,
		c.ItemCode,
	}
	if withGranularity {
		parts = append(parts, c.Granularity)
	}
	return base64.RawURLEncoding.EncodeToString([]byte(strings.Join(parts, "\x1f")))
}

func decodeSnapshotCursor(s string, withGranularity bool) (snapshotCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return snapshotCursor{}, errors.New("不是合法的 base64url")
	}
	parts := strings.Split(string(raw), "\x1f")
	want := 3
	if withGranularity {
		want = 4
	}
	if len(parts) != want {
		return snapshotCursor{}, errors.New("字段数不符")
	}
	seg, err := time.Parse(snapshotCursorLayout, parts[0])
	if err != nil {
		return snapshotCursor{}, fmt.Errorf("segment_start: %w", err)
	}
	if parts[1] == "" || parts[2] == "" {
		return snapshotCursor{}, errors.New("主键为空")
	}
	c := snapshotCursor{SegmentStart: seg, TerminalID: parts[1], ItemCode: parts[2]}
	if withGranularity {
		if parts[3] == "" {
			return snapshotCursor{}, errors.New("granularity 为空")
		}
		c.Granularity = parts[3]
	}
	return c, nil
}

// snapshotQuery 是解析后的游标分页参数（顺序固定，不接受 sort/order）。
type snapshotQuery struct {
	Limit  int
	Cursor *snapshotCursor
}

func parseSnapshotQuery(v url.Values, withGranularity bool) (snapshotQuery, error) {
	q := snapshotQuery{Limit: SnapshotDefaultLimit}
	if raw := strings.TrimSpace(v.Get("sort")); raw != "" {
		return q, invalidQuery("快照游标页按 segment_start 倒序固定，不接受 sort")
	}
	if raw := strings.TrimSpace(v.Get("order")); raw != "" {
		return q, invalidQuery("快照游标页按 segment_start 倒序固定，不接受 order")
	}
	if raw := strings.TrimSpace(v.Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return q, invalidQuery("limit 不是整数: %q", raw)
		}
		if n < 1 {
			n = SnapshotDefaultLimit
		}
		if n > SnapshotMaxLimit {
			n = SnapshotMaxLimit
		}
		q.Limit = n
	}
	if raw := strings.TrimSpace(v.Get("cursor")); raw != "" {
		cur, err := decodeSnapshotCursor(raw, withGranularity)
		if err != nil {
			return q, invalidQuery("cursor 非法: %v", err)
		}
		q.Cursor = &cur
	}
	return q, nil
}

// buildSnapshotQuery 生成游标页 SQL：筛选复用 QueryState 的白名单 where，
// 排序固定为 segment_start desc + 自然键升序（与 0011 的索引列顺序一致）。
//
// 游标的"下一页"条件按混合方向展开，而不是行比较 `(a,b) < (x,y)`：
// 排序是 (segment_start DESC, terminal_id ASC, item_code ASC)，行比较只适用于同向排序。
func buildSnapshotQuery(e Entity, qs QueryState, q snapshotQuery, withGranularity bool) (string, []any, error) {
	where, args, err := buildWhere(e, qs)
	if err != nil {
		return "", nil, err
	}
	var conds []string
	if where != "" {
		conds = append(conds, "("+where+")")
	}
	if q.Cursor != nil {
		seg := fmt.Sprintf("$%d", len(args)+1)
		term := fmt.Sprintf("$%d", len(args)+2)
		item := fmt.Sprintf("$%d", len(args)+3)
		args = append(args, q.Cursor.SegmentStart, q.Cursor.TerminalID, q.Cursor.ItemCode)
		expr := fmt.Sprintf(
			"(segment_start < %s or (segment_start = %s and terminal_id > %s) or (segment_start = %s and terminal_id = %s and item_code > %s)",
			seg, seg, term, seg, term, item)
		if withGranularity {
			gran := fmt.Sprintf("$%d", len(args)+1)
			args = append(args, q.Cursor.Granularity)
			expr = fmt.Sprintf(
				"(segment_start < %s or (segment_start = %s and terminal_id > %s) or (segment_start = %s and terminal_id = %s and item_code > %s) or (segment_start = %s and terminal_id = %s and item_code = %s and granularity > %s))",
				seg, seg, term, seg, term, item, seg, term, item, gran)
		} else {
			expr += ")"
		}
		conds = append(conds, expr)
	}

	var sb strings.Builder
	sb.WriteString("select " + strings.Join(e.Cols, ", ") + " from " + e.Table)
	if len(conds) > 0 {
		sb.WriteString(" where " + strings.Join(conds, " and "))
	}
	if withGranularity {
		sb.WriteString(" order by segment_start desc, terminal_id, item_code, granularity")
	} else {
		sb.WriteString(" order by segment_start desc, terminal_id, item_code")
	}
	args = append(args, q.Limit+1) // 多取一行判 hasMore
	fmt.Fprintf(&sb, " limit $%d", len(args))
	return sb.String(), args, nil
}

// SnapshotCursorMeta 是游标页的 meta（与 offset 列表的 meta 同层级）。
//
// 信封沿用全站约定：data 直接是行数组，分页信息进 meta。前端 fetchCursorPage 读的就是
// `payload.data`（数组）+ `payload.meta.nextCursor/hasMore/limit`——这与已下线 changes 端点、
// 以及 offset 列表的 `{data: rows[], meta:{...}}` 完全一致。
// 空结果（例如保留期把老分区 DROP 了）就是空数组 + hasMore=false，不是错误。
type SnapshotCursorMeta struct {
	Limit      int    `json:"limit"`
	HasMore    bool   `json:"hasMore"`
	NextCursor string `json:"nextCursor,omitempty"`
}

// snapshotCursorResponse 是游标页的响应信封（data=行数组，meta=分页信息）。
type snapshotCursorResponse struct {
	Code    int                `json:"code"`
	Message string             `json:"message"`
	Data    []map[string]any   `json:"data"`
	Meta    SnapshotCursorMeta `json:"meta"`
}

// writeSnapshotList 处理 snapshot-* 三个只读 slot 的游标分页请求。
func (a *API) writeSnapshotList(w http.ResponseWriter, r *http.Request, page ReadonlyPage) {
	withGranularity := false // 粗表查询改走模板化 snapshot-query，剩余游标页只有分钟档
	qs, err := ParseQueryState(page.Entity, r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	sq, err := parseSnapshotQuery(r.URL.Query(), withGranularity)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	sql, args, err := buildSnapshotQuery(page.Entity, qs, sq, withGranularity)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	rows, err := a.Pool.Query(r.Context(), sql, args...)
	if err != nil {
		writeDBError(w, err)
		return
	}
	items, err := collect(rows)
	if err != nil {
		writeDBError(w, err)
		return
	}

	hasMore := len(items) > sq.Limit
	if hasMore {
		items = items[:sq.Limit]
	}
	if items == nil {
		items = []map[string]any{}
	}
	out := snapshotCursorResponse{
		Code:    0,
		Message: "ok",
		Data:    items,
		Meta:    SnapshotCursorMeta{Limit: sq.Limit, HasMore: hasMore},
	}
	if hasMore {
		if cur, ok := cursorOf(items[len(items)-1], withGranularity); ok {
			out.Meta.NextCursor = encodeSnapshotCursor(cur, withGranularity)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// cursorOf 从一行的 JSON 视图里取回游标键（collect 把时间转成了 RFC3339Nano 字符串）。
func cursorOf(row map[string]any, withGranularity bool) (snapshotCursor, bool) {
	seg, ok := row["segment_start"].(string)
	if !ok {
		return snapshotCursor{}, false
	}
	at, err := time.Parse(time.RFC3339Nano, seg)
	if err != nil {
		return snapshotCursor{}, false
	}
	terminal, _ := row["terminal_id"].(string)
	item, _ := row["item_code"].(string)
	cur := snapshotCursor{SegmentStart: at, TerminalID: terminal, ItemCode: item}
	if withGranularity {
		cur.Granularity, _ = row["granularity"].(string)
	}
	return cur, true
}
