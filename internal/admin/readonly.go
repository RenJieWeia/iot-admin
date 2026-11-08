package admin

import "net/http"

// 只读数据源页（R-022 读路径）：与异常两页同一套机制——不进 admin.Entities，
// 走独立的 /admin/v1/readonly/{slot} 路由。
//
// 只读源：
//   - latest-values    latest_value      offset 分页（行数受「终端 × 数据项」约束）
//   - snapshot-minute  snapshot_minute   游标分页
//
// 时/日/月/年不再提供原始只读页：统一走「快照查询」模板（见 snapshot_query.go）。
// 快照表是分区表：count(*) 会随存活分区数退化，因此只做游标分页、不报总数；
// 保留期把老分区 DROP 之后查不到数据 = 正常空结果（不是错误）。
//
// 分页方式由 Pagination 显式声明并随 _meta.readonly[].pagination 下发。
type ReadonlyPage struct {
	Slot       string
	Entity     Entity
	Pagination string
}

// 分页方式闭集。
const (
	PaginationOffset = "offset"
	PaginationCursor = "cursor"
)

// 快照只读页的 URL 段（粗表查询已由模板化「快照查询」取代）。
const SlotSnapshotMinute = "snapshot-minute"

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
		Slot: SlotSnapshotMinute,
		Entity: Entity{
			Name:  "snapshot-minute",
			Table: "snapshot_minute",
			Cols: []string{
				"terminal_id", "item_code", "segment_start",
				"value_num", "value_text", "unit", "value_time", "quality", "quality_reason",
				"revision", "updated_at",
			},
		},
		Pagination: PaginationCursor,
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

// readonlyList 处理 /admin/v1/readonly/{slot}；未知 slot → 404；
// 分页方式由页面定义决定：offset 走通用列表，cursor 走快照游标页。
func (a *API) readonlyList(w http.ResponseWriter, r *http.Request) {
	page, ok := ReadonlyPageBySlot(r.PathValue("slot"))
	if !ok {
		writeError(w, http.StatusNotFound, "unknown_slot")
		return
	}
	if page.Pagination == PaginationCursor {
		a.writeSnapshotList(w, r, page)
		return
	}
	a.writeList(w, r, page.Entity)
}
