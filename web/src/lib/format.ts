import type { FieldMeta, RowData } from '../api/types';

export function isBlank(value: unknown): boolean {
  return value === null || value === undefined || value === '';
}

export function formatDateTime(value: unknown): string {
  if (isBlank(value)) return '—';
  const date = value instanceof Date ? value : new Date(String(value));
  if (Number.isNaN(date.getTime())) return String(value);
  const pad = (n: number) => String(n).padStart(2, '0');
  return (
    `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ` +
    `${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`
  );
}

export function formatNumber(value: unknown): string {
  if (isBlank(value)) return '—';
  const num = typeof value === 'number' ? value : Number(value);
  if (Number.isNaN(num)) return String(value);
  return new Intl.NumberFormat('zh-CN', { maximumFractionDigits: 4 }).format(num);
}

/** 简要 JSON 预览：列表列里最多 60 字符，抽屉里完整展示。 */
export function briefJson(value: unknown): string {
  if (isBlank(value)) return '—';
  const text = typeof value === 'string' ? value : JSON.stringify(value);
  return text.length > 60 ? `${text.slice(0, 60)}…` : text;
}

export function optionLabel(
  field: FieldMeta,
  value: unknown,
): { label: string; tone?: string } | null {
  if (isBlank(value)) return null;
  const raw = String(value);
  const hit = field.options.find((option) => option.value === raw);
  return hit ? { label: hit.label, tone: hit.tone } : null;
}

/** 可写字段的空值语义：必填不给默认值，避免"看起来填了其实没填"。 */
export function emptyValue(field: FieldMeta): unknown {
  switch (field.type) {
    case 'boolean':
      return false;
    case 'number':
      return '';
    default:
      return '';
  }
}

/** 表单提交前把字符串输入还原成后端列类型。 */
export function coerceForSubmit(field: FieldMeta, raw: unknown): unknown {
  if (raw === '' || raw === undefined || raw === null) return raw === '' ? null : raw;
  switch (field.type) {
    case 'number':
      return typeof raw === 'number' ? raw : Number(raw);
    case 'boolean':
      return typeof raw === 'boolean' ? raw : raw === 'true';
    case 'json':
      if (typeof raw !== 'string') return raw;
      try {
        return JSON.parse(raw);
      } catch {
        throw new Error(`${field.label} 不是合法 JSON`);
      }
    default:
      return raw;
  }
}

export function rowTitle(entityTitle: string, row: RowData): string {
  const keys = ['name', 'code', 'id'];
  for (const key of keys) {
    const value = row[key];
    if (!isBlank(value)) return `${entityTitle} · ${String(value)}`;
  }
  return entityTitle;
}
