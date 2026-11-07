package admin

import "net/http"

// _meta 是前端字段元数据契约的唯一来源：实体/异常页的中文名、字段全集、枚举中文与 Tone、
// 关系目标实体、分页档位全部由此下发，前端不硬编码任何业务词。
//
// 契约（JSON）：
//
//	{"code":0,"message":"ok","data":{
//	  "entities":[{"name","label","route","readOnly","defaultSort","defaultOrder","fields":[MetaField]}],
//	  "exceptions":[{"slot","name","title","route","readOnly","defaultSort","defaultOrder","fields":[MetaField]}],
//	  "readonly":[…同 exceptions 形状…],
//	  "pagination":{"default":10,"max":50,"options":[10,20,50]}}}
//
// MetaField 是 FieldMeta 全字段的 JSON 投影：operators/options 永远是数组（不出现 null），
// relation 只出现在有关联目标的字段上（omitempty）。每字段另带 operatorLabels（操作符 → 中文），
// 前端筛选标签直接取用，不必自己维护中文映射。

// 实体与异常页的中文名：_meta 下发，上层复用同一份，避免同一文案散落多处。
var entityLabels = map[string]string{
	"data-domains":           "数据域",
	"device-types":           "终端类型",
	"thing-model-properties": "终端属性",
	"terminals":              "终端",
	"quality-rules":          "质量规则",
	"snapshot-slots":         "快照计划",
	"snapshot-bindings":      "快照绑定",
	"snapshot-settings":      "存储与保留",
	"exception-events":       "异常事件",
	"latest-values":          "最新值",
	// 三个快照存储：导航里由前端按 snapshot- 前缀合并成一项「快照」，
	// 页面内的「存储」切换用这里的标题（粗表要连档位一起说清，否则用户不知道能筛档）。
	"snapshot-coarse": "快照 · 时/日/月/年",
	"snapshot-minute": "快照 · 分钟",
	"snapshot-second": "快照 · 秒",
}

// EntityLabel 返回实体或只读页的中文名；未知名字原样返回，不吞掉新实体。
func EntityLabel(name string) string {
	if label, ok := entityLabels[name]; ok {
		return label
	}
	return name
}

// PageSizeOptions 是分页选择器可选的每页条数（与 DefaultPageSize / MaxPageSize 同源）。
var PageSizeOptions = []int{10, 20, 50}

// MetaOption 是枚举取值的 JSON 形态（中文标签 + 状态色语义）。
type MetaOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
	Tone  string `json:"tone"`
}

// MetaRef 是关联字段的目标实体与取值/标签列（前端据此拼 options 请求与展示）。
type MetaRef struct {
	Entity   string `json:"entity"`
	ValueKey string `json:"valueKey"`
	LabelKey string `json:"labelKey"`
}

// MetaField 是 FieldMeta 的全字段 JSON 契约。
type MetaField struct {
	Key            string            `json:"key"`
	Label          string            `json:"label"`
	Type           string            `json:"type"`
	Visible        bool              `json:"visible"`
	Sortable       bool              `json:"sortable"`
	Filterable     bool              `json:"filterable"`
	FilterType     string            `json:"filterType"`
	Operators      []string          `json:"operators"`
	OperatorLabels map[string]string `json:"operatorLabels"`
	// Derived 表示该列由服务端列表 SQL 计算（不在物理表里），前端只展示、不可排序/筛选。
	Derived       bool         `json:"derived,omitempty"`
	OptionsSource string       `json:"optionsSource"`
	Options       []MetaOption `json:"options"`
	AdvancedOnly  bool         `json:"advancedOnly"`
	WidthStrategy string       `json:"widthStrategy"`
	// Width 是列宽的推导结论（px）：字段显式覆写优先，否则由 Type 推导（widthForType）。
	// 0 表示不推导（json/未知类型），前端回落到 widthStrategy。
	Width int `json:"width,omitempty"`
	// Editable / Required 是抽屉表单契约：前端不再靠 id && filterable 推断可编辑列。
	Editable bool     `json:"editable"`
	Required bool     `json:"required"`
	Ref      *MetaRef `json:"ref,omitempty"`
	// Relation 是 ref.entity 的兼容别名（R-019 首版键名），新代码用 ref。
	Relation string `json:"relation,omitempty"`
}

// MetaEntity 是一个可 CRUD 实体的元数据；title 是中文名（task-004 §4 契约键名）。
type MetaEntity struct {
	Name         string      `json:"name"`
	Title        string      `json:"title"`
	Route        string      `json:"route"`
	ReadOnly     bool        `json:"readOnly"`
	DefaultSort  string      `json:"defaultSort"`
	DefaultOrder string      `json:"defaultOrder"`
	Fields       []MetaField `json:"fields"`
}

// MetaReadonly 是一张只读页（异常两页 + 通用只读源）的元数据；slot 是 URL 段。
//
// 说明：`exceptions` 段是 `readonly` 的特例（异常页也走 readonlyPages 那套机制），
// 但契约上保留两个独立键，前端已在用 `exceptions`，本期不动、后续可合并。
type MetaReadonly struct {
	Slot         string `json:"slot"`
	Name         string `json:"name"`
	Title        string `json:"title"`
	Route        string `json:"route"`
	ReadOnly     bool   `json:"readOnly"`
	DefaultSort  string `json:"defaultSort"`
	DefaultOrder string `json:"defaultOrder"`
	// Pagination 是只读页的分页方式（offset|cursor），前端据此选择分页器。
	Pagination string      `json:"pagination,omitempty"`
	Fields     []MetaField `json:"fields"`
}

// MetaException 是 MetaReadonly 的别名（历史键名保持可读，JSON 形状一致）。
type MetaException = MetaReadonly

// MetaPagination 下发分页档位，前端不硬编码 10/20/50。
type MetaPagination struct {
	Default int   `json:"default"`
	Max     int   `json:"max"`
	Options []int `json:"options"`
}

// MetaPayload 是 GET /admin/v1/_meta 的 data。
// enums / booleans 是静态字典（枚举值 → 中文），前端筛选与状态文案直接取用。
type MetaPayload struct {
	Entities []MetaEntity `json:"entities"`
	// Subresources 是「可 CRUD 但不可导航」实体的字段元数据，形状与 entities[] 完全一致。
	// **它不是导航来源**：只是为了在实体不进导航后，详情页仍能拿到列定义/筛选能力/必填。
	Subresources []MetaEntity                 `json:"subresources"`
	Exceptions   []MetaException              `json:"exceptions"`
	Readonly     []MetaReadonly               `json:"readonly"`
	Enums        map[string]map[string]string `json:"enums"`
	Booleans     map[string]string            `json:"booleans"`
	Pagination   MetaPagination               `json:"pagination"`
}

// meta 返回全部实体与异常页的字段元数据；不访问数据库，nil pool 也可用。
func (a *API) meta(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, Response{Code: 0, Message: "ok", Data: BuildMeta()})
}

// derivedWidth 是「列宽结论」的唯一来源：
//
//   - 字段显式 Width 最高优先（连 flex 也能覆盖）；
//   - widthStrategy=flex 的字段**不推导**：那一档的语义就是「吸收剩余空间」，
//     给它一个固定 px 会把 flex 变成 fixed，整张表再没有列能吃掉多余宽度；
//   - 其余字段按类型推导（widthForType）。
func derivedWidth(f FieldMeta) int {
	if f.Width != 0 {
		return f.Width
	}
	if f.WidthStrategy == "flex" {
		return 0
	}
	return widthForType(f.Type)
}

// metaEntityOf 把 Entity 投影成 _meta 的实体元数据（entities[] 与 subresources[] 共用）。
func metaEntityOf(e Entity) MetaEntity {
	return MetaEntity{
		Name:         e.Name,
		Title:        EntityLabel(e.Name),
		Route:        "/admin/v1/" + e.Name,
		ReadOnly:     false,
		DefaultSort:  defaultSortOf(e.Name),
		DefaultOrder: "desc",
		Fields:       metaFields(e.Name),
	}
}

// BuildMeta 组装字段元数据全集（导出以便用例与上层复用）。
func BuildMeta() MetaPayload {
	payload := MetaPayload{
		Entities:     make([]MetaEntity, 0, len(Entities)),
		Subresources: make([]MetaEntity, 0, 1),
		Exceptions:   make([]MetaException, 0, len(exceptionPages)),
		Readonly:     make([]MetaReadonly, 0, len(readonlyPages)),
		Enums:        enumDictionaries(),
		Booleans:     optionLabels(boolOptions),
		Pagination:   MetaPagination{Default: DefaultPageSize, Max: MaxPageSize, Options: PageSizeOptions},
	}
	for _, e := range Entities {
		// 导航实体进 entities；Hidden 的进 subresources（同一份字段元数据，两个出口）。
		if e.Hidden {
			payload.Subresources = append(payload.Subresources, metaEntityOf(e))
			continue
		}
		payload.Entities = append(payload.Entities, metaEntityOf(e))
	}
	for _, page := range readonlyPages {
		payload.Readonly = append(payload.Readonly, MetaReadonly{
			Slot:         page.Slot,
			Name:         page.Entity.Name,
			Title:        EntityLabel(page.Entity.Name),
			Route:        "/admin/v1/readonly/" + page.Slot,
			ReadOnly:     true,
			DefaultSort:  defaultSortOf(page.Entity.Name),
			DefaultOrder: "desc",
			Pagination:   page.Pagination,
			Fields:       metaFields(page.Entity.Name),
		})
	}
	for _, page := range exceptionPages {
		payload.Exceptions = append(payload.Exceptions, MetaException{
			Slot:         page.Slot,
			Name:         page.Entity.Name,
			Title:        EntityLabel(page.Entity.Name),
			Route:        "/admin/v1/exceptions/" + page.Slot,
			ReadOnly:     true,
			DefaultSort:  defaultSortOf(page.Entity.Name),
			DefaultOrder: "desc",
			Fields:       metaFields(page.Entity.Name),
		})
	}
	return payload
}

// defaultSortOf 与列表默认排序同一口径（没有 id 列的异常事件按时间列倒序）。
func defaultSortOf(entity string) string {
	fields, ok := FieldMetas(entity)
	if !ok {
		return ""
	}
	return defaultSort(fields)
}

// labelKeyOf 是关联下拉的展示列：目标实体有 name 用 name，其次 code，最后回退 id。
func labelKeyOf(entity string) string {
	if _, ok := FieldMetaOf(entity, "name"); ok {
		return "name"
	}
	if _, ok := FieldMetaOf(entity, "code"); ok {
		return "code"
	}
	return "id"
}

// enumDictionaries 下发静态枚举字典（value_type / rule_type / 六档步长），文案仍以 fields.go 为准。
// 步长字典的 key 与字段名一致（second_step…year_step），前端下拉直接按字段名取。
func enumDictionaries() map[string]map[string]string {
	return map[string]map[string]string{
		"value_type":  optionLabels(valueTypeOptions),
		"rule_type":   optionLabels(ruleTypeOptions),
		"second_step": optionLabels(secondStepOptions),
		"minute_step": optionLabels(minuteStepOptions),
		"hour_step":   optionLabels(hourStepOptions),
		"day_step":    optionLabels(dayStepOptions),
		"month_step":  optionLabels(monthStepOptions),
		"year_step":   optionLabels(yearStepOptions),
	}
}

// optionLabels 把 Option 列表压成 值 → 中文 的字典。
func optionLabels(options []Option) map[string]string {
	out := make(map[string]string, len(options))
	for _, o := range options {
		out[o.Value] = o.Label
	}
	return out
}

// metaFields 把 FieldMeta 投影成 JSON 契约；nil 切片归一成空数组，前端拿到的永远是数组。
func metaFields(entity string) []MetaField {
	fields, ok := FieldMetas(entity)
	if !ok {
		return []MetaField{}
	}
	out := make([]MetaField, 0, len(fields))
	for _, f := range fields {
		mf := MetaField{
			Key:            f.Key,
			Label:          f.Label,
			Type:           f.Type,
			Visible:        f.Visible,
			Sortable:       f.Sortable,
			Filterable:     f.Filterable,
			FilterType:     f.FilterType,
			Operators:      make([]string, 0, len(f.Operators)),
			OperatorLabels: make(map[string]string, len(f.Operators)),
			OptionsSource:  f.OptionsSource,
			Options:        make([]MetaOption, 0, len(f.Options)),
			AdvancedOnly:   f.AdvancedOnly,
			WidthStrategy:  f.WidthStrategy,
			Width:          derivedWidth(f),
			Editable:       f.Editable,
			Required:       f.Required,
			Derived:        f.Derived,
		}
		for _, op := range f.Operators {
			mf.Operators = append(mf.Operators, op)
			mf.OperatorLabels[op] = OperatorLabel(op)
		}
		for _, o := range f.Options {
			mf.Options = append(mf.Options, MetaOption{Value: o.Value, Label: o.Label, Tone: o.Tone})
		}
		if target, ok := RelationTarget(entity, f.Key); ok {
			mf.Ref = &MetaRef{Entity: target, ValueKey: "id", LabelKey: labelKeyOf(target)}
			mf.Relation = target
		}
		out = append(out, mf)
	}
	return out
}
