import styles from './Toolbar.module.css';

interface Props {
  hasMore: boolean;
  loading: boolean;
  /** 后端下发的单批条数。 */
  limit: number;
  /** 当前页已渲染的行数。 */
  loaded: number;
  onMore: () => void;
}

/**
 * 游标分页器：只认 `hasMore`，**不显示总数**（分区表上 count(*) 会随月退化）。
 * 「加载更多」把 nextCursor 写进 URL，刷新/后退可以复原同一页。
 */
export function CursorPager({ hasMore, loading, limit, loaded, onMore }: Props) {
  return (
    <div className={styles.pagination}>
      <span className={styles.rangeText}>
        已加载 {loaded} 条 · 每批 {limit || '—'} 条（游标分页，不显示总数）
      </span>
      {hasMore ? (
        <button type="button" className="btn" onClick={onMore} disabled={loading}>
          {loading ? '加载中…' : '加载更多'}
        </button>
      ) : (
        <span className={styles.rangeText}>已到末尾</span>
      )}
    </div>
  );
}
