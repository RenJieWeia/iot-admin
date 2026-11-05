import styles from './Skeleton.module.css';

interface Props {
  /** 短条用于参差列宽，避免骨架看起来像实心色块。 */
  short?: boolean;
}

/**
 * 骨架条：只画条形，不做脉冲动画；外层 `td` 保持真实行高，
 * 保证加载态与就绪态布局完全一致（§6 布局稳定性）。
 */
export function SkeletonCell({ short = false }: Props) {
  return <span className={short ? `${styles.cell} ${styles.short}` : styles.cell} aria-hidden="true" />;
}
