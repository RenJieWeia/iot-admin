import { ChevronLeft, ChevronRight } from 'lucide-react';
import { cn } from '../lib/cn';
import { formatNumber } from '../lib/format';

interface PaginationProps {
  page: number;
  pageSize: number;
  total: number;
  options: number[];
  disabled?: boolean;
  onChange: (patch: { page?: number; pageSize?: number }) => void;
}

const buttonClass =
  'inline-flex h-8 min-w-8 items-center justify-center rounded-md border border-line px-2 text-xs text-fg hover:bg-surface-2 disabled:cursor-not-allowed disabled:opacity-40';

/** 页码窗口：首页/末页/当前页邻域，中间用省略号。 */
function pageWindow(current: number, total: number): (number | '…')[] {
  if (total <= 7) return Array.from({ length: total }, (_, index) => index + 1);
  const picked = new Set([1, total, current, current - 1, current + 1]);
  const pages = [...picked].filter((page) => page >= 1 && page <= total).sort((a, b) => a - b);
  const out: (number | '…')[] = [];
  let previous = 0;
  for (const page of pages) {
    if (previous && page - previous > 1) out.push('…');
    out.push(page);
    previous = page;
  }
  return out;
}

/** 偏移分页：总数 + 页码 + 每页条数（档位来自 meta.pagination.options）。 */
export function Pagination({ page, pageSize, total, options, disabled, onChange }: PaginationProps) {
  const totalPages = Math.max(1, Math.ceil(total / pageSize));
  const current = Math.min(Math.max(page, 1), totalPages);

  return (
    <div className="flex flex-wrap items-center justify-between gap-3 border-t border-line px-4 py-3 text-sm">
      <span className="text-muted">
        共 <span className="tabular text-fg">{formatNumber(total)}</span> 条
      </span>
      <div className="flex flex-wrap items-center gap-3">
        <label className="flex items-center gap-2 text-xs text-muted">
          每页
          <select
            value={pageSize}
            disabled={disabled}
            onChange={(event) => onChange({ pageSize: Number(event.target.value) })}
            className="h-8 rounded-md border border-line bg-surface px-1.5 text-xs text-fg"
          >
            {options.map((option) => (
              <option key={option} value={option}>
                {option}
              </option>
            ))}
          </select>
          条
        </label>
        <div className="flex items-center gap-1">
          <button
            type="button"
            className={buttonClass}
            disabled={disabled || current <= 1}
            onClick={() => onChange({ page: current - 1 })}
            aria-label="上一页"
          >
            <ChevronLeft className="size-3.5" aria-hidden />
          </button>
          {pageWindow(current, totalPages).map((item, index) =>
            item === '…' ? (
              <span key={`gap-${index}`} className="px-1 text-xs text-muted">
                …
              </span>
            ) : (
              <button
                key={item}
                type="button"
                className={cn(
                  buttonClass,
                  'tabular',
                  item === current && 'border-primary bg-primary-soft text-primary',
                )}
                disabled={disabled}
                aria-current={item === current ? 'page' : undefined}
                onClick={() => onChange({ page: item })}
              >
                {item}
              </button>
            ),
          )}
          <button
            type="button"
            className={buttonClass}
            disabled={disabled || current >= totalPages}
            onClick={() => onChange({ page: current + 1 })}
            aria-label="下一页"
          >
            <ChevronRight className="size-3.5" aria-hidden />
          </button>
        </div>
      </div>
    </div>
  );
}
