import type { ReactNode } from 'react';
import { StatePanel } from './StatePanel';

interface Props {
  title: string;
  /** 失败原因（后端 detail 原文）+ 恢复路径，不能只写「操作失败」。 */
  text: string;
  onRetry: () => void;
  retryLabel?: string;
  extra?: ReactNode;
}

/** 加载失败态：原因 + 重试（有时附「清除筛选」等恢复路径）。 */
export function ErrorState({ title, text, onRetry, retryLabel = '重试', extra }: Props) {
  return (
    <StatePanel
      title={title}
      text={text}
      tone="error"
      actions={
        <>
          <button type="button" className="btn" onClick={onRetry}>
            {retryLabel}
          </button>
          {extra}
        </>
      }
    />
  );
}
