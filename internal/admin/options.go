package admin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgxpool"
)

// 关联下拉/筛选选项：GET /admin/v1/options/{entity} → {"data":[{"value":"7","label":"泵站"}]}。
// 只暴露 id→label，不泄露整行。
//
// 三种取法（F4）：
//   - ids=1,2,3   按 id 精确取 label（表格列回填），≤MaxOptionIDs 个；忽略 q/limit；
//     不存在的 id 直接跳过；返回顺序与请求顺序一致
//   - q=关键词    label ilike 模糊搜索（下拉搜索），≤MaxOptionQueryChars 字符；
//     limit 默认 DefaultOptionSearchLimit、上限 MaxOptionRows
//   - 无参        全量，上限 MaxOptionRows（兼容既有调用）
//
// ids 与 q 互斥，同时出现 → 400 invalid_query。q 为空串等同无参。

const (
	// MaxOptionRows 是选项接口的行数上限（全量 / q 搜索共用）。
	MaxOptionRows = 1000
	// MaxOptionIDs 是 ids 参数的 id 个数上限。
	MaxOptionIDs = 200
	// MaxOptionQueryChars 是 q 参数的关键词长度上限（按字符计，中文按 1 个算）。
	MaxOptionQueryChars = 200
	// DefaultOptionSearchLimit 是 q 搜索的默认条数。
	DefaultOptionSearchLimit = 50
)

// OptionItem 是选项接口的 data 元素；value 一律是 id 的文本形态。
type OptionItem struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// optionQuery 是解析后的选项查询参数（三选一）。
type optionQuery struct {
	IDs   []string // 非空表示按 id 精确取
	Q     string   // 非空表示模糊搜索
	Limit int      // 仅 Q 生效
}

// options 返回某实体的 id→label 选项；未知实体 404（异常只读页不在此接口内）。
func (a *API) options(w http.ResponseWriter, r *http.Request) {
	e, ok := lookup(r.PathValue("entity"))
	if !ok {
		writeError(w, http.StatusNotFound, "unknown_entity")
		return
	}
	q, err := parseOptionQuery(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	items, err := runOptionQuery(r.Context(), a.Pool, e, q)
	if err != nil {
		var qerr *QueryError
		if errors.As(err, &qerr) {
			writeError(w, http.StatusBadRequest, qerr.Error())
			return
		}
		writeDBError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, Response{Code: 0, Message: "ok", Data: items})
}

// parseOptionQuery 解析并校验参数；ids 与 q 互斥，超限一律 400 invalid_query。
func parseOptionQuery(v url.Values) (optionQuery, error) {
	rawIDs := strings.TrimSpace(v.Get("ids"))
	rawQ := strings.TrimSpace(v.Get("q"))
	q := optionQuery{Limit: DefaultOptionSearchLimit}

	if rawIDs != "" && rawQ != "" {
		return q, invalidQuery("ids 与 q 互斥，不能同时使用")
	}

	if rawIDs != "" {
		ids := make([]string, 0, 8)
		seen := make(map[string]struct{}, 8)
		for _, part := range strings.Split(rawIDs, ",") {
			id := strings.TrimSpace(part)
			if id == "" {
				continue
			}
			if _, dup := seen[id]; dup {
				continue // 去重，保持首次出现的顺序
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
		if len(ids) == 0 {
			return q, invalidQuery("ids 不能为空")
		}
		if len(ids) > MaxOptionIDs {
			return q, invalidQuery("ids 最多 %d 个，实际 %d 个", MaxOptionIDs, len(ids))
		}
		q.IDs = ids
		return q, nil
	}

	if rawQ != "" {
		if n := utf8.RuneCountInString(rawQ); n > MaxOptionQueryChars {
			return q, invalidQuery("q 最多 %d 个字符，实际 %d 个", MaxOptionQueryChars, n)
		}
		q.Q = rawQ
		if raw := strings.TrimSpace(v.Get("limit")); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil {
				return q, invalidQuery("limit 不是整数: %q", raw)
			}
			if n < 1 {
				n = DefaultOptionSearchLimit
			}
			if n > MaxOptionRows {
				n = MaxOptionRows // 静默收敛到上限
			}
			q.Limit = n
		}
		return q, nil
	}
	// 无参（含 q= 空串）：全量，limit 参数不生效。
	q.Limit = MaxOptionRows
	return q, nil
}

// runOptionQuery 按参数分派三种取法。
func runOptionQuery(ctx context.Context, pool *pgxpool.Pool, e Entity, q optionQuery) ([]OptionItem, error) {
	switch {
	case len(q.IDs) > 0:
		return listOptionsByIDs(ctx, pool, e, q.IDs)
	case q.Q != "":
		return searchOptions(ctx, pool, e, q.Q, q.Limit)
	default:
		return listOptions(ctx, pool, e)
	}
}

// listOptionsByIDs 按 id 精确取；SQL 只按 id 过滤，返回顺序在 Go 侧按请求顺序重排。
func listOptionsByIDs(ctx context.Context, pool *pgxpool.Pool, e Entity, ids []string) ([]OptionItem, error) {
	query := fmt.Sprintf("select id::text as value, %s as label from %s where id::text = any($1)",
		optionLabelExpr(e), e.Table)
	rows, err := pool.Query(ctx, query, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byValue := make(map[string]OptionItem, len(ids))
	for rows.Next() {
		var it OptionItem
		if err := rows.Scan(&it.Value, &it.Label); err != nil {
			return nil, err
		}
		byValue[it.Value] = it
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// 保持请求顺序；不存在的 id 直接跳过。
	items := make([]OptionItem, 0, len(ids))
	for _, id := range ids {
		if it, ok := byValue[id]; ok {
			items = append(items, it)
		}
	}
	return items, nil
}

// searchOptions 按 label 做 ilike 模糊搜索（% _ \ 已转义），按 label 升序。
func searchOptions(ctx context.Context, pool *pgxpool.Pool, e Entity, keyword string, limit int) ([]OptionItem, error) {
	query := fmt.Sprintf("select id::text as value, %s as label from %s where %s ilike $1 order by label asc, id asc limit $2",
		optionLabelExpr(e), e.Table, optionLabelExpr(e))
	rows, err := pool.Query(ctx, query, "%"+escapeLike(keyword)+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]OptionItem, 0, limit)
	for rows.Next() {
		var it OptionItem
		if err := rows.Scan(&it.Value, &it.Label); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

// listOptions 取前 MaxOptionRows 行，按 label 升序；标签取 name → code → id 的降级链。
func listOptions(ctx context.Context, pool *pgxpool.Pool, e Entity) ([]OptionItem, error) {
	query := fmt.Sprintf("select id::text as value, %s as label from %s order by label asc, id asc limit $1",
		optionLabelExpr(e), e.Table)
	rows, err := pool.Query(ctx, query, MaxOptionRows)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]OptionItem, 0, MaxOptionRows)
	for rows.Next() {
		var it OptionItem
		if err := rows.Scan(&it.Value, &it.Label); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

// optionLabelExpr 按列白名单拼展示列表达式：name 优先，其次 code，最后回退 id。
// 只用到 Entity.Cols 里真实存在的列，不做运行时反射。
func optionLabelExpr(e Entity) string {
	hasName := hasColumn(e, "name")
	hasCode := hasColumn(e, "code")
	switch {
	case hasName && hasCode:
		return "coalesce(nullif(name::text, ''), nullif(code::text, ''), id::text)"
	case hasName:
		return "coalesce(nullif(name::text, ''), id::text)"
	case hasCode:
		return "coalesce(nullif(code::text, ''), id::text)"
	default:
		return "id::text"
	}
}
