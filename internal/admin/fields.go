package admin

// 字段元数据是列表页与查询白名单的唯一来源：页面控件、SQL 白名单、排序/筛选能力、
// 中文标签与状态颜色全部从这里派生，页面与模板不硬编码控件或枚举文案。

// Option 是枚举/布尔字段的取值与中文标签；Tone 决定状态标签的颜色语义（文字始终保留）。
type Option struct {
	Value string
	Label string
	Tone  string // neutral | positive | negative | warning
}

// FieldMeta 描述一个可查询字段。Filterable 必须显式声明，不允许"没实现所以不可筛选"。
//
// Editable 与 Required 是写路径契约：
//   - Editable 不在本表里填值，由 FieldMetas 从 Entity.Cols（唯一可写白名单）推导，
//     避免同一事实存两份；异常只读页没有写路径，恒为 false。
//   - Required 依据 migrations/0001_datasource.sql：列 not null（或 primary key）且无 default
//     才为 true；可空或有默认值一律显式 false。例外只读页无写入，恒为 false。
type FieldMeta struct {
	Key           string
	Label         string
	Type          string // text|id|enum|number|boolean|relation|json|datetime
	Visible       bool
	Sortable      bool
	Filterable    bool
	Editable      bool     // 由 Entity.Cols 推导（见 FieldMetas）
	Required      bool     // not null（含 primary key）且无 default
	FilterType    string   // text|multiSelect|numberRange|boolean|relation|none
	Operators     []string // contains|eq|prefix|in|gte|lte
	OptionsSource string   // dict|remote|static
	Options       []Option
	AdvancedOnly  bool
	WidthStrategy string // fixed|min|flex
}

var boolOptions = []Option{
	{Value: "true", Label: "是", Tone: "positive"},
	{Value: "false", Label: "否", Tone: "neutral"},
}

var enabledOptions = []Option{
	{Value: "true", Label: "启用", Tone: "positive"},
	{Value: "false", Label: "停用", Tone: "negative"},
}

var valueTypeOptions = []Option{
	{Value: "number", Label: "数值", Tone: "neutral"},
	{Value: "string", Label: "文本", Tone: "neutral"},
	{Value: "bool", Label: "布尔", Tone: "neutral"},
}

var ruleTypeOptions = []Option{
	{Value: "range", Label: "范围", Tone: "neutral"},
	{Value: "enum", Label: "枚举", Tone: "neutral"},
	{Value: "precision", Label: "精度", Tone: "neutral"},
	{Value: "unit_whitelist", Label: "单位白名单", Tone: "neutral"},
	{Value: "not_null", Label: "非空", Tone: "neutral"},
}

var eventTypeOptions = []Option{
	{Value: "upload_timeout", Label: "上报超时", Tone: "warning"},
	{Value: "protocol_error", Label: "协议错误", Tone: "negative"},
	{Value: "msg_incomplete", Label: "消息不完整", Tone: "warning"},
	{Value: "clean_failed", Label: "清洗失败", Tone: "warning"},
	{Value: "cache_write_failed", Label: "写入失败", Tone: "negative"},
	{Value: "mqtt_publish_failed", Label: "发布失败", Tone: "negative"},
	{Value: "snapshot_failed", Label: "快照失败", Tone: "negative"},
	{Value: "late_data", Label: "迟到数据", Tone: "warning"},
	{Value: "quality_violation", Label: "质量违规", Tone: "warning"},
}

// qualityOptions 是行级质量位掩码的 8 种组合（R-022 读路径）：0 正常，其余都是"有问题"。
// 位语义：1 物模型越界、2 规则命中、4 单位闭集外，可叠加（3 = 越界+规则，7 = 三者全中）。
var qualityOptions = []Option{
	{Value: "0", Label: "正常", Tone: "neutral"},
	{Value: "1", Label: "越界", Tone: "warning"},
	{Value: "2", Label: "规则命中", Tone: "warning"},
	{Value: "3", Label: "越界+规则", Tone: "warning"},
	{Value: "4", Label: "单位闭集外", Tone: "warning"},
	{Value: "5", Label: "越界+单位闭集外", Tone: "warning"},
	{Value: "6", Label: "规则命中+单位闭集外", Tone: "warning"},
	{Value: "7", Label: "越界+规则+单位闭集外", Tone: "warning"},
}

var intervalTypeOptions = []Option{
	{Value: "gateway_disconnected", Label: "网关断连", Tone: "negative"},
	{Value: "auth_failed", Label: "鉴权失败", Tone: "negative"},
	{Value: "item_silent", Label: "数据项沉默", Tone: "warning"},
}

// hourStartField 是运行时两页的归属小时列（staging/snapshot 共用）。
var hourStartField = FieldMeta{
	Key: "hour_start", Label: "归属小时", Type: "datetime", Visible: true, Sortable: true, Filterable: true,
	FilterType: "dateRange", Operators: []string{"gte", "lte"}, WidthStrategy: "min",
}

// revisionField 是快照页的修正次数（R-030）：> 0 即"被改写过的格子"。
var revisionField = FieldMeta{
	Key: "revision", Label: "修正次数", Type: "number", Visible: true, Sortable: true, Filterable: true,
	FilterType: "numberRange", Operators: []string{"gte", "lte"}, WidthStrategy: "fixed",
}

// settledAtField 是快照页的落库/修正时刻：游标分页的排序列，也是 changes 端点的 since 口径。
var settledAtField = FieldMeta{
	Key: "settled_at", Label: "落库时间", Type: "datetime", Visible: true, Sortable: true, Filterable: true,
	FilterType: "dateRange", Operators: []string{"gte", "lte"}, WidthStrategy: "min",
}

// runtimeCommonFields 返回运行时表（最新值/暂存/快照）共有的 8 列。
// 每次调用返回独立切片：调用方可以放心 append 自己的列而不污染其它页。
func runtimeCommonFields() []FieldMeta {
	return []FieldMeta{
		{Key: "terminal_id", Label: "终端", Type: "text", Visible: true, Sortable: true, Filterable: true,
			FilterType: "text", Operators: []string{"contains", "eq"}, WidthStrategy: "min"},
		{Key: "item_code", Label: "数据项", Type: "text", Visible: true, Sortable: true, Filterable: true,
			FilterType: "text", Operators: []string{"contains"}, WidthStrategy: "min"},
		{Key: "value_num", Label: "数值", Type: "number", Visible: true, Sortable: true, Filterable: true,
			FilterType: "numberRange", Operators: []string{"gte", "lte"}, AdvancedOnly: true, WidthStrategy: "min"},
		{Key: "value_text", Label: "文本值", Type: "text", Visible: true, Sortable: true, Filterable: true,
			FilterType: "text", Operators: []string{"contains"}, AdvancedOnly: true, WidthStrategy: "min"},
		{Key: "unit", Label: "单位", Type: "text", Visible: true, Sortable: true, Filterable: true,
			FilterType: "text", Operators: []string{"contains"}, AdvancedOnly: true, WidthStrategy: "fixed"},
		{Key: "value_time", Label: "采集时间", Type: "datetime", Visible: true, Sortable: true, Filterable: true,
			FilterType: "dateRange", Operators: []string{"gte", "lte"}, WidthStrategy: "min"},
		// quality 是位掩码：不新增 SQL 操作符，"仅看有问题"用 filter.quality.gte=1 表达。
		{Key: "quality", Label: "质量", Type: "number", Visible: true, Sortable: true, Filterable: true,
			FilterType: "numberRange", Operators: []string{"gte", "lte"}, OptionsSource: "static",
			Options: qualityOptions, WidthStrategy: "fixed"},
		{Key: "quality_reason", Label: "质量说明", Type: "text", Visible: true, Sortable: false, Filterable: true,
			FilterType: "text", Operators: []string{"contains"}, AdvancedOnly: true, WidthStrategy: "flex"},
	}
}

// prependFields 返回 lead + rest 的新切片（不用 append，避免共享底层数组）。
func prependFields(lead, rest []FieldMeta) []FieldMeta {
	out := make([]FieldMeta, 0, len(lead)+len(rest))
	out = append(out, lead...)
	out = append(out, rest...)
	return out
}

type fieldSet struct {
	fields []FieldMeta
	// relations 记录关联字段的目标实体，供列表页做名称回填。
	relations map[string]string
}

var fieldSets = map[string]fieldSet{
	"data-domains": {
		fields: []FieldMeta{
			{Key: "id", Label: "ID", Type: "id", Visible: true, Sortable: true, FilterType: "none", WidthStrategy: "fixed"},
			{Key: "code", Label: "编码", Type: "text", Visible: true, Sortable: true, Filterable: true, Required: true,
				FilterType: "text", Operators: []string{"contains", "eq"}, WidthStrategy: "min"},
			{Key: "name", Label: "名称", Type: "text", Visible: true, Sortable: true, Filterable: true, Required: true,
				FilterType: "text", Operators: []string{"contains"}, WidthStrategy: "flex"},
		},
	},
	"device-types": {
		fields: []FieldMeta{
			{Key: "id", Label: "ID", Type: "id", Visible: true, Sortable: true, FilterType: "none", WidthStrategy: "fixed"},
			{Key: "code", Label: "编码", Type: "text", Visible: true, Sortable: true, Filterable: true, Required: true,
				FilterType: "text", Operators: []string{"contains"}, WidthStrategy: "min"},
			{Key: "name", Label: "名称", Type: "text", Visible: true, Sortable: true, Filterable: true, Required: true,
				FilterType: "text", Operators: []string{"contains"}, WidthStrategy: "flex"},
			{Key: "domain_id", Label: "数据域", Type: "relation", Visible: true, Sortable: true, Filterable: true, Required: true,
				FilterType: "relation", Operators: []string{"in"}, OptionsSource: "remote", WidthStrategy: "min"},
		},
		relations: map[string]string{"domain_id": "data-domains"},
	},
	"thing-model-properties": {
		fields: []FieldMeta{
			{Key: "id", Label: "ID", Type: "id", Visible: true, Sortable: true, FilterType: "none", WidthStrategy: "fixed"},
			{Key: "type_id", Label: "终端类型", Type: "relation", Visible: true, Sortable: true, Filterable: true, Required: true,
				FilterType: "relation", Operators: []string{"in"}, OptionsSource: "remote", WidthStrategy: "min"},
			{Key: "item_code", Label: "数据项编码", Type: "text", Visible: true, Sortable: true, Filterable: true, Required: true,
				FilterType: "text", Operators: []string{"contains"}, WidthStrategy: "min"},
			{Key: "name", Label: "名称", Type: "text", Visible: true, Sortable: true, Filterable: true, Required: true,
				FilterType: "text", Operators: []string{"contains"}, WidthStrategy: "flex"},
			{Key: "value_type", Label: "值类型", Type: "enum", Visible: true, Sortable: true, Filterable: true, Required: true,
				FilterType: "multiSelect", Operators: []string{"in"}, OptionsSource: "static",
				Options: valueTypeOptions, WidthStrategy: "min"},
			{Key: "unit", Label: "单位", Type: "text", Visible: true, Sortable: false, Filterable: true,
				FilterType: "text", Operators: []string{"contains"}, WidthStrategy: "min"},
			{Key: "required", Label: "必填", Type: "boolean", Visible: true, Sortable: true, Filterable: true,
				FilterType: "boolean", Operators: []string{"eq"}, OptionsSource: "static",
				Options: boolOptions, WidthStrategy: "fixed"},
			{Key: "precision", Label: "小数位", Type: "number", Visible: false, Sortable: true, Filterable: true,
				FilterType: "numberRange", Operators: []string{"gte", "lte"}, AdvancedOnly: true, WidthStrategy: "fixed"},
			{Key: "min_value", Label: "最小值", Type: "number", Visible: false, Sortable: true, Filterable: true,
				FilterType: "numberRange", Operators: []string{"gte", "lte"}, AdvancedOnly: true, WidthStrategy: "fixed"},
			{Key: "max_value", Label: "最大值", Type: "number", Visible: false, Sortable: true, Filterable: true,
				FilterType: "numberRange", Operators: []string{"gte", "lte"}, AdvancedOnly: true, WidthStrategy: "fixed"},
		},
		relations: map[string]string{"type_id": "device-types"},
	},
	"terminals": {
		fields: []FieldMeta{
			{Key: "id", Label: "终端 ID", Type: "id", Visible: true, Sortable: true, Filterable: true, Required: true,
				FilterType: "text", Operators: []string{"eq", "prefix"}, WidthStrategy: "min"},
			{Key: "type_id", Label: "终端类型", Type: "relation", Visible: true, Sortable: true, Filterable: true, Required: true,
				FilterType: "relation", Operators: []string{"in"}, OptionsSource: "remote", WidthStrategy: "min"},
			{Key: "gateway_id", Label: "网关 ID", Type: "text", Visible: true, Sortable: true, Filterable: true, Required: true,
				FilterType: "text", Operators: []string{"contains", "eq"}, WidthStrategy: "min"},
			{Key: "name", Label: "名称", Type: "text", Visible: true, Sortable: true, Filterable: true,
				FilterType: "text", Operators: []string{"contains"}, WidthStrategy: "flex"},
			{Key: "enabled", Label: "状态", Type: "boolean", Visible: true, Sortable: true, Filterable: true,
				FilterType: "boolean", Operators: []string{"eq"}, OptionsSource: "static",
				Options: enabledOptions, WidthStrategy: "fixed"},
		},
		relations: map[string]string{"type_id": "device-types"},
	},
	"quality-rules": {
		fields: []FieldMeta{
			{Key: "id", Label: "ID", Type: "id", Visible: true, Sortable: true, FilterType: "none", WidthStrategy: "fixed"},
			{Key: "terminal_id", Label: "终端", Type: "relation", Visible: true, Sortable: true, Filterable: true, Required: true,
				FilterType: "text", Operators: []string{"prefix", "eq"}, OptionsSource: "remote", WidthStrategy: "min"},
			{Key: "item_code", Label: "数据项编码", Type: "text", Visible: true, Sortable: true, Filterable: true, Required: true,
				FilterType: "text", Operators: []string{"contains"}, WidthStrategy: "min"},
			{Key: "rule_type", Label: "规则类型", Type: "enum", Visible: true, Sortable: true, Filterable: true, Required: true,
				FilterType: "multiSelect", Operators: []string{"in"}, OptionsSource: "static",
				Options: ruleTypeOptions, WidthStrategy: "min"},
			{Key: "enabled", Label: "状态", Type: "boolean", Visible: true, Sortable: true, Filterable: true,
				FilterType: "boolean", Operators: []string{"eq"}, OptionsSource: "static",
				Options: enabledOptions, WidthStrategy: "fixed"},
			{Key: "params", Label: "参数", Type: "json", Visible: false, Sortable: false, Filterable: false,
				FilterType: "none", AdvancedOnly: true, WidthStrategy: "flex"},
		},
		relations: map[string]string{"terminal_id": "terminals"},
	},
	// 两张异常表只读：字段元数据只服务于管理后台的筛选/排序/白名单，不出现在 admin.Entities 里。
	"exception-events": {
		fields: []FieldMeta{
			{Key: "terminal_id", Label: "终端", Type: "text", Visible: true, Sortable: true, Filterable: true,
				FilterType: "text", Operators: []string{"contains", "eq"}, WidthStrategy: "min"},
			{Key: "gateway_id", Label: "网关", Type: "text", Visible: true, Sortable: true, Filterable: true,
				FilterType: "text", Operators: []string{"contains"}, WidthStrategy: "min"},
			{Key: "error_type", Label: "异常类型", Type: "enum", Visible: true, Sortable: true, Filterable: true,
				FilterType: "multiSelect", Operators: []string{"in"}, OptionsSource: "static",
				Options: eventTypeOptions, WidthStrategy: "min"},
			{Key: "hour_start", Label: "归属小时", Type: "datetime", Visible: true, Sortable: true, Filterable: true,
				FilterType: "dateRange", Operators: []string{"gte", "lte"}, WidthStrategy: "min"},
			{Key: "count", Label: "次数", Type: "number", Visible: true, Sortable: true, Filterable: true,
				FilterType: "numberRange", Operators: []string{"gte", "lte"}, WidthStrategy: "fixed"},
			{Key: "first_seen", Label: "首次出现", Type: "datetime", Visible: true, Sortable: true, Filterable: true,
				FilterType: "dateRange", Operators: []string{"gte", "lte"}, AdvancedOnly: true, WidthStrategy: "min"},
			{Key: "last_seen", Label: "最近出现", Type: "datetime", Visible: true, Sortable: true, Filterable: true,
				FilterType: "dateRange", Operators: []string{"gte", "lte"}, AdvancedOnly: true, WidthStrategy: "min"},
			{Key: "sample_digest", Label: "样本摘要", Type: "text", Visible: false, Sortable: false, Filterable: false,
				FilterType: "none", AdvancedOnly: true, WidthStrategy: "flex"},
		},
	},
	"exception-intervals": {
		fields: []FieldMeta{
			{Key: "id", Label: "ID", Type: "id", Visible: true, Sortable: true, FilterType: "none", WidthStrategy: "fixed"},
			{Key: "terminal_id", Label: "终端", Type: "text", Visible: true, Sortable: true, Filterable: true,
				FilterType: "text", Operators: []string{"contains", "eq"}, WidthStrategy: "min"},
			{Key: "gateway_id", Label: "网关", Type: "text", Visible: true, Sortable: true, Filterable: true,
				FilterType: "text", Operators: []string{"contains"}, WidthStrategy: "min"},
			{Key: "item_code", Label: "数据项", Type: "text", Visible: true, Sortable: true, Filterable: true,
				FilterType: "text", Operators: []string{"contains"}, WidthStrategy: "min"},
			{Key: "error_type", Label: "异常类型", Type: "enum", Visible: true, Sortable: true, Filterable: true,
				FilterType: "multiSelect", Operators: []string{"in"}, OptionsSource: "static",
				Options: intervalTypeOptions, WidthStrategy: "min"},
			// period 是 tstzrange：可展示但不可直接比较，时间范围筛选走 created_at。
			{Key: "period", Label: "区间", Type: "text", Visible: true, Sortable: false, Filterable: false,
				FilterType: "none", WidthStrategy: "flex"},
			{Key: "raw_count", Label: "原始计数", Type: "number", Visible: true, Sortable: true, Filterable: true,
				FilterType: "numberRange", Operators: []string{"gte", "lte"}, WidthStrategy: "fixed"},
			{Key: "created_at", Label: "创建时间", Type: "datetime", Visible: true, Sortable: true, Filterable: true,
				FilterType: "dateRange", Operators: []string{"gte", "lte"}, WidthStrategy: "min"},
		},
	},
	// 只读数据源（R-022 读路径 / R-030 可见面）：不在 admin.Entities 里，只服务
	// /admin/v1/readonly/{slot}。只读页没有写路径，Editable/Required 一律 false
	// （Editable 由 FieldMetas 推导，这些名字不在 Entities 内，因此天然为 false）。
	//
	// 三项共用同一组 8 个公共列（runtimeCommonFields），暂存/快照再追加各自的列。
	"latest-values":  {fields: runtimeCommonFields()},
	"hourly-staging": {fields: prependFields([]FieldMeta{hourStartField}, runtimeCommonFields())},
	"hourly-snapshot": {fields: prependFields([]FieldMeta{hourStartField},
		append(runtimeCommonFields(), revisionField, settledAtField))},
	"settle-configs": {
		fields: []FieldMeta{
			{Key: "id", Label: "ID", Type: "id", Visible: true, Sortable: true, FilterType: "none", WidthStrategy: "fixed"},
			{Key: "domain_id", Label: "数据域", Type: "relation", Visible: true, Sortable: true, Filterable: true,
				FilterType: "relation", Operators: []string{"in"}, OptionsSource: "remote", WidthStrategy: "min"},
			{Key: "type_id", Label: "终端类型", Type: "relation", Visible: true, Sortable: true, Filterable: true,
				FilterType: "relation", Operators: []string{"in"}, OptionsSource: "remote", WidthStrategy: "min"},
			{Key: "window_seconds", Label: "窗口秒数", Type: "number", Visible: true, Sortable: true, Filterable: true, Required: true,
				FilterType: "numberRange", Operators: []string{"gte", "lte"}, WidthStrategy: "min"},
		},
		relations: map[string]string{"domain_id": "data-domains", "type_id": "device-types"},
	},
}

// FieldMetas 返回实体的字段元数据；未知实体返回 false。
func FieldMetas(entity string) ([]FieldMeta, bool) {
	set, ok := fieldSets[entity]
	if !ok || len(set.fields) == 0 {
		return nil, false
	}
	out := make([]FieldMeta, len(set.fields))
	copy(out, set.fields)
	// Editable 一律由 Entity.Cols 推导（写路径的唯一白名单，见 Entity.columns()）；
	// 异常只读页不在 Entities 里，没有写路径，保持 false。
	if e, ok := lookup(entity); ok {
		for i := range out {
			out[i].Editable = hasColumn(e, out[i].Key)
		}
	}
	return out, true
}

// FieldMetaOf 返回单个字段的元数据。
func FieldMetaOf(entity, key string) (FieldMeta, bool) {
	for _, f := range fieldSets[entity].fields {
		if f.Key == key {
			return f, true
		}
	}
	return FieldMeta{}, false
}

// RelationTarget 返回关联字段的目标实体（"terminals" 的 type_id → "device-types"）。
func RelationTarget(entity, key string) (string, bool) {
	target, ok := fieldSets[entity].relations[key]
	return target, ok
}

// OperatorLabel 是筛选操作符的中文说明，筛选标签直接展示。
func OperatorLabel(op string) string {
	switch op {
	case "contains":
		return "包含"
	case "eq":
		return "等于"
	case "prefix":
		return "前缀"
	case "in":
		return "属于"
	case "gte":
		return "≥"
	case "lte":
		return "≤"
	case "between":
		return "介于"
	default:
		return op
	}
}

// OptionLabel 把字段取值映射成中文标签；未知值原样返回，避免吞掉脏数据。
func OptionLabel(entity, key, value string) string {
	f, ok := FieldMetaOf(entity, key)
	if !ok {
		return value
	}
	for _, o := range f.Options {
		if o.Value == value {
			return o.Label
		}
	}
	return value
}

// OptionTone 返回状态标签的颜色语义，模板据此选 class（文字始终显示）。
func OptionTone(entity, key, value string) string {
	f, ok := FieldMetaOf(entity, key)
	if !ok {
		return "neutral"
	}
	for _, o := range f.Options {
		if o.Value == value {
			return o.Tone
		}
	}
	return "neutral"
}

// VisibleFields 返回列表页展示的字段（顺序即列顺序，语义作用域固定为首列/末列之外）。
func VisibleFields(entity string) []FieldMeta {
	all, ok := FieldMetas(entity)
	if !ok {
		return nil
	}
	out := make([]FieldMeta, 0, len(all))
	for _, f := range all {
		if f.Visible {
			out = append(out, f)
		}
	}
	return out
}

// QuickFilters 返回常驻筛选字段（每页 ≤5），其余为 AdvancedOnly。
func QuickFilters(entity string) []FieldMeta {
	all, ok := FieldMetas(entity)
	if !ok {
		return nil
	}
	out := make([]FieldMeta, 0, 5)
	for _, f := range all {
		if f.Filterable && !f.AdvancedOnly && len(out) < 5 {
			out = append(out, f)
		}
	}
	return out
}

// AdvancedFilters 返回「更多筛选」抽屉里的字段。
func AdvancedFilters(entity string) []FieldMeta {
	all, ok := FieldMetas(entity)
	if !ok {
		return nil
	}
	var out []FieldMeta
	for _, f := range all {
		if f.Filterable && f.AdvancedOnly {
			out = append(out, f)
		}
	}
	return out
}

// TextSearchFields 返回参与关键词搜索的列（只做文本列，不做全表模糊）。
func TextSearchFields(entity string) []FieldMeta {
	all, ok := FieldMetas(entity)
	if !ok {
		return nil
	}
	var out []FieldMeta
	for _, f := range all {
		if f.Filterable && f.Type == "text" {
			out = append(out, f)
		}
	}
	return out
}
