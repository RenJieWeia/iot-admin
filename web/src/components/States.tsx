import type { ReactNode } from 'react';
import { Inbox, RotateCw, TriangleAlert } from 'lucide-react';
import { cn } from '../lib/cn';

/** 骨架占位：尺寸由调用方给，必须贴近真实内容高度。 */
export function Skeleton({ className }: { className?: string }) {
  return <div className={cn('animate-pulse rounded bg-neutral-soft', className)} aria-hidden />;
}

export function EmptyState({
  title = '暂无数据',
  hint,
  action,
}: {
  title?: string;
  hint?: ReactNode;
  action?: ReactNode;
}) {
  return (
    <div className="flex flex-col items-center gap-2 px-6 py-14 text-center">
      <Inbox className="size-8 text-muted/60" aria-hidden />
      <p className="text-sm font-medium text-fg">{title}</p>
      {hint ? <p className="max-w-md text-xs leading-relaxed text-muted">{hint}</p> : null}
      {action ? <div className="mt-2">{action}</div> : null}
    </div>
  );
}

export function ErrorState({
  message,
  onRetry,
}: {
  message: string;
  onRetry?: () => void;
}) {
  return (
    <div className="flex flex-col items-center gap-2 px-6 py-12 text-center">
      <TriangleAlert className="size-8 text-negative" aria-hidden />
      <p className="text-sm font-medium text-fg">加载失败</p>
      <p className="max-w-lg break-words text-xs leading-relaxed text-muted">{message}</p>
      {onRetry ? (
        <button
          type="button"
          onClick={onRetry}
          className="mt-2 inline-flex items-center gap-1.5 rounded-md border border-line px-3 py-1.5 text-sm text-fg hover:bg-surface-2"
        >
          <RotateCw className="size-3.5" aria-hidden />
          重试
        </button>
      ) : null}
    </div>
  );
}
