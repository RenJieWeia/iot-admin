import { describe, expect, it } from 'vitest';
import type { FieldMeta } from '../api/types';
import { formatCell, optionTone } from './format';

function errorTypeField(): FieldMeta {
  return {
    key: 'error_type',
    label: '异常类型',
    type: 'enum',
    visible: true,
    sortable: true,
    filterable: true,
    filterType: 'multiSelect',
    operators: ['in'],
    operatorLabels: { in: '属于' },
    options: [
      { value: 'protocol_error', label: '协议错误', tone: 'negative' },
      { value: 'cache_write_failed', label: '写入失败', tone: 'negative' },
      { value: 'late_data', label: '迟到数据', tone: 'negative' },
      { value: 'upload_timeout', label: '上报超时', tone: 'warning' },
    ],
    editable: false,
    required: false,
    widthStrategy: 'min',
  };
}

describe('状态 tone 的展示层收敛（C4）', () => {
  it('协议错误显示为 warning，而不是后端给的 negative', () => {
    const field = errorTypeField();
    expect(optionTone(field, 'protocol_error')).toBe('warning');
    expect(formatCell(field, 'protocol_error').tone).toBe('warning');
  });

  it('写入失败保持 danger（negative），迟到数据为 warning', () => {
    const field = errorTypeField();
    expect(optionTone(field, 'cache_write_failed')).toBe('negative');
    expect(optionTone(field, 'late_data')).toBe('warning');
  });

  it('未登记的取值沿用后端 tone', () => {
    const field = errorTypeField();
    expect(optionTone(field, 'upload_timeout')).toBe('warning');
  });

  it('其他字段不受映射影响（沿用后端 tone，不变成 warning）', () => {
    const field: FieldMeta = { ...errorTypeField(), key: 'rule_type' };
    expect(optionTone(field, 'protocol_error')).toBe('negative');
  });

  it('表格时间统一 YYYY-MM-DD HH:mm，秒与 +08:00 只留在 title（§16.5）', () => {
    const field: FieldMeta = {
      ...errorTypeField(),
      key: 'value_time',
      label: '取值时间',
      type: 'datetime',
      options: [],
    };
    const cell = formatCell(field, '2026-11-05T20:00:12Z');
    // UTC 20:00:12 → Asia/Shanghai 次日 04:00:12。
    expect(cell.text).toBe('2026-11-06 04:00');
    expect(cell.full).toBe('2026-11-06 04:00:12 +08:00');
  });
});
