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
	// Width 是列宽（CSS px）的**显式覆写**；0 表示按 Type 推导（见 widthForType）。
	// 推导结果由 _meta 下发，前端只消费结论，不自行按类型重算。
	Width int
	// Derived 表示该列不在物理表里，由列表 SQL 计算得出（如快照计划的「已启用档位」）。
	// 它不进写白名单、不可排序/筛选，DDL 对齐类测试会跳过它。
	Derived bool
}

// widthForType 是列宽默认值的唯一推导处：按字段类型给一个经验宽度。
//
// 为什么不全用 widthStrategy 的三档（fixed/min/flex）：三档只能表达"固定/最小/弹性"三种语义，
// 无法区分 boolean(90) 与 datetime(170)——两者都只能是 fixed。所以宽度改成
// 「类型推导 + 字段显式覆写」，widthStrategy 仍是布局语义（是否允许被内容撑开）。
//
// 数值取舍：布尔最窄（一个中文字）；时间戳最长（YYYY-MM-DD HH:mm）；json/长文本交给 flex。
func widthForType(t string) int {
	switch t {
	case "id":
		return 80
	case "boolean":
		return 90
	case "enum":
		return 120
	case "number":
		return 110
	case "datetime":
		return 170
	case "text":
		return 160
	case "relation":
		return 180
	default: // json 及未知类型：不推导，交给 widthStrategy 的 flex
		return 0
	}
}

var boolOptions = []Option{
	{Value: "true", Label: "是", Tone: "positive"},
	{Value: "false", Label: "否", Tone: "neutral"},
}

var enabledOptions = []Option{
	{Value: "true", Label: "启用", Tone: "positive"},
	{Value: "false", Label: "停用", Tone: "negative"},
}

// onlineOptions 是终端在线状态（默认离线；收到报文置在线）。
var onlineOptions = []Option{
	{Value: "true", Label: "在线", Tone: "positive"},
	{Value: "false", Label: "离线", Tone: "neutral"},
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

// eventTypeOptions 必须与 exception.EventTypes 一一对应（9 类事件型异常）。
// exceptions_test.go 的 TestEventTypeOptionsMatchEnum 会断言两边集合相等——
// 下线能力时要同时改枚举与这里，别只改一边留下"下拉里有、后台不认"的死选项。
var eventTypeOptions = []Option{
	{Value: "protocol_error", Label: "协议错误", Tone: "negative"},
	{Value: "msg_incomplete", Label: "消息不完整", Tone: "warning"},
	{Value: "clean_failed", Label: "清洗失败", Tone: "warning"},
	{Value: "cache_write_failed", Label: "写入失败", Tone: "negative"},
	{Value: "mqtt_publish_failed", Label: "发布失败", Tone: "negative"},
	{Value: "quality_violation", Label: "质量违规", Tone: "warning"},
	{Value: "terminal_unknown", Label: "终端未注册", Tone: "negative"},
	// 注意：这里的 snapshot_write_failed 是「六档快照写入失败」，
	// 与已删除的 snapshot_failed（旧「结算任务失败」）不是一回事，别混用。
	{Value: "snapshot_write_failed", Label: "快照写入失败", Tone: "negative"},
	// 快照迟到丢弃：某档超过「段结束 + 15 分钟」被跳过时记一次（终端 + 小时聚合，多档合并成一条）。
	{Value: "snapshot_late_dropped", Label: "快照迟到丢弃", Tone: "warning"},
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

// 六档步长白名单（与 migrations/0010 的 CHECK 约束同源）：
// 秒/分 1,2,3,4,5,6,10,12,15,20,30；时 1,2,3,4,6,8,12；日 1；月 1,2,3,4,6,12；年 1。
// 选项既用于抽屉下拉，也经 _meta.enums 下发（key 与字段名一致）。
func stepOptions(unit string, values ...string) []Option {
	out := make([]Option, len(values))
	for i, v := range values {
		out[i] = Option{Value: v, Label: v + " " + unit, Tone: "neutral"}
	}
	return out
}

// minuteStepOptions 是分钟档的倍数步长（快照只剩这一档需要步长下拉）。
var minuteStepOptions = stepOptions("分", "1", "2", "3", "4", "5", "6", "10", "12", "15", "20", "30")

// snapshotEnabledField 生成一个档位启用字段（boolean）。
func snapshotEnabledField(key, label string) FieldMeta {
	return FieldMeta{
		Key: key, Label: label, Type: "boolean", Visible: true, Sortable: false, Filterable: true,
		FilterType: "boolean", Operators: []string{"eq"}, OptionsSource: "static",
		Options: boolOptions, WidthStrategy: "fixed",
	}
}

// runtimeCommonFields 返回最新值表的 8 个公共列。
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
		{Key: "value_time", Label: "最近上报", Type: "datetime", Visible: true, Sortable: true, Filterable: true,
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

// segmentStartField 是快照页的段起点（分区键；游标按它倒序翻页）。
var segmentStartField = FieldMeta{
	Key: "segment_start", Label: "段起点", Type: "datetime", Visible: true, Sortable: true, Filterable: true,
	FilterType: "dateRange", Operators: []string{"gte", "lte"}, WidthStrategy: "min",
}

// snapshotGranularityOptions 是快照数据页的档位闭集（时/日/月/年，与 0010 的 CHECK 一致）。
var snapshotGranularityOptions = []Option{
	{Value: "hour", Label: "小时", Tone: "neutral"},
	{Value: "day", Label: "日", Tone: "neutral"},
	{Value: "month", Label: "月", Tone: "neutral"},
	{Value: "year", Label: "年", Tone: "neutral"},
}

// snapshotSlotOptions 是「快照计划」配置页的两行：快照（统管四档）+ 分钟档。
var snapshotSlotOptions = []Option{
	{Value: "snapshot", Label: "快照", Tone: "neutral"},
	{Value: "minute", Label: "分钟", Tone: "neutral"},
}

// granularityField 是粗表的档位列（分表/秒表各自只有一档，没有这一列）。
var granularityField = FieldMeta{
	Key: "granularity", Label: "档位", Type: "enum", Visible: true, Sortable: true, Filterable: true,
	FilterType: "multiSelect", Operators: []string{"in"}, OptionsSource: "static",
	Options: snapshotGranularityOptions, WidthStrategy: "min",
}

// revisionField 是快照行的覆盖计数（只有「采集时间更新」的覆盖才 +1）。
var revisionField = FieldMeta{
	Key: "revision", Label: "修正次数", Type: "number", Visible: true, Sortable: true,
	Filterable: true, FilterType: "numberRange", Operators: []string{"gte", "lte"}, WidthStrategy: "fixed",
}

// updatedAtField 是快照行的覆盖时刻（每覆盖一次刷新的 updated_at；只展示，不筛选）。
var updatedAtField = FieldMeta{
	Key: "updated_at", Label: "更新时间", Type: "datetime", Visible: false, Sortable: true,
	Filterable: false, FilterType: "none", WidthStrategy: "min",
}

// snapshotFields 按「终端、数据项、[档位]、段起点、值…、修正次数、更新时间」组织列，
// 与三张表真实列顺序一致；粗表多一列 granularity。
func snapshotFields(withGranularity bool) []FieldMeta {
	common := runtimeCommonFields()
	out := []FieldMeta{common[0], common[1]}
	if withGranularity {
		out = append(out, granularityField)
	}
	out = append(out, segmentStartField)
	out = append(out, common[2:]...)
	out = append(out, revisionField, updatedAtField)
	return out
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
	"thing-models": {
		fields: []FieldMeta{
			{Key: "id", Label: "ID", Type: "id", Visible: true, Sortable: true, FilterType: "none", WidthStrategy: "fixed"},
			{Key: "code", Label: "编码", Type: "text", Visible: true, Sortable: true, Filterable: true, Required: true,
				FilterType: "text", Operators: []string{"contains"}, WidthStrategy: "min"},
			{Key: "name", Label: "名称", Type: "text", Visible: true, Sortable: true, Filterable: true, Required: true,
				FilterType: "text", Operators: []string{"contains"}, WidthStrategy: "flex"},
			{Key: "domain_id", Label: "数据域", Type: "relation", Visible: true, Sortable: true, Filterable: true, Required: true,
				FilterType: "relation", Operators: []string{"in"}, OptionsSource: "remote", WidthStrategy: "min"},
			// 派生计数：类型绑定了多少字段、下挂多少终端实例（列表 SQL 计算，不可排序/筛选/编辑）。
			{Key: "properties_count", Label: "字段数", Type: "number", Visible: true, Sortable: false, Filterable: false,
				FilterType: "none", WidthStrategy: "fixed", Derived: true},
			{Key: "terminals_count", Label: "终端数", Type: "number", Visible: true, Sortable: false, Filterable: false,
				FilterType: "none", WidthStrategy: "fixed", Derived: true},
		},
		relations: map[string]string{"domain_id": "data-domains"},
	},
	"thing-model-properties": {
		fields: []FieldMeta{
			// 自增主键对使用者没有价值，却要占 80px：该实体没有独立列表页，
			// 唯一消费方是终端类型详情页的属性表（7 列挤在 1024~1366 视口里）。
			// 其它实体（终端/数据域/规则…）的 ID 列保持可见。
			{Key: "id", Label: "ID", Type: "id", Visible: false, Sortable: true, FilterType: "none", WidthStrategy: "fixed"},
			{Key: "type_id", Label: "物模型", Type: "relation", Visible: true, Sortable: true, Filterable: true, Required: true,
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
		relations: map[string]string{"type_id": "thing-models"},
	},
	"terminals": {
		fields: []FieldMeta{
			{Key: "id", Label: "终端 ID", Type: "id", Visible: true, Sortable: true, Filterable: true, Required: true,
				FilterType: "text", Operators: []string{"eq", "prefix"}, WidthStrategy: "min"},
			{Key: "type_id", Label: "物模型", Type: "relation", Visible: true, Sortable: true, Filterable: true, Required: true,
				FilterType: "relation", Operators: []string{"in"}, OptionsSource: "remote", WidthStrategy: "min"},
			{Key: "gateway_id", Label: "网关 ID", Type: "text", Visible: true, Sortable: true, Filterable: true, Required: true,
				FilterType: "text", Operators: []string{"contains", "eq"}, WidthStrategy: "min"},
			{Key: "name", Label: "名称", Type: "text", Visible: true, Sortable: true, Filterable: true,
				FilterType: "text", Operators: []string{"contains"}, WidthStrategy: "flex"},
			// 启停由列表操作栏/详情页的开关承载，不再占普通列。
			{Key: "enabled", Label: "状态", Type: "boolean", Visible: false, Sortable: true, Filterable: true,
				FilterType: "boolean", Operators: []string{"eq"}, OptionsSource: "static",
				Options: enabledOptions, WidthStrategy: "fixed"},
			{Key: "online", Label: "在线状态", Type: "boolean", Visible: true, Sortable: true, Filterable: true,
				FilterType: "boolean", Operators: []string{"eq"}, OptionsSource: "static",
				Options: onlineOptions, WidthStrategy: "fixed"},
			{Key: "last_seen_at", Label: "最近上报", Type: "datetime", Visible: true, Sortable: true, Filterable: true,
				FilterType: "dateRange", Operators: []string{"gte", "lte"}, WidthStrategy: "min"},
		},
		relations: map[string]string{"type_id": "thing-models"},
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
	// 只读数据源（R-022 读路径 / R-030 可见面）：不在 admin.Entities 里，只服务
	// /admin/v1/readonly/{slot}。只读页没有写路径，Editable/Required 一律 false
	// （Editable 由 FieldMetas 推导，这些名字不在 Entities 内，因此天然为 false）。
	//
	// 只读源只剩「最新值」：公共 8 列由 runtimeCommonFields 提供。
	"latest-values": {fields: runtimeCommonFields()},
	// 快照三页：游标分页（不报总数），字段与三张表真实列一致；粗表含档位列。
	"snapshot-minute": {fields: snapshotFields(false)},
	// 全局六档配置：六行固定（主键 granularity），只能改 enabled / step。
	// step 不挂枚举：白名单按档位不同（秒/分最多 30；小时/日/月/年固定 1，即每自然单位一份），
	// 逐档选项由 Go 侧 Validate 兜底并给出可读原因；_meta.enums 里仍下发六档步长字典供前端复用。
	"snapshot-slots": {
		fields: []FieldMeta{
			{Key: "granularity", Label: "档位", Type: "enum", Visible: true, Sortable: true, Required: true,
				Filterable: true, FilterType: "multiSelect", Operators: []string{"in"}, OptionsSource: "static",
				Options: snapshotSlotOptions, WidthStrategy: "min"},
			{Key: "enabled", Label: "启用", Type: "boolean", Visible: true, Sortable: true, Filterable: true,
				FilterType: "boolean", Operators: []string{"eq"}, OptionsSource: "static", Options: enabledOptions,
				WidthStrategy: "fixed"},
			{Key: "step", Label: "步长", Type: "number", Visible: true, Sortable: true, Filterable: false,
				FilterType: "numberRange", Operators: []string{"gte", "lte"}, WidthStrategy: "fixed"},
			{Key: "updated_at", Label: "更新时间", Type: "datetime", Visible: false, Sortable: true, Filterable: false,
				FilterType: "none", WidthStrategy: "min"},
		},
	},
	// 边缘网关：接入鉴权的注册表。gateway_id 是主键也是接入标识，name 是页面展示名。
	"gateways": {
		fields: []FieldMeta{
			{Key: "gateway_id", Label: "网关 ID", Type: "id", Visible: true, Sortable: true, Filterable: true,
				Required: true, FilterType: "text", Operators: []string{"eq", "prefix"}, WidthStrategy: "fixed"},
			{Key: "name", Label: "名称", Type: "text", Visible: true, Sortable: true, Filterable: true,
				Required: true, FilterType: "text", Operators: []string{"contains"}, WidthStrategy: "min"},
			// 启用以开关形式放在列表最后一列（操作栏），不再占一个普通列。
			{Key: "enabled", Label: "启用", Type: "boolean", Visible: false, Sortable: true, Filterable: true,
				FilterType: "boolean", Operators: []string{"eq"}, OptionsSource: "static",
				Options: enabledOptions, WidthStrategy: "fixed"},
			{Key: "remark", Label: "备注", Type: "text", Visible: true, Sortable: false, Filterable: false,
				FilterType: "none", WidthStrategy: "flex"},
			{Key: "created_at", Label: "注册时间", Type: "datetime", Visible: true, Sortable: true, Filterable: true,
				FilterType: "dateRange", Operators: []string{"gte", "lte"}, WidthStrategy: "min"},
			{Key: "last_seen_at", Label: "最近上报", Type: "datetime", Visible: true, Sortable: true, Filterable: true,
				FilterType: "dateRange", Operators: []string{"gte", "lte"}, WidthStrategy: "min"},
			{Key: "online", Label: "在线状态", Type: "boolean", Visible: true, Sortable: true, Filterable: true,
				FilterType: "boolean", Operators: []string{"eq"}, OptionsSource: "static",
				Options: onlineOptions, WidthStrategy: "fixed"},
			{Key: "mqtt_topic", Label: "MQTT 主题", Type: "text", Visible: true, Derived: true,
				WidthStrategy: "min"},
			// 用户名就是 gateway_id，列表不重复占列；凭据弹窗里完整展示。
			{Key: "mqtt_username", Label: "MQTT 用户名", Type: "text", Visible: false, Derived: true,
				WidthStrategy: "min"},
			{Key: "mqtt_password", Label: "MQTT 密码", Type: "text", Visible: true, Derived: true,
				WidthStrategy: "min"},
		},
	},
	// 快照查询模板：name/description 直接可编辑；类型与字段数组由专用编辑页维护。
	"snapshot-queries": {
		fields: []FieldMeta{
			{Key: "id", Label: "ID", Type: "id", Visible: true, Sortable: true, FilterType: "none", WidthStrategy: "fixed"},
			{Key: "name", Label: "名称", Type: "text", Visible: true, Sortable: true, Filterable: true,
				Required: true, FilterType: "text", Operators: []string{"contains"}, WidthStrategy: "min"},
			{Key: "description", Label: "说明", Type: "text", Visible: true, Sortable: false, Filterable: false,
				FilterType: "none", WidthStrategy: "flex"},
			{Key: "device_type_ids", Label: "物模型", Type: "json", Visible: false, Sortable: false, Filterable: false,
				FilterType: "none", WidthStrategy: "flex"},
			{Key: "item_codes", Label: "字段", Type: "json", Visible: false, Sortable: false, Filterable: false,
				FilterType: "none", WidthStrategy: "flex"},
			{Key: "updated_at", Label: "更新时间", Type: "datetime", Visible: true, Sortable: true, Filterable: false,
				FilterType: "none", WidthStrategy: "min"},
		},
	},
	// 绑定：一条 = 一个作用域目标 + 一个数据项数组；item_codes 与 quality_rule.params 同为 jsonb。
	// fields_count 是列表派生列（jsonb_array_length，SQL 计算），不可排序/筛选/编辑。
	"snapshot-bindings": {
		fields: []FieldMeta{
			{Key: "id", Label: "ID", Type: "id", Visible: true, Sortable: true, FilterType: "none", WidthStrategy: "fixed"},
			{Key: "device_type_id", Label: "物模型（类型级）", Type: "relation", Visible: true, Sortable: true, Filterable: true,
				FilterType: "relation", Operators: []string{"in"}, OptionsSource: "remote", WidthStrategy: "min"},
			{Key: "terminal_id", Label: "终端（实例级）", Type: "relation", Visible: true, Sortable: true, Filterable: true,
				FilterType: "relation", Operators: []string{"in"}, OptionsSource: "remote", WidthStrategy: "min"},
			{Key: "item_codes", Label: "数据项编码（可多个）", Type: "json", Visible: true, Sortable: false, Filterable: false,
				FilterType: "none", WidthStrategy: "flex"},
			{Key: "fields_count", Label: "字段数", Type: "number", Visible: true, Sortable: false, Filterable: false,
				FilterType: "none", WidthStrategy: "fixed", Derived: true},
			{Key: "created_at", Label: "创建时间", Type: "datetime", Visible: true, Sortable: true, Filterable: false,
				FilterType: "none", WidthStrategy: "min"},
		},
		relations: map[string]string{"device_type_id": "thing-models", "terminal_id": "terminals"},
	},
	// 表级保留期：三行固定，只能改 retention_days；空值 = 永久保留。
	"snapshot-settings": {
		fields: []FieldMeta{
			{Key: "table_name", Label: "存储", Type: "id", Visible: true, Sortable: true, Required: true, FilterType: "none", WidthStrategy: "min"},
			{Key: "retention_days", Label: "保留天数（空 = 永久）", Type: "number", Visible: true, Sortable: true,
				Filterable: false, FilterType: "none", WidthStrategy: "fixed"},
			{Key: "updated_at", Label: "更新时间", Type: "datetime", Visible: false, Sortable: true, Filterable: false,
				FilterType: "none", WidthStrategy: "min"},
		},
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
			out[i].Editable = hasWritableColumn(e, out[i].Key)
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

// RelationTarget 返回关联字段的目标实体（"terminals" 的 type_id → "thing-models"）。
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
