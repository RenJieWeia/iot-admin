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
/**
 * 展示层 tone 收敛（C4 / task-014 §2 步 3）：
 * 后端 `_meta.eventTypeOptions` 把 protocol_error 标成 negative，但审计要求
 * 「协议错误→warning、写入失败→danger、迟到数据→warning」。本任务书不允许改 `_meta`，
 * 因此只在前端展示层做一张最小的 key+value 映射，其余取值沿用后端 tone。
 * 仅影响渲染，不参与筛选/排序，也不改接口。
 */
// 注意：这里用的是 v1 tone 词表（neutral/positive/negative/warning），
// StatusTag 再把它映射成五档语义（negative → danger、positive → success）。
const TONE_OVERRIDES: Record<string, Record<string, Tone>> = {
  error_type: {
    protocol_error: 'warning',
    cache_write_failed: 'negative',
    late_data: 'warning',
  },
};

/** 编码类文本列：用等宽字体展示（D2）。 */
const MONO_TEXT_KEYS = new Set(['terminal_id', 'item_code', 'gateway_id', 'code']);

/** 快照页的修正次数列（task-010 §4.3）。 */
const REVISION_FIELD_KEY = 'revision';
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

/** 表格里的时间：`YYYY-MM-DD HH:mm`（§16.5）；秒与 +08:00 只放进 title 的完整值。 */
export function formatTimeShort(raw: unknown): { text: string; full: string } {
  if (raw === null || raw === undefined || raw === '') return { text: EMPTY, full: '' };
  const full = formatTime(raw);
  if (full === EMPTY) return { text: EMPTY, full: '' };
  // full 形如 `2026-11-05 20:00:12 +08:00`：表格只到分钟，完整值留在 title。
  const match = /^(\d{4}-\d{2}-\d{2}) (\d{2}:\d{2}):\d{2}/.exec(full);
  if (!match) return { text: full, full };
  return { text: `${match[1]} ${match[2]}`, full };
}

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
  return TONE_OVERRIDES[field.key]?.[value] ?? toneOf(field, value);
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
  // revision 是快照页的「修正次数」：0/空显示 —，>0 显示数字并把完整说明放进 title。
  // 字段键是本批固定的契约（task-010 §4.3），与 quality 的 preset 一样集中一处。
  if (field.key === REVISION_FIELD_KEY && field.type === 'number') {
    if (raw === '') return { text: EMPTY, full: '', kind: 'muted', tone: 'neutral' };
    const times = Number(raw);
    if (!Number.isFinite(times)) return { text: String(raw), full: String(raw), kind: 'text', tone: 'neutral' };
    return {
      text: times === 0 ? EMPTY : String(times),
      full: times === 0 ? '未修正' : `已修正 ${times} 次`,
      kind: times === 0 ? 'muted' : 'text',
      tone: 'neutral',
    };
  }

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
      const { text, full } = formatTimeShort(raw);
      return { text, full, kind: text === EMPTY ? 'muted' : 'text', tone: 'neutral' };
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
    // 编码类文本列（终端/数据项/网关）用等宽字体，便于逐字符比对（D2）。
    default: {
      const text = raw === '' ? EMPTY : String(raw);
      const mono = MONO_TEXT_KEYS.has(field.key);
      return {
        text,
        full: text,
        kind: text === EMPTY ? 'muted' : mono ? 'mono' : 'text',
        tone: 'neutral',
      };
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
