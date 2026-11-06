import type { FieldMeta, Option, Tone } from '../api/types';
import { labelOf, toneOf } from './fieldMeta';

export type CellKind = 'text' | 'tag' | 'mono' | 'muted' | 'json';

export interface CellView {
  text: string;
  /** 完整值（悬浮 title 与复制用），与 text 不同的场景是截断后的展示。 */
  full: string;
  kind: CellKind;
  tone: Tone;
}

const TIME_ZONE = 'Asia/Shanghai';
const OFFSET_SUFFIX = '+08:00';
const EMPTY = '—';

const timeFormatter = new Intl.DateTimeFormat('zh-CN', {
  timeZone: TIME_ZONE,
  year: 'numeric',
  month: '2-digit',
  day: '2-digit',
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
  hour12: false,
});

/** 时间一律显示绝对时间并带时区（§16 语义规范），不用相对时间。 */
export function formatTime(raw: unknown): string {
  if (raw === null || raw === undefined || raw === '') return EMPTY;
  if (typeof raw === 'string' && /^\[/.test(raw)) return raw; // tstzrange 文本原样展示
  const date = new Date(String(raw));
  if (Number.isNaN(date.getTime())) return String(raw);
  const parts = timeFormatter.formatToParts(date);
  const pick = (type: string) => parts.find((p) => p.type === type)?.value ?? '';
  return `${pick('year')}-${pick('month')}-${pick('day')} ${pick('hour')}:${pick('minute')}:${pick('second')} ${OFFSET_SUFFIX}`;
}

export function formatNumber(raw: unknown): string {
  if (raw === null || raw === undefined || raw === '') return EMPTY;
  const n = Number(raw);
  if (!Number.isFinite(n)) return String(raw);
  return n.toLocaleString('zh-CN');
}

function compactJSON(raw: unknown): string {
  if (typeof raw === 'string') {
    try {
      return JSON.stringify(JSON.parse(raw));
    } catch {
      return raw;
    }
  }
  if (typeof raw === 'object' && raw !== null) return JSON.stringify(raw);
  return String(raw);
}

export function optionLabel(field: FieldMeta, value: string): string {
  return labelOf(field, value);
}

export function optionTone(field: FieldMeta, value: string): Tone {
  return toneOf(field, value);
}

/**
 * 单元格按字段元数据渲染：状态=文字+颜色（枚举/布尔），关系=名称，时间=绝对时间，
 * 空值统一 `—`。截断只在 CSS 做，完整值放 title，保证不换行撑高行高（§6）。
 */
export function formatCell(
  field: FieldMeta,
  value: unknown,
  relationLabels: Record<string, string> = {},
): CellView {
  const raw = value === null || value === undefined ? '' : value;
  // 带 options 的字段（枚举/布尔/带语义的数值，如 quality）一律渲染成「文字 + 颜色」标签，
  // 取值与色调全部来自 _meta，前端不硬编码文案。
  if (field.options && field.options.length > 0) {
    if (raw === '') return { text: EMPTY, full: '', kind: 'muted', tone: 'neutral' };
    const key = String(raw);
    const label = optionLabel(field, key);
    const tone = optionTone(field, key);
    return { text: label, full: label, kind: 'tag', tone };
  }
  switch (field.type) {
    case 'boolean': {
      const text = raw === '' ? EMPTY : optionLabel(field, String(raw));
      return {
        text,
        full: text,
        kind: raw === '' ? 'muted' : 'tag',
        tone: raw === '' ? 'neutral' : optionTone(field, String(raw)),
      };
    }
    case 'enum': {
      const parts = String(raw)
        .split(',')
        .map((v) => v.trim())
        .filter(Boolean);
      const text = parts.length ? parts.map((v) => optionLabel(field, v)).join('、') : EMPTY;
      const tone: Tone = parts.length ? optionTone(field, parts[0] ?? '') : 'neutral';
      return { text, full: text, kind: parts.length ? 'tag' : 'muted', tone };
    }
    case 'relation': {
      const id = String(raw);
      if (!id) return { text: EMPTY, full: '', kind: 'muted', tone: 'neutral' };
      const label = relationLabels[id];
      return { text: label ?? id, full: label ?? id, kind: 'text', tone: 'neutral' };
    }
    case 'datetime': {
      const text = formatTime(raw);
      return { text, full: text, kind: text === EMPTY ? 'muted' : 'text', tone: 'neutral' };
    }
    case 'number': {
      const text = formatNumber(raw);
      return { text, full: text, kind: text === EMPTY ? 'muted' : 'mono', tone: 'neutral' };
    }
    case 'json': {
      const text = raw === '' ? EMPTY : compactJSON(raw);
      return { text, full: text, kind: text === EMPTY ? 'muted' : 'json', tone: 'neutral' };
    }
    case 'id': {
      const text = raw === '' ? EMPTY : String(raw);
      return { text, full: text, kind: text === EMPTY ? 'muted' : 'mono', tone: 'neutral' };
    }
    default: {
      const text = raw === '' ? EMPTY : String(raw);
      return { text, full: text, kind: text === EMPTY ? 'muted' : 'text', tone: 'neutral' };
    }
  }
}

/** 筛选标签显示值：枚举/布尔转中文，其余原样，多值用「、」连接。 */
export function formatFilterValue(
  field: FieldMeta,
  values: string[],
  relationLabels: Record<string, string> = {},
): string {
  if (values.length === 0) return '';
  const shown = values.map((value) => {
    if (field.filterType === 'boolean' || field.filterType === 'multiSelect') {
      return optionLabel(field, value);
    }
    if (field.filterType === 'relation' || field.filterType === 'remoteSelect') {
      return relationLabels[value] ?? value;
    }
    if (field.type === 'datetime') return formatTime(value);
    return value;
  });
  return shown.join('、');
}

/**
 * 操作符中文来自字段元数据（FieldMeta.operatorLabels，后端 fields.go 的 OperatorLabel），
 * 前端只保留一个兜底：字段没声明时原样显示操作符，绝不自己发明中文。
 */
export function operatorLabel(field: FieldMeta | undefined, op: string): string {
  if (!field) return op;
  return field.operatorLabels?.[op] ?? op;
}

export function optionMatches(option: Option, keyword: string): boolean {
  if (!keyword) return true;
  const k = keyword.trim().toLowerCase();
  return option.label.toLowerCase().includes(k) || option.value.toLowerCase().includes(k);
}

/** 表单值 → 提交值：布尔转 true/false，数值转数字，空串按 null（可空列）。 */
export function toSubmitValue(field: FieldMeta, raw: string): unknown {
  const trimmed = raw.trim();
  if (trimmed === '') return null;
  if (field.type === 'number') {
    const n = Number(trimmed);
    return Number.isFinite(n) ? n : trimmed;
  }
  if (field.type === 'boolean') return trimmed === 'true';
  if (field.type === 'json') {
    try {
      return JSON.parse(trimmed);
    } catch {
      return trimmed;
    }
  }
  return trimmed;
}

/** 行值 → 表单字符串。 */
export function toFormValue(value: unknown): string {
  if (value === null || value === undefined) return '';
  if (typeof value === 'object') {
    try {
      return JSON.stringify(value);
    } catch {
      return String(value);
    }
  }
  return String(value);
}
