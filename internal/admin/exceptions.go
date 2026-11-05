package admin

import "net/http"

// 两张异常表只读：**不进 Entities**（管理 API 实体数固定为 6，nongoal_test 断言），
// 走独立的 /admin/v1/exceptions/{slot} 路由；字段元数据仍在 fields.go 的 fieldSets 里，
// 查询/分页/排序复用同一套 QueryState（q/sort/order/page/pageSize/filter.* + meta{total,...}）。

// ExceptionPage 描述一张异常只读页：Slot 是 URL 段，Entity 是查询用的实体定义。
// Entity.Name 与 fieldSets 的键一致（"exception-events" / "exception-intervals"）。
type ExceptionPage struct {
	Slot   string
	Entity Entity
}

var exceptionPages = []ExceptionPage{
	{
		Slot: "events",
		// 异常事件按 (terminal_id, hour_start, error_type) 汇总，没有单列主键：
		// 分页稳定靠 Entity.Cols 全列 tie-breaker 兜底（见 query.go 的 tieBreakers）。
		Entity: Entity{
			Name:  "exception-events",
			Table: "exception_count",
			Cols: []string{
				"terminal_id", "gateway_id", "hour_start", "error_type",
				"count", "sample_digest", "first_seen", "last_seen",
			},
		},
	},
	{
		Slot: "intervals",
		// period 是 tstzrange：外层先转 text 才能按 JSON 直接展示；筛选/排序仍走普通列。
		Entity: Entity{
			Name: "exception-intervals",
			Table: "(select id, terminal_id, gateway_id, item_code, error_type, period::text as period, " +
				"raw_count, created_at from exception_interval)",
			Cols: []string{
				"id", "terminal_id", "gateway_id", "item_code", "error_type",
				"period", "raw_count", "created_at",
			},
		},
	},
}

// ExceptionPages 返回异常只读页的固定顺序定义（events、intervals），供 _meta 与上层复用。
func ExceptionPages() []ExceptionPage {
	out := make([]ExceptionPage, len(exceptionPages))
	copy(out, exceptionPages)
	return out
}

// ExceptionBySlot 按 URL 段取异常只读实体；未知 slot 返回 false（调用方据此 404）。
func ExceptionBySlot(slot string) (Entity, bool) {
	for _, page := range exceptionPages {
		if page.Slot == slot {
			return page.Entity, true
		}
	}
	return Entity{}, false
}

// ExceptionByEntity 按实体名取异常只读实体（"exception-events" → exception_count）。
func ExceptionByEntity(name string) (Entity, bool) {
	for _, page := range exceptionPages {
		if page.Entity.Name == name {
			return page.Entity, true
		}
	}
	return Entity{}, false
}

// exceptionList 是异常只读列表：语义与实体列表完全一致，只是数据源固定、不接受写入。
func (a *API) exceptionList(w http.ResponseWriter, r *http.Request) {
	e, ok := ExceptionBySlot(r.PathValue("slot"))
	if !ok {
		writeError(w, http.StatusNotFound, "unknown_slot")
		return
	}
	a.writeList(w, r, e)
}
