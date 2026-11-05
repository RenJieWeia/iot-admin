package admin

import (
	"context"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
)

// 关联下拉/筛选选项：GET /admin/v1/options/{entity} → {"data":[{"value":"7","label":"泵站"}]}。
// 只暴露 id→label，不泄露整行；上限 MaxOptionRows 行，避免下拉一次拉全表。

// MaxOptionRows 是选项接口的行数上限（与 task-004 §4 一致）。
const MaxOptionRows = 1000

// OptionItem 是选项接口的 data 元素；value 一律是 id 的文本形态。
type OptionItem struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// options 返回某实体的 id→label 选项；未知实体 404（异常只读页不在此接口内）。
func (a *API) options(w http.ResponseWriter, r *http.Request) {
	e, ok := lookup(r.PathValue("entity"))
	if !ok {
		writeError(w, http.StatusNotFound, "unknown_entity")
		return
	}
	items, err := listOptions(r.Context(), a.Pool, e)
	if err != nil {
		writeDBError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, Response{Code: 0, Message: "ok", Data: items})
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
