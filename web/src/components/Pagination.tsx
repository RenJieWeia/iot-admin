import type { ListMeta } from '../api/types';
import type { PageSize } from '../lib/queryState';

interface Props {
  meta: ListMeta;
  rangeText: string;
  outOfRange: boolean;
  /** 每页条数档位来自 _meta.pagination.options（默认 10 / 上限 50）。 */
  pageSizeOptions: number[];
  onPage: (page: number) => void;
  onPageSize: (size: PageSize) => void;
}

/** 分页：范围 + 总数 + 上/下页 + 每页条数；变化一律回第一页（table-query §6）。 */
export function Pagination({ meta, rangeText, outOfRange, pageSizeOptions, onPage, onPageSize }: Props) {
  const total = meta.total;
  const hasPrev = meta.page > 1 && total > 0;
  const hasNext = meta.page * meta.pageSize < total;

  return (
    <>
      {outOfRange && (
        <p
          className="my-2 rounded-control border border-warning bg-warning-weak px-3 py-2 text-warning"
          role="status"
        >
          当前页超出范围：第 {meta.page} 页已无数据，共 {total} 条。筛选条件已保留，请返回有效页或清除筛选。
        </p>
      )}
      <div className="mt-4 flex flex-wrap items-center justify-end gap-3">
        <span className="range-text text-muted">{rangeText}</span>
        <button type="button" className="btn h-8" disabled={!hasPrev} onClick={() => onPage(meta.page - 1)}>
          上一页
        </button>
        <span aria-current="page" className="range-text text-muted">
          第 {meta.page} 页
        </span>
        <button type="button" className="btn h-8" disabled={!hasNext} onClick={() => onPage(meta.page + 1)}>
          下一页
        </button>
        <label className="inline-flex items-center gap-2 text-muted">
          每页
          <select
            className="h-8 rounded-control border border-border bg-surface px-2"
            value={meta.pageSize}
            onChange={(event) => onPageSize(Number(event.target.value) as PageSize)}
          >
            {pageSizeOptions.map((size) => (
              <option key={size} value={size}>
                {size} 条
              </option>
            ))}
          </select>
        </label>
      </div>
    </>
  );
}
