import type { FieldMeta, Tone } from '../api/types';
import { useRelationLabels } from '../meta';
import { briefJson, formatDateTime, formatNumber, isBlank, optionLabel } from '../lib/format';
import { cn } from '../lib/cn';

/** 语义色令牌 → 状态标签底色，文字保留（颜色不是唯一信息）。 */
const TONE_CLASS: Record<Tone, string> = {
  positive: 'bg-positive-soft text-positive',
  negative: 'bg-negative-soft text-negative',
  warning: 'bg-warning-soft text-warning',
  neutral: 'bg-neutral-soft text-neutral',
};

export function StatusBadge({ label, tone }: { label: string; tone?: Tone }) {
  return (
    <span
      className={cn(
        'inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium whitespace-nowrap',
        TONE_CLASS[tone ?? 'neutral'],
      )}
    >
      {label}
    </span>
  );
}

/** 按字段元数据渲染单元格：类型映射唯一入口，列表与详情共用。 */
export function FieldValue({ field, value }: { field: FieldMeta; value: unknown }) {
  const relationLabel = useRelationLabels();
  if (isBlank(value)) return <span className="text-muted">—</span>;

  switch (field.type) {
    case 'enum': {
      const hit = optionLabel(field, value);
      if (!hit) return <span>{String(value)}</span>;
      return <StatusBadge label={hit.label} tone={hit.tone as Tone | undefined} />;
    }
    case 'boolean': {
      const on = value === true || value === 'true';
      const hit = field.options.find((option) => option.value === String(on));
      // 状态型布尔列（在线状态/启用）用 badge 展示；其它布尔列保持是/否文本。
      if ((field.key === 'online' || field.key === 'enabled') && hit) {
        return <StatusBadge label={hit.label} tone={hit.tone as Tone | undefined} />;
      }
      return <span className={on ? 'text-fg' : 'text-muted'}>{hit?.label ?? (on ? '是' : '否')}</span>;
    }
    case 'datetime':
      return <span className="tabular">{formatDateTime(value)}</span>;
    case 'number':
      return <span className="tabular">{formatNumber(value)}</span>;
    case 'relation':
      return <span>{relationLabel(field.ref, value)}</span>;
    case 'json':
      return <code className="text-xs text-muted">{briefJson(value)}</code>;
    case 'id':
      return <span className="font-mono text-xs text-muted tabular">{String(value)}</span>;
    default:
      return <span>{String(value)}</span>;
  }
}
