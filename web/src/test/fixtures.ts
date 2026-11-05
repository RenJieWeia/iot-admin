import type { FieldMeta, MetaPayload } from '../api/types';

/** 假元数据严格按 task-004 §4 最终契约（rv-06 `03ca27e`）的键名构造。 */
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
    widthStrategy: 'flex',
    ...partial,
  } as FieldMeta;
}

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
        field({
          key: 'id',
          label: '终端 ID',
          type: 'id',
          filterType: 'text',
          operators: ['eq', 'prefix'],
          widthStrategy: 'min',
        }),
        field({
          key: 'type_id',
          label: '终端类型',
          type: 'relation',
          filterType: 'relation',
          operators: ['in'],
          optionsSource: 'remote',
          widthStrategy: 'min',
          ref: { entity: 'device-types', valueKey: 'id', labelKey: 'name' },
        }),
        field({
          key: 'gateway_id',
          label: '网关 ID',
          type: 'text',
          filterType: 'text',
          operators: ['contains', 'eq'],
          widthStrategy: 'min',
        }),
        field({ key: 'name', label: '名称', type: 'text', widthStrategy: 'flex' }),
        field({
          key: 'enabled',
          label: '状态',
          type: 'boolean',
          filterType: 'boolean',
          operators: ['eq'],
          optionsSource: 'static',
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

export const exceptionEvents = [
  { terminal_id: 't-001', error_type: 'upload_timeout', count: 3 },
  { terminal_id: 't-002', error_type: 'protocol_error', count: 1 },
];
