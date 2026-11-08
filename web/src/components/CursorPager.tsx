import { ChevronDown } from 'lucide-react';

interface CursorPagerProps {
  hasMore: boolean;
  loading: boolean;
  loaded: number;
  onLoadMore: () => void;
}

/** 游标分页：只能顺序加载，不可跳页（服务端 cursor 契约）。 */
export function CursorPager({ hasMore, loading, loaded, onLoadMore }: CursorPagerProps) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-3 border-t border-line px-4 py-3 text-sm">
      <span className="text-muted">
        已加载 <span className="tabular text-fg">{loaded}</span> 条 · 游标分页（不可跳页）
      </span>
      {hasMore ? (
        <button
          type="button"
          onClick={onLoadMore}
          disabled={loading}
          className="inline-flex items-center gap-1.5 rounded-md border border-line px-3 py-1.5 text-sm text-fg hover:bg-surface-2 disabled:cursor-not-allowed disabled:opacity-50"
        >
          <ChevronDown className={loading ? 'size-4 animate-bounce' : 'size-4'} aria-hidden />
          {loading ? '加载中…' : '加载更多'}
        </button>
      ) : (
        <span className="text-xs text-muted">没有更多了</span>
      )}
    </div>
  );
}
