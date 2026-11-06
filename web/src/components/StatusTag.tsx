import { cva, type VariantProps } from 'class-variance-authority';
import type { Tone } from '../api/types';
import { cn } from '../lib/cn';

/** 变体集中用 CVA：tone 的取值与 tokens.css 的语义色一一对应。 */
const tag = cva(
  'inline-block max-w-full overflow-hidden text-ellipsis whitespace-nowrap rounded-sm px-2 text-sm leading-lg',
  {
    variants: {
      tone: {
        neutral: 'bg-neutral-bg text-neutral-text',
        positive: 'bg-positive-bg text-positive-text',
        negative: 'bg-negative-bg text-negative-text',
        warning: 'bg-warning-bg text-warning-text',
      },
    },
    defaultVariants: { tone: 'neutral' },
  },
);

interface Props extends VariantProps<typeof tag> {
  label: string;
  /** 悬浮显示完整文本（单元格截断时使用）。 */
  title?: string;
}

/** 状态标签 = 文字 + 颜色：颜色只是辅助，文字始终保留（§16 语义规范）。 */
export function StatusTag({ label, tone, title }: Props) {
  return (
    <span className={cn(tag({ tone: tone as Tone | null | undefined }))} title={title ?? label}>
      {label}
    </span>
  );
}
