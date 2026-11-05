import type { ReactNode } from 'react';
import styles from './Panel.module.css';

interface Props {
  title: string;
  /** 说明必须含原因与恢复路径（§16），不能只写「暂无数据」。 */
  text: string;
  actions?: ReactNode;
  tone?: 'neutral' | 'error';
}

export function StatePanel({ title, text, actions, tone = 'neutral' }: Props) {
  return (
    <div className={styles.state} role={tone === 'error' ? 'alert' : 'status'}>
      <h2 className={styles.stateTitle}>{title}</h2>
      <p className={styles.stateText}>{text}</p>
      {actions && <div className={styles.stateActions}>{actions}</div>}
    </div>
  );
}
