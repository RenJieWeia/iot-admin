import type { Tone } from '../api/types';
import styles from './StatusTag.module.css';

const TONE_CLASS: Record<Tone, string> = {
  neutral: styles.neutral ?? '',
  positive: styles.positive ?? '',
  negative: styles.negative ?? '',
  warning: styles.warning ?? '',
};

interface Props {
  label: string;
  tone?: Tone;
  /** 悬浮显示完整文本（单元格截断时使用）。 */
  title?: string;
}

/** 状态标签 = 文字 + 颜色：颜色只是辅助，文字始终保留（§16 语义规范）。 */
export function StatusTag({ label, tone = 'neutral', title }: Props) {
  return (
    <span className={`${styles.tag} ${TONE_CLASS[tone]}`} title={title ?? label}>
      {label}
    </span>
  );
}
