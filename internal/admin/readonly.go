package admin

import "net/http"

// 只读数据源页（R-022 读路径）：与异常两页同一套机制——不进 admin.Entities（管理实体仍 6），
// 走独立的 /admin/v1/readonly/{slot} 路由，查询/分页/排序复用同一个 QueryState。
//
// 本期只开放 latest_value（最新值）：quality/quality_reason 只在这类运行时表里，
// hourly_staging / hourly_snapshot 留到下一批（快照是分区表、量大）。
//
// 结构复用 ExceptionPage：slot 是 URL 段，Entity 是查询用的实体定义（Name 必须与
// fields.go 的 fieldSets 键一致）。

var readonlyPages = []ExceptionPage{
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
	},
}

// ReadonlyPages 返回只读页的固定顺序定义，供 _meta 与上层复用。
func ReadonlyPages() []ExceptionPage {
	out := make([]ExceptionPage, len(readonlyPages))
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

// readonlyList 是只读数据源的列表：语义与实体列表一致，只是不接受写入。
func (a *API) readonlyList(w http.ResponseWriter, r *http.Request) {
	e, ok := ReadonlyBySlot(r.PathValue("slot"))
	if !ok {
		writeError(w, http.StatusNotFound, "unknown_slot")
		return
	}
	a.writeList(w, r, e)
}
