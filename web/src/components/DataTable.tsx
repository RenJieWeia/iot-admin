import { ChevronDown, ChevronUp, ChevronsUpDown } from 'lucide-react';
import type { FieldMeta, RowData } from '../api/types';
import { cn } from '../lib/cn';
import { FieldValue } from './FieldValue';
import { EmptyState, ErrorState, Skeleton } from './States';

interface DataTableProps {
  fields: FieldMeta[];
  rows: RowData[];
  loading: boolean;
  error: string | null;
  sort: string;
  order: 'asc' | 'desc';
  skeletonRows?: number;
  /** 游标页排序由服务端固定，表头不可点。 */
  disableSort?: boolean;
  onSortChange: (key: string) => void;
  onRowClick: (row: RowData) => void;
  onRetry?: () => void;
  onReset?: () => void;
}

/** 列宽从 field.width 取：fixed/min 走像素，flex 吃剩余宽度；超宽横向滚动不压缩。 */
export function DataTable({
  fields,
  rows,
  loading,
  error,
  sort,
  order,
  skeletonRows = 8,
  disableSort = false,
  onSortChange,
  onRowClick,
  onRetry,
  onReset,
}: DataTableProps) {
  const fixedWidth = fields.reduce(
    (sum, field) => sum + (field.widthStrategy === 'flex' ? 0 : (field.width ?? 120)),
    0,
  );
  const flexCount = fields.filter((field) => field.widthStrategy === 'flex').length;
  const minWidth = fixedWidth + flexCount * 180;
  const showSkeleton = loading && rows.length === 0;
  const colSpan = Math.max(fields.length, 1);

  return (
    <div className="overflow-x-auto" aria-busy={loading || undefined}>
      <table
        className="w-full table-fixed border-collapse text-sm"
        style={{ minWidth: minWidth || undefined }}
      >
        <colgroup>
          {fields.map((field) => (
            <col
              key={field.key}
              style={
                field.widthStrategy === 'flex' ? undefined : { width: `${field.width ?? 120}px` }
              }
            />
          ))}
        </colgroup>
        <thead>
          <tr className="bg-surface-2 text-xs font-medium text-muted">
            {fields.map((field) => {
              const sortable = field.sortable && !disableSort;
              const active = sortable && sort === field.key;
              return (
                <th
                  key={field.key}
                  scope="col"
                  aria-sort={active ? (order === 'asc' ? 'ascending' : 'descending') : 'none'}
                  className="border-b border-line px-3 py-2.5 text-left font-medium"
                >
                  {sortable ? (
                    <button
                      type="button"
                      onClick={() => onSortChange(field.key)}
                      title={`按${field.label}排序`}
                      className={cn(
                        'group -mx-1 inline-flex max-w-full items-center gap-1 rounded px-1 hover:text-fg',
                        active && 'text-fg',
                      )}
                    >
                      <span className="whitespace-nowrap">{field.label}</span>
                      {/* 未激活列不占图标宽度：列宽来自 _meta（id 列 80px），放个隐形图标会把表头挤成省略号。 */}
                      {active ? (
                        order === 'asc' ? (
                          <ChevronUp className="size-3.5 shrink-0" aria-hidden />
                        ) : (
                          <ChevronDown className="size-3.5 shrink-0" aria-hidden />
                        )
                      ) : (
                        <ChevronsUpDown
                          className="size-3.5 shrink-0 opacity-0 transition-opacity group-hover:opacity-60"
                          aria-hidden
                        />
                      )}
                    </button>
                  ) : (
                    <span className="whitespace-nowrap">{field.label}</span>
                  )}
                </th>
              );
            })}
          </tr>
        </thead>
        <tbody>
          {showSkeleton
            ? Array.from({ length: skeletonRows }, (_, rowIndex) => (
                <tr key={`skeleton-${rowIndex}`} className="border-b border-line last:border-b-0">
                  {fields.map((field, cellIndex) => (
                    <td key={field.key} className="px-3 py-2.5">
                      <Skeleton
                        className={cn('h-4', cellIndex === 0 ? 'w-3/5' : 'w-4/5')}
                      />
                    </td>
                  ))}
                </tr>
              ))
            : null}
          {!showSkeleton && error ? (
            <tr>
              <td colSpan={colSpan}>
                <ErrorState message={error} onRetry={onRetry} />
              </td>
            </tr>
          ) : null}
          {!showSkeleton && !error && rows.length === 0 ? (
            <tr>
              <td colSpan={colSpan}>
                <EmptyState
                  title="没有符合条件的数据"
                  hint="调整搜索或筛选条件后重试；也可以直接刷新列表。"
                  action={
                    onReset ? (
                      <button
                        type="button"
                        onClick={onReset}
                        className="rounded-md border border-line px-3 py-1.5 text-sm text-fg hover:bg-surface-2"
                      >
                        清空筛选
                      </button>
                    ) : null
                  }
                />
              </td>
            </tr>
          ) : null}
          {!showSkeleton && !error
            ? rows.map((row, index) => (
                <tr
                  key={String(row.id ?? index)}
                  tabIndex={0}
                  aria-label="查看详情"
                  onClick={() => onRowClick(row)}
                  onKeyDown={(event) => {
                    if (event.key === 'Enter' || event.key === ' ') {
                      event.preventDefault();
                      onRowClick(row);
                    }
                  }}
                  className={cn(
                    'cursor-pointer border-b border-line transition-colors last:border-b-0',
                    'hover:bg-surface-2 focus-visible:bg-surface-2',
                    loading && rows.length > 0 && 'opacity-60',
                  )}
                >
                  {fields.map((field) => {
                    const raw = row[field.key];
                    const title =
                      typeof raw === 'string' || typeof raw === 'number' ? String(raw) : undefined;
                    return (
                      <td key={field.key} className="px-3 py-2.5 align-middle">
                        <div className="truncate" title={title}>
                          <FieldValue field={field} value={raw} />
                        </div>
                      </td>
                    );
                  })}
                </tr>
              ))
            : null}
        </tbody>
      </table>
    </div>
  );
}
