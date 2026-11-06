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
function runtimeFields(): FieldMeta[] {
  return [
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
    field({ key: 'value_text', label: '文本值', type: 'text', advancedOnly: true, widthStrategy: 'min' }),
    field({ key: 'unit', label: '单位', type: 'text', advancedOnly: true, widthStrategy: 'fixed' }),
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
  ];
}

function settledAtField(): FieldMeta {
  return field({
    key: 'settled_at',
    label: '结算时间',
    type: 'datetime',
    filterType: 'dateRange',
    operators: ['gte', 'lte'],
    widthStrategy: 'min',
  });
}

function revisionField(): FieldMeta {
  return field({
    key: 'revision',
    label: '修正次数',
    type: 'number',
    filterType: 'numberRange',
    operators: ['gte', 'lte'],
    widthStrategy: 'fixed',
  });
}

function hourStart(): FieldMeta {
  return field({
    key: 'hour_start',
    label: '归属小时',
    type: 'datetime',
    filterType: 'dateRange',
    operators: ['gte', 'lte'],
    widthStrategy: 'min',
  });
}

/** `_meta.readonly`：最新值（offset）、小时暂存（offset）、小时快照（cursor）。 */
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
    slot: 'hourly-staging',
    name: 'hourly-staging',
    title: '小时暂存',
    route: '/admin/v1/readonly/hourly-staging',
    readOnly: true,
    defaultSort: 'hour_start',
    defaultOrder: 'desc',
    pagination: 'offset',
    fields: [hourStart(), ...runtimeFields()],
  },
  {
    slot: 'hourly-snapshot',
    name: 'hourly-snapshot',
    title: '小时快照',
    route: '/admin/v1/readonly/hourly-snapshot',
    readOnly: true,
    // 默认排序命中 hourly_snapshot_settled_idx（task-010 §2）。
    defaultSort: 'settled_at',
    defaultOrder: 'desc',
    pagination: 'cursor',
    // 字段顺序与后端实测一致：hour_start 打头，revision 在 settled_at 之前。
    fields: [hourStart(), ...runtimeFields(), revisionField(), settledAtField()],
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
        field({ key: 'name', label: '名称', type: 'text', widthStrategy: 'flex' }),
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
    {
      slot: 'intervals',
      name: 'exception-intervals',
      title: '异常区间',
      route: '/admin/v1/exceptions/intervals',
      readOnly: true,
      defaultSort: 'created_at',
      defaultOrder: 'desc',
      fields: [
        field({
          key: 'terminal_id',
          label: '终端',
          type: 'text',
          operators: ['contains', 'eq'],
          widthStrategy: 'min',
        }),
        field({
          key: 'raw_count',
          label: '原始计数',
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
    error_type: { upload_timeout: '上报超时', protocol_error: '协议错误' },
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

const runtimeRow = (index: number, quality: number) => ({
  terminal_id: `t-${String((index % 20) + 1).padStart(3, '0')}`,
  item_code: `item-${(index % 6) + 1}`,
  value_num: (index * 5) % 100,
  value_text: null,
  unit: index % 5 === 0 ? 'kW' : '℃',
  value_time: new Date(Date.UTC(2026, 10, 5, 12, 0, index % 60)).toISOString(),
  quality: String(quality),
  quality_reason: quality === 0 ? null : `原因-${quality}`,
});

/** 小时暂存 25 行（offset 分页：10/页 → 3 页）。 */
export const stagingValues = Array.from({ length: 25 }).map((_, index) => ({
  id: index + 1,
  ...runtimeRow(index, index % 8),
  hour_start: new Date(Date.UTC(2026, 10, 5, index % 24)).toISOString(),
}));

/** 小时快照 45 行；每 4 行有 1 行被修正过（revision>0），用于「仅看被修正过」。 */
export const snapshotValues = Array.from({ length: 45 }).map((_, index) => ({
  id: index + 1,
  ...runtimeRow(index, index % 8),
  hour_start: new Date(Date.UTC(2026, 10, 5, index % 24)).toISOString(),
  settled_at: new Date(Date.UTC(2026, 10, 5, 1, index % 60)).toISOString(),
  revision: index % 4 === 0 ? (index % 3) + 1 : 0,
}));

export const exceptionEvents = [
  { terminal_id: 't-001', error_type: 'upload_timeout', count: 3 },
  { terminal_id: 't-002', error_type: 'protocol_error', count: 1 },
];
