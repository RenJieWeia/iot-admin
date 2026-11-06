import type { ReactNode } from 'react';
import { cn } from '../lib/cn';

interface Props {
  title: string;
  /** 说明必须含原因与恢复路径（§16），不能只写「暂无数据」。 */
  text: string;
  actions?: ReactNode;
  tone?: 'neutral' | 'error';
}

export function StatePanel({ title, text, actions, tone = 'neutral' }: Props) {
  return (
    <div
      className="flex flex-col items-start gap-2 rounded-md border border-border bg-surface p-6"
      role={tone === 'error' ? 'alert' : 'status'}
    >
      <h2 className="m-0 text-lg font-semibold">{title}</h2>
      <p className={cn('m-0 text-muted state-text')}>{text}</p>
      {actions && <div className="mt-2 flex gap-2">{actions}</div>}
    </div>
  );
}
