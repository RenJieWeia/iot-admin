import type { FieldMeta, MetaPayload, ReadonlyMeta } from '../api/types';

/**
 * 假元数据严格按 task-004 §4 契约 + 本轮新增的 editable/required 构造。
 * editable/required 的默认值为 false（与前端 fail-closed 口径一致），逐字段显式覆盖。
 */
function field(partial: Partial<FieldMeta> & Pick<FieldMeta, 'key' | 'label' | 'type'>): FieldMeta {
  return {
    visible: true,
    sortable: true,
    filterable: true,
    filterType: 'text',
    operators: ['contains'],
    operatorLabels: { contains: '包含', eq: '等于', prefix: '前缀', in: '属于', gte: '≥', lte: '≤' },
    options: [],
    advancedOnly: false,
    editable: false,
    required: false,
    widthStrategy: 'flex',
    ...partial,
  };
}

/** quality 的 8 种组合（task-009 §2 A2）：文案与色调全部由后端下发，前端只是渲染。 */
export const qualityOptions = [
  { value: '0', label: '正常', tone: 'neutral' as const },
  { value: '1', label: '越界', tone: 'warning' as const },
  { value: '2', label: '规则命中', tone: 'warning' as const },
  { value: '3', label: '越界+规则', tone: 'warning' as const },
  { value: '4', label: '单位闭集外', tone: 'warning' as const },
  { value: '5', label: '越界+单位', tone: 'warning' as const },
  { value: '6', label: '规则+单位', tone: 'warning' as const },
  { value: '7', label: '越界+规则+单位', tone: 'warning' as const },
];

/** 运行时只读页共用的 8 列（task-010 §3.2）。 */




/** `_meta.readonly`：最新值（offset）+ 快照三存储（cursor，无 total）。 */
export const readonlyFixture: ReadonlyMeta[] = [
  {
    slot: 'latest-values',
    name: 'latest-values',
    title: '最新值',
    route: '/admin/v1/readonly/latest-values',
    readOnly: true,
    defaultSort: 'value_time',
    defaultOrder: 'desc',
    pagination: 'offset',
    fields: [
      field({ key: 'terminal_id', label: '终端', type: 'text', operators: ['contains', 'eq'], widthStrategy: 'min' }),
      field({ key: 'item_code', label: '数据项编码', type: 'text', widthStrategy: 'min' }),
      field({
        key: 'value_num',
        label: '数值',
        type: 'number',
        filterType: 'numberRange',
        operators: ['gte', 'lte'],
        advancedOnly: true,
        widthStrategy: 'fixed',
      }),
      field({
        key: 'value_text',
        label: '文本值',
        type: 'text',
        advancedOnly: true,
        widthStrategy: 'min',
      }),
      field({
        key: 'unit',
        label: '单位',
        type: 'text',
        advancedOnly: true,
        widthStrategy: 'fixed',
      }),
      field({
        key: 'value_time',
        label: '取值时间',
        type: 'datetime',
        filterType: 'dateRange',
        operators: ['gte', 'lte'],
        widthStrategy: 'min',
      }),
      field({
        key: 'quality',
        label: '质量',
        type: 'number',
        filterType: 'numberRange',
        operators: ['gte', 'lte'],
        optionsSource: 'static',
        options: qualityOptions,
        widthStrategy: 'fixed',
      }),
      field({
        key: 'quality_reason',
        label: '质量原因',
        type: 'text',
        sortable: false,
        advancedOnly: true,
        widthStrategy: 'flex',
      }),
    ],
  },
  {
    slot: 'snapshot-coarse',
    name: 'snapshot-coarse',
    title: '粗表快照',
    route: '/admin/v1/readonly/snapshot-coarse',
    readOnly: true,
    defaultSort: 'segment_start',
    defaultOrder: 'desc',
    pagination: 'cursor',
    fields: [
      field({ key: 'terminal_id', label: '终端', type: 'text', operators: ['contains', 'eq'], widthStrategy: 'min' }),
      field({ key: 'item_code', label: '数据项', type: 'text', operators: ['contains'], widthStrategy: 'min' }),
      field({
        key: 'granularity', label: '档位', type: 'enum', filterType: 'multiSelect', operators: ['in'],
        optionsSource: 'static', widthStrategy: 'fixed',
        options: [
          { value: 'hour', label: '时' }, { value: 'day', label: '日' },
          { value: 'month', label: '月' }, { value: 'year', label: '年' },
        ],
      }),
      field({ key: 'segment_start', label: '段起点', type: 'datetime', filterType: 'dateRange', operators: ['gte', 'lte'], widthStrategy: 'min' }),
      field({ key: 'value_num', label: '数值', type: 'number', filterType: 'numberRange', operators: ['gte', 'lte'], widthStrategy: 'fixed' }),
      field({ key: 'unit', label: '单位', type: 'text', operators: ['contains'], widthStrategy: 'fixed' }),
    ],
  },
  {
    slot: 'snapshot-minute',
    name: 'snapshot-minute',
    title: '分表快照',
    route: '/admin/v1/readonly/snapshot-minute',
    readOnly: true,
    defaultSort: 'segment_start',
    defaultOrder: 'desc',
    pagination: 'cursor',
    fields: [
      field({ key: 'terminal_id', label: '终端', type: 'text', operators: ['contains', 'eq'], widthStrategy: 'min' }),
      field({ key: 'item_code', label: '数据项', type: 'text', operators: ['contains'], widthStrategy: 'min' }),
      field({ key: 'segment_start', label: '段起点', type: 'datetime', filterType: 'dateRange', operators: ['gte', 'lte'], widthStrategy: 'min' }),
      field({ key: 'value_num', label: '数值', type: 'number', filterType: 'numberRange', operators: ['gte', 'lte'], widthStrategy: 'fixed' }),
      field({ key: 'unit', label: '单位', type: 'text', operators: ['contains'], widthStrategy: 'fixed' }),
    ],
  },
  {
    slot: 'snapshot-second',
    name: 'snapshot-second',
    title: '秒表快照',
    route: '/admin/v1/readonly/snapshot-second',
    readOnly: true,
    defaultSort: 'segment_start',
    defaultOrder: 'desc',
    pagination: 'cursor',
    fields: [
      field({ key: 'terminal_id', label: '终端', type: 'text', operators: ['contains', 'eq'], widthStrategy: 'min' }),
      field({ key: 'item_code', label: '数据项', type: 'text', operators: ['contains'], widthStrategy: 'min' }),
      field({ key: 'segment_start', label: '段起点', type: 'datetime', filterType: 'dateRange', operators: ['gte', 'lte'], widthStrategy: 'min' }),
      field({ key: 'value_num', label: '数值', type: 'number', filterType: 'numberRange', operators: ['gte', 'lte'], widthStrategy: 'fixed' }),
      field({ key: 'unit', label: '单位', type: 'text', operators: ['contains'], widthStrategy: 'fixed' }),
    ],
  },
];

export const metaFixture: MetaPayload = {
  entities: [
    {
      name: 'terminals',
      title: '终端',
      route: '/admin/v1/terminals',
      readOnly: false,
      defaultSort: 'id',
      defaultOrder: 'desc',
      fields: [
        // 用户自填主键：可编辑 + 必填（后端按 admin.Entity.Cols 判定的结果）。
        field({
          key: 'id',
          label: '终端 ID',
          type: 'id',
          filterType: 'text',
          operators: ['eq', 'prefix'],
          editable: true,
          required: true,
          widthStrategy: 'min',
        }),
        field({
          key: 'type_id',
          label: '终端类型',
          type: 'relation',
          filterType: 'relation',
          operators: ['in'],
          optionsSource: 'remote',
          editable: true,
          widthStrategy: 'min',
          ref: { entity: 'device-types', valueKey: 'id', labelKey: 'name' },
        }),
        field({
          key: 'gateway_id',
          label: '网关 ID',
          type: 'text',
          filterType: 'text',
          operators: ['contains', 'eq'],
          editable: true,
          widthStrategy: 'min',
        }),
        field({
          key: 'name',
          label: '名称',
          type: 'text',
          editable: true,
          required: false,
          widthStrategy: 'flex',
        }),
        field({
          key: 'enabled',
          label: '状态',
          type: 'boolean',
          filterType: 'boolean',
          operators: ['eq'],
          optionsSource: 'static',
          editable: true,
          widthStrategy: 'fixed',
        }),
        field({
          key: 'precision',
          label: '小数位',
          type: 'number',
          visible: false,
          filterType: 'numberRange',
          operators: ['gte', 'lte'],
          advancedOnly: true,
          widthStrategy: 'fixed',
        }),
      ],
    },
    {
      // 自增主键不可编辑；code/name 可编辑但非必填。
      name: 'data-domains',
      title: '数据域',
      route: '/admin/v1/data-domains',
      readOnly: false,
      defaultSort: 'id',
      defaultOrder: 'desc',
      fields: [
        field({
          key: 'id',
          label: 'ID',
          type: 'id',
          filterable: false,
          filterType: 'none',
          operators: [],
          editable: false,
          required: false,
          widthStrategy: 'fixed',
        }),
        field({
          key: 'code',
          label: '编码',
          type: 'text',
          editable: true,
          required: true,
          widthStrategy: 'min',
        }),
        field({
          key: 'name',
          label: '名称',
          type: 'text',
          editable: true,
          required: true,
          widthStrategy: 'flex',
        }),
      ],
    },
    {
      // json 参数列：可编辑、非必填（覆盖 required=false 的写路径）。
      name: 'quality-rules',
      title: '质量规则',
      route: '/admin/v1/quality-rules',
      readOnly: false,
      defaultSort: 'id',
      defaultOrder: 'desc',
      fields: [
        field({
          key: 'id',
          label: 'ID',
          type: 'id',
          filterable: false,
          filterType: 'none',
          operators: [],
          editable: false,
          widthStrategy: 'fixed',
        }),
        field({
          key: 'terminal_id',
          label: '终端',
          type: 'relation',
          filterType: 'text',
          operators: ['prefix', 'eq'],
          editable: true,
          required: true,
          ref: { entity: 'terminals', valueKey: 'id', labelKey: 'name' },
          widthStrategy: 'min',
        }),
        field({
          key: 'item_code',
          label: '数据项编码',
          type: 'text',
          editable: true,
          required: true,
          widthStrategy: 'min',
        }),
        field({
          key: 'rule_type',
          label: '规则类型',
          type: 'enum',
          filterType: 'multiSelect',
          operators: ['in'],
          editable: true,
          required: true,
          widthStrategy: 'min',
        }),
        field({
          key: 'enabled',
          label: '状态',
          type: 'boolean',
          filterType: 'boolean',
          operators: ['eq'],
          editable: true,
          required: false,
          widthStrategy: 'fixed',
        }),
        field({
          key: 'params',
          label: '参数',
          type: 'json',
          visible: false,
          filterable: false,
          filterType: 'none',
          operators: [],
          editable: true,
          required: false,
          widthStrategy: 'flex',
        }),
      ],
    },
    {
      // 六档全局配置：导航实体（标题仍是「快照计划」），六行固定、主键是 granularity。
      name: 'snapshot-slots',
      title: '快照计划',
      route: '/admin/v1/snapshot-slots',
      readOnly: false,
      defaultSort: 'granularity',
      defaultOrder: 'asc',
      fields: [
        field({ key: 'granularity', label: '档位', type: 'id', filterable: false, filterType: 'none', operators: [], required: true, widthStrategy: 'fixed' }),
        field({ key: 'enabled', label: '启用', type: 'boolean', filterable: false, filterType: 'none', operators: [], editable: true, widthStrategy: 'fixed' }),
        field({ key: 'step', label: '步长', type: 'number', sortable: false, filterable: false, filterType: 'none', operators: [], editable: true, widthStrategy: 'fixed' }),
        field({ key: 'updated_at', label: '更新时间', type: 'datetime', visible: false, filterable: false, filterType: 'none', operators: [], widthStrategy: 'min' }),
      ],
    },
    {
      name: 'device-types',
      title: '终端类型',
      route: '/admin/v1/device-types',
      readOnly: false,
      defaultSort: 'id',
      defaultOrder: 'desc',
      fields: [
        field({
          key: 'id',
          label: 'ID',
          type: 'id',
          filterable: false,
          filterType: 'none',
          operators: [],
          widthStrategy: 'fixed',
        }),
        field({ key: 'code', label: '编码', type: 'text', editable: true, widthStrategy: 'min' }),
        field({ key: 'name', label: '名称', type: 'text', editable: true, widthStrategy: 'flex' }),
        field({
          key: 'domain_id',
          label: '归属数据域',
          type: 'relation',
          filterType: 'relation',
          operators: ['in'],
          optionsSource: 'remote',
          widthStrategy: 'min',
          ref: { entity: 'data-domains', valueKey: 'id', labelKey: 'name' },
        }),
      ],
    },
  ],
  /**
   * 子资源（task-016）：与 entities 同形，但不进导航。终端属性从独立页收进
   * 「终端类型详情页」后，字段元数据由这里下发。
   */
  subresources: [
    {
      name: 'snapshot-bindings',
      title: '快照绑定',
      route: '/admin/v1/snapshot-bindings',
      readOnly: false,
      defaultSort: 'id',
      defaultOrder: 'desc',
      fields: [
        field({ key: 'id', label: 'ID', type: 'id', filterable: false, filterType: 'none', operators: [], widthStrategy: 'fixed' }),
        field({
          key: 'device_type_id', label: '终端类型', type: 'relation', filterType: 'relation', operators: ['in'],
          optionsSource: 'remote', editable: true, required: false, widthStrategy: 'min',
          ref: { entity: 'device-types', valueKey: 'id', labelKey: 'name' },
        }),
        field({ key: 'terminal_id', label: '终端', type: 'text', filterType: 'text', operators: ['contains'], editable: true, required: false, widthStrategy: 'min' }),
        field({ key: 'item_code', label: '数据项编码', type: 'text', editable: true, required: true, widthStrategy: 'min' }),
        field({ key: 'created_at', label: '创建时间', type: 'datetime', filterable: false, filterType: 'none', operators: [], widthStrategy: 'min' }),
      ],
    },
    {
      name: 'snapshot-settings',
      title: '存储与保留',
      route: '/admin/v1/snapshot-settings',
      readOnly: false,
      defaultSort: 'updated_at',
      defaultOrder: 'desc',
      fields: [
        field({ key: 'table_name', label: '存储', type: 'id', filterable: false, filterType: 'none', operators: [], required: true, widthStrategy: 'min' }),
        field({ key: 'retention_days', label: '保留天数（空 = 永久）', type: 'number', sortable: true, filterable: false, filterType: 'none', operators: [], editable: true, widthStrategy: 'fixed' }),
        field({ key: 'updated_at', label: '更新时间', type: 'datetime', visible: false, filterable: false, filterType: 'none', operators: [], widthStrategy: 'min' }),
      ],
    },
    {
      name: 'thing-model-properties',
      title: '终端属性',
      route: '/admin/v1/thing-model-properties',
      readOnly: false,
      defaultSort: 'item_code',
      defaultOrder: 'asc',
      fields: [
        field({
          key: 'id',
          label: '属性 ID',
          type: 'id',
          filterable: false,
          filterType: 'none',
          operators: [],
          widthStrategy: 'fixed',
        }),
        field({
          key: 'type_id',
          label: '终端类型',
          type: 'relation',
          filterType: 'relation',
          operators: ['in'],
          optionsSource: 'remote',
          editable: true,
          required: true,
          widthStrategy: 'fixed',
          ref: { entity: 'device-types', valueKey: 'id', labelKey: 'name' },
        }),
        field({
          key: 'item_code',
          label: '点位编码',
          type: 'text',
          filterType: 'text',
          operators: ['contains', 'eq', 'prefix'],
          editable: true,
          required: true,
          widthStrategy: 'min',
        }),
        field({
          key: 'name',
          label: '属性名称',
          type: 'text',
          editable: true,
          required: true,
          widthStrategy: 'flex',
        }),
        field({
          key: 'value_type',
          label: '值类型',
          type: 'enum',
          filterType: 'multiSelect',
          operators: ['in'],
          editable: true,
          required: true,
          optionsSource: 'static',
          options: [
            { value: 'number', label: '数字' },
            { value: 'string', label: '文本' },
            { value: 'bool', label: '布尔' },
          ],
          widthStrategy: 'fixed',
        }),
        field({
          key: 'unit',
          label: '单位',
          type: 'text',
          editable: true,
          required: false,
          widthStrategy: 'fixed',
        }),
      ],
    },
  ],
  exceptions: [
    {
      slot: 'events',
      name: 'exception-events',
      title: '异常事件',
      route: '/admin/v1/exceptions/events',
      readOnly: true,
      defaultSort: 'hour_start',
      defaultOrder: 'desc',
      fields: [
        field({
          key: 'terminal_id',
          label: '终端',
          type: 'text',
          operators: ['contains', 'eq'],
          editable: false,
          required: false,
          widthStrategy: 'min',
        }),
        field({
          key: 'error_type',
          label: '异常类型',
          type: 'enum',
          filterType: 'multiSelect',
          operators: ['in'],
          optionsSource: 'static',
          widthStrategy: 'min',
        }),
        field({
          key: 'count',
          label: '次数',
          type: 'number',
          filterType: 'numberRange',
          operators: ['gte', 'lte'],
          widthStrategy: 'fixed',
        }),
      ],
    },
  ],
  // 枚举字典与布尔字典由后端下发，前端归一后渲染中文。
  enums: {
    second_step: { '1': '1 秒', '5': '5 秒', '10': '10 秒', '15': '15 秒', '30': '30 秒（_meta）' },
    minute_step: { '1': '1 分', '5': '5 分', '10': '10 分', '30': '30 分' },
    hour_step: { '1': '1 时', '6': '6 时', '12': '12 时' },
    day_step: { '1': '1 日' },
    month_step: { '1': '1 月', '12': '12 月' },
    year_step: { '1': '1 年' },
    error_type: { protocol_error: '协议错误', msg_incomplete: '报文不完整' },
  },
  booleans: { true: '是', false: '否' },
  pagination: { default: 10, max: 50, options: [10, 20, 50] },
  readonly: readonlyFixture,
};

/** 128 条终端，用于验证「只渲染当前页 10 行」。 */
export const allTerminals = Array.from({ length: 128 }).map((_, index) => ({
  id: `t-${String(index + 1).padStart(3, '0')}`,
  type_id: index % 2 === 0 ? '1' : '2',
  gateway_id: `gw-${(index % 4) + 1}`,
  name: `终端 ${index + 1}`,
  enabled: index % 3 === 0 ? 'false' : 'true',
}));

export const deviceTypeOptions = [
  { value: '1', label: '泵站' },
  { value: '2', label: '阀门' },
  { value: '3', label: '电表' },
  { value: '4', label: '水表' },
];

/** 最新值 128 行：quality 轮转 0..7，用于验证「按 options 渲染标签」与总数 128。 */
export const latestValues = Array.from({ length: 128 }).map((_, index) => {
  const quality = index % 8;
  return {
    terminal_id: `t-${String((index % 20) + 1).padStart(3, '0')}`,
    item_code: `item-${(index % 6) + 1}`,
    value_num: (index * 7) % 100,
    value_text: null,
    unit: index % 5 === 0 ? 'kW' : '℃',
    value_time: new Date(Date.UTC(2026, 10, 5, 12, 0, index % 60)).toISOString(),
    quality: String(quality),
    quality_reason: quality === 0 ? null : `原因-${quality}`,
  };
});

export const exceptionEvents = [
  { terminal_id: 't-001', error_type: 'msg_incomplete', count: 3 },
  { terminal_id: 't-002', error_type: 'protocol_error', count: 1 },
];

/** 终端类型行（详情页摘要与列表入口用）。 */
export const allDeviceTypes = [
  { id: '1', code: 'pump', name: '泵站', domain_id: '1' },
  { id: '2', code: 'valve', name: '阀门', domain_id: '1' },
  { id: '3', code: 'meter', name: '电表', domain_id: '2' },
];

/** 终端属性行：type_id 用于验证详情页的固定过滤。 */
export const allProperties = [
  { id: 'p-1', type_id: '1', item_code: 'temp', name: '温度', value_type: 'number', unit: 'C' },
  { id: 'p-2', type_id: '1', item_code: 'power', name: '功率', value_type: 'number', unit: 'kW' },
  { id: 'p-3', type_id: '1', item_code: 'status', name: '状态', value_type: 'string', unit: '' },
  { id: 'p-4', type_id: '1', item_code: 'online', name: '在线', value_type: 'bool', unit: '' },
  { id: 'p-5', type_id: '2', item_code: 'temp', name: '温度', value_type: 'number', unit: 'C' },
  { id: 'p-6', type_id: '2', item_code: 'flow', name: '流量', value_type: 'number', unit: 'm3/h' },
];

/** 六档快照的 enabled/step 字段（与 internal/admin/fields.go 的步长白名单同源）。 */
export function sixGranularityFields(): FieldMeta[] {
  const steps: Record<string, string[]> = {
    second: ['1', '2', '3', '4', '5', '6', '10', '12', '15', '20', '30'],
    minute: ['1', '2', '3', '4', '5', '6', '10', '12', '15', '20', '30'],
    hour: ['1', '2', '3', '4', '6', '8', '12'],
    day: ['1'],
    month: ['1', '2', '3', '4', '6', '12'],
    year: ['1'],
  };
  const labels: Record<string, string> = { second: '秒', minute: '分', hour: '时', day: '日', month: '月', year: '年' };
  const out: FieldMeta[] = [];
  for (const key of ['second', 'minute', 'hour', 'day', 'month', 'year']) {
    out.push(
      field({
        key: `${key}_enabled`, label: `${labels[key]}档`, type: 'boolean', filterType: 'boolean',
        operators: ['eq'], optionsSource: 'static', editable: true, widthStrategy: 'fixed',
      }),
      field({
        key: `${key}_step`, label: `${labels[key]}步长`, type: 'enum', sortable: false, filterType: 'multiSelect',
        operators: ['in'], optionsSource: 'static', editable: true, widthStrategy: 'fixed',
        options: (steps[key] ?? []).map((value) => ({ value, label: `${value} ${labels[key]}`, tone: 'neutral' as const })),
      }),
    );
  }
  return out;
}

export const allSnapshotPlans = [
  { id: 1, device_type_id: '1', item_code: 'temp', enabled_steps: '秒30, 分5, 时1', second_enabled: 'true', second_step: '30', minute_enabled: 'true', minute_step: '5', hour_enabled: 'true', hour_step: '1', day_enabled: 'false', day_step: null, month_enabled: 'false', month_step: null, year_enabled: 'false', year_step: null },
  { id: 2, device_type_id: '1', item_code: 'power', enabled_steps: '分1', second_enabled: 'false', second_step: null, minute_enabled: 'true', minute_step: '1', hour_enabled: 'false', hour_step: null, day_enabled: 'false', day_step: null, month_enabled: 'false', month_step: null, year_enabled: 'false', year_step: null },
];

export const allSnapshotSettings = [
  { table_name: 'snapshot_coarse', retention_days: null, updated_at: '2026-11-06T10:00:00+08:00' },
  { table_name: 'snapshot_minute', retention_days: 30, updated_at: '2026-11-06T10:00:00+08:00' },
  { table_name: 'snapshot_second', retention_days: 7, updated_at: '2026-11-06T10:00:00+08:00' },
];

/** 快照三存储的行（游标分页：字段里没有 total）。 */
export const snapshotCoarseRows = [
  { terminal_id: 't-001', item_code: 'temp', granularity: 'hour', segment_start: '2026-11-06T10:00:00+08:00', value_num: 25.4, unit: 'C' },
  { terminal_id: 't-001', item_code: 'temp', granularity: 'day', segment_start: '2026-11-06T00:00:00+08:00', value_num: 26.1, unit: 'C' },
  { terminal_id: 't-002', item_code: 'power', granularity: 'month', segment_start: '2026-11-01T00:00:00+08:00', value_num: 1.2, unit: 'kW' },
  { terminal_id: 't-002', item_code: 'power', granularity: 'year', segment_start: '2026-01-01T00:00:00+08:00', value_num: 1.1, unit: 'kW' },
];

export const snapshotMinuteRows = Array.from({ length: 25 }).map((_, index) => ({
  terminal_id: `t-${String((index % 3) + 1).padStart(3, '0')}`,
  item_code: 'temp',
  segment_start: new Date(Date.UTC(2026, 10, 6, 10, index)).toISOString(),
  value_num: 20 + index,
  unit: 'C',
}));

export const snapshotSecondRows = Array.from({ length: 25 }).map((_, index) => ({
  terminal_id: 't-001',
  item_code: 'temp',
  segment_start: new Date(Date.UTC(2026, 10, 6, 10, 0, index)).toISOString(),
  value_num: 20 + index,
  unit: 'C',
}));

/** 全局六档配置：六行固定（顺序与迁移一致）。 */
export const allSnapshotSlots = [
  { granularity: 'second', enabled: 'false', step: null, updated_at: '2026-11-06T10:00:00+08:00' },
  { granularity: 'minute', enabled: 'true', step: 5, updated_at: '2026-11-06T10:00:00+08:00' },
  { granularity: 'hour', enabled: 'true', step: 1, updated_at: '2026-11-06T10:00:00+08:00' },
  { granularity: 'day', enabled: 'false', step: 1, updated_at: '2026-11-06T10:00:00+08:00' },
  { granularity: 'month', enabled: 'false', step: null, updated_at: '2026-11-06T10:00:00+08:00' },
  { granularity: 'year', enabled: 'false', step: 1, updated_at: '2026-11-06T10:00:00+08:00' },
];

/** 绑定：类型级一条、实例级一条。 */
export const allSnapshotBindings = [
  // 一行 = 一个目标 + 多个字段；fields_count 是后端派生列（测试里也按数组长度算兜底）。
  { id: 1, device_type_id: '1', terminal_id: null, item_codes: ['temp', 'power'], fields_count: 2, created_at: '2026-11-06T10:00:00+08:00' },
  { id: 2, device_type_id: null, terminal_id: 't-001', item_codes: ['power'], fields_count: 1, created_at: '2026-11-06T10:05:00+08:00' },
];
