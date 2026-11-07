import { cva, type VariantProps } from 'class-variance-authority';
import type { Tone } from '../api/types';
import { cn } from '../lib/cn';

/**
 * 状态 tone 收敛为五档（C4）：success / warning / danger / info / neutral。
 * 后端 `_meta.options[].tone` 仍是 v1 词汇（positive/negative/warning/neutral），
 * 但某个值得的语义在这一层统一映射，页面不再散写颜色类。
 */
const tag = cva(
  'inline-block max-w-full overflow-hidden text-ellipsis whitespace-nowrap rounded-full px-2 text-sm leading-meta',
  {
    variants: {
      tone: {
        neutral: 'bg-surface-2 text-muted',
        success: 'bg-success-weak text-success',
        warning: 'bg-warning-weak text-warning',
        danger: 'bg-danger-weak text-danger',
        info: 'bg-info-weak text-info',
      },
    },
    defaultVariants: { tone: 'neutral' },
  },
);

/** v1 tone → 五档语义。 */
const TONE_MAP: Record<string, VariantProps<typeof tag>['tone']> = {
  neutral: 'neutral',
  positive: 'success',
  negative: 'danger',
  warning: 'warning',
  info: 'info',
  success: 'success',
  danger: 'danger',
};

interface Props {
  label: string;
  tone?: Tone | string;
  /** 悬浮显示完整文本（单元格截断时使用）。 */
  title?: string;
}

/** 状态标签 = 文字 + 颜色：颜色只是辅助，文字始终保留（§16 语义规范）。 */
export function StatusTag({ label, tone = 'neutral', title }: Props) {
  return (
    <span className={cn(tag({ tone: TONE_MAP[tone] ?? 'neutral' }))} title={title ?? label}>
      {label}
    </span>
  );
}
