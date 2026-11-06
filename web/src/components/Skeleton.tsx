import { cn } from '../lib/cn';

interface Props {
  /** 短条用于参差列宽，避免骨架看起来像实心色块。 */
  short?: boolean;
}

/**
 * 骨架条：只画条形，不做脉冲动画；外层单元格保持真实行高，
 * 保证加载态与就绪态布局完全一致（§6 布局稳定性）。
 */
export function SkeletonCell({ short = false }: Props) {
  return <span className={cn('block h-3 rounded-sm bg-skeleton', short && 'w-3/5')} aria-hidden="true" />;
}
