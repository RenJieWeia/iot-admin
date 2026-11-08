import { LoaderCircle, TriangleAlert } from 'lucide-react';

/** 元数据未就绪时的整页挡板：加载中显示 spinner，失败显示原因 + 重试。 */
export default function MetaGate({
  loading,
  error,
  onRetry,
}: {
  loading: boolean;
  error: string | null;
  onRetry: () => void;
}) {
  return (
    <div
      className="flex min-h-screen flex-col items-center justify-center gap-4 bg-canvas px-6 text-center"
      role="status"
      aria-live="polite"
    >
      {loading ? (
        <>
          <LoaderCircle className="size-6 animate-spin text-primary" aria-hidden />
          <p className="text-sm text-muted">正在加载管理配置…</p>
        </>
      ) : (
        <>
          <TriangleAlert className="size-6 text-negative" aria-hidden />
          <div className="space-y-1">
            <p className="text-sm font-medium text-fg">管理配置加载失败</p>
            <p className="max-w-md break-words text-xs text-muted">{error ?? '未知错误'}</p>
          </div>
          <button
            type="button"
            onClick={onRetry}
            className="rounded-md border border-line bg-surface px-3 py-1.5 text-sm font-medium hover:bg-neutral-soft"
          >
            重试
          </button>
        </>
      )}
    </div>
  );
}
