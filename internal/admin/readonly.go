package admin

import "net/http"

// 只读数据源页（R-022 读路径）：与异常两页同一套机制——不进 admin.Entities，
// 走独立的 /admin/v1/readonly/{slot} 路由。
//
// 小时级历史下线后（迁移 0009 删除 hourly_snapshot / hourly_staging），只读源只剩一项：
//   - latest-values    latest_value     offset 分页（行数受「终端 × 数据项」约束）
//
// 分页方式由 Pagination 显式声明并随 _meta.readonly[].pagination 下发。
type ReadonlyPage struct {
	Slot       string
	Entity     Entity
	Pagination string
}

// 分页方式闭集：只剩 offset（游标分页随小时快照页一并下线）。
const PaginationOffset = "offset"

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

// readonlyList 处理 /admin/v1/readonly/{slot}；未知 slot → 404，其余走通用列表（offset 分页）。
func (a *API) readonlyList(w http.ResponseWriter, r *http.Request) {
	page, ok := ReadonlyPageBySlot(r.PathValue("slot"))
	if !ok {
		writeError(w, http.StatusNotFound, "unknown_slot")
		return
	}
	a.writeList(w, r, page.Entity)
}
