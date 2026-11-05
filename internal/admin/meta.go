package admin

import "net/http"

// _meta 是前端字段元数据契约的唯一来源：实体/异常页的中文名、字段全集、枚举中文与 Tone、
// 关系目标实体、分页档位全部由此下发，前端不硬编码任何业务词。
//
// 契约（JSON）：
//
//	{"code":0,"message":"ok","data":{
//	  "entities":[{"name","label","route","readOnly","defaultSort","defaultOrder","fields":[MetaField]}],
//	  "exceptions":[{"slot","name","label","route","readOnly","defaultSort","defaultOrder","fields":[MetaField]}],
//	  "pagination":{"default":10,"max":50,"options":[10,20,50]}}}
//
// MetaField 是 FieldMeta 全字段的 JSON 投影：operators/options 永远是数组（不出现 null），
// relation 只出现在有关联目标的字段上（omitempty）。每字段另带 operatorLabels（操作符 → 中文），
// 前端筛选标签直接取用，不必自己维护中文映射。

// 实体与异常页的中文名：_meta 下发，上层复用同一份，避免同一文案散落多处。
var entityLabels = map[string]string{
	"data-domains":           "数据域",
	"device-types":           "终端类型",
	"thing-model-properties": "物模型属性",
	"terminals":              "终端",
	"quality-rules":          "质量规则",
	"settle-configs":         "结算窗口",
	"exception-events":       "异常事件",
	"exception-intervals":    "异常区间",
}

// EntityLabel 返回实体或异常页的中文名；未知实体原样返回名字，不吞掉新实体。
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
	OptionsSource  string            `json:"optionsSource"`
	Options        []MetaOption      `json:"options"`
	AdvancedOnly   bool              `json:"advancedOnly"`
	WidthStrategy  string            `json:"widthStrategy"`
	Ref            *MetaRef          `json:"ref,omitempty"`
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

// MetaException 是一张异常只读页的元数据；slot 是 URL 段（events|intervals）。
type MetaException struct {
	Slot         string      `json:"slot"`
	Name         string      `json:"name"`
	Title        string      `json:"title"`
	Route        string      `json:"route"`
	ReadOnly     bool        `json:"readOnly"`
	DefaultSort  string      `json:"defaultSort"`
	DefaultOrder string      `json:"defaultOrder"`
	Fields       []MetaField `json:"fields"`
}

// MetaPagination 下发分页档位，前端不硬编码 10/20/50。
type MetaPagination struct {
	Default int   `json:"default"`
	Max     int   `json:"max"`
	Options []int `json:"options"`
}

// MetaPayload 是 GET /admin/v1/_meta 的 data。
// enums / booleans 是静态字典（枚举值 → 中文），前端筛选与状态文案直接取用。
type MetaPayload struct {
	Entities   []MetaEntity                 `json:"entities"`
	Exceptions []MetaException              `json:"exceptions"`
	Enums      map[string]map[string]string `json:"enums"`
	Booleans   map[string]string            `json:"booleans"`
	Pagination MetaPagination               `json:"pagination"`
}

// meta 返回全部实体与异常页的字段元数据；不访问数据库，nil pool 也可用。
func (a *API) meta(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, Response{Code: 0, Message: "ok", Data: BuildMeta()})
}

// BuildMeta 组装字段元数据全集（导出以便用例与上层复用）。
func BuildMeta() MetaPayload {
	payload := MetaPayload{
		Entities:   make([]MetaEntity, 0, len(Entities)),
		Exceptions: make([]MetaException, 0, len(exceptionPages)),
		Enums:      enumDictionaries(),
		Booleans:   optionLabels(boolOptions),
		Pagination: MetaPagination{Default: DefaultPageSize, Max: MaxPageSize, Options: PageSizeOptions},
	}
	for _, e := range Entities {
		payload.Entities = append(payload.Entities, MetaEntity{
			Name:         e.Name,
			Title:        EntityLabel(e.Name),
			Route:        "/admin/v1/" + e.Name,
			ReadOnly:     false,
			DefaultSort:  defaultSortOf(e.Name),
			DefaultOrder: "desc",
			Fields:       metaFields(e.Name),
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

// enumDictionaries 下发静态枚举字典（value_type / rule_type），文案仍以 fields.go 为准。
func enumDictionaries() map[string]map[string]string {
	return map[string]map[string]string{
		"value_type": optionLabels(valueTypeOptions),
		"rule_type":  optionLabels(ruleTypeOptions),
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
