import type { ReactNode } from 'react';
import type { FieldMeta, RowData } from '../api/types';
import { cn } from '../lib/cn';
import { isBlank } from '../lib/format';
import type { CardDensity } from '../lib/viewPrefs';
import { FieldValue } from './FieldValue';
import { EmptyState, ErrorState, Skeleton } from './States';

interface CardGridProps {
  fields: FieldMeta[];
  rows: RowData[];
  loading: boolean;
  error: string | null;
  density: CardDensity;
  onRowClick: (row: RowData) => void;
  onRetry?: () => void;
  onReset?: () => void;
  /** 卡片右上角的操作（如网关启用开关）。 */
  renderActions?: (row: RowData) => ReactNode;
  /** 卡片级右键：由调用方弹对象操作菜单。 */
  onRowContextMenu?: (event: React.MouseEvent<HTMLElement>, row: RowData) => void;
}

/** 卡片视图：字段与表格共用 _meta 的 visible 列，点卡片等同点行。 */
export function CardGrid({
  fields,
  rows,
  loading,
  error,
  density,
  onRowClick,
  onRetry,
  onReset,
  renderActions,
  onRowContextMenu,
}: CardGridProps) {
  const grid =
    density === 'compact'
      ? 'grid-cols-[repeat(auto-fill,minmax(220px,1fr))]'
      : 'grid-cols-[repeat(auto-fill,minmax(300px,1fr))]';

  if (error) {
    return (
      <div className="p-4">
        <ErrorState message={error} onRetry={onRetry} />
      </div>
    );
  }

  if (loading && rows.length === 0) {
    return (
      <div className={cn('grid gap-3 p-4', grid)}>
        {Array.from({ length: 6 }, (_, index) => (
          <div key={index} className="rounded-lg border border-line bg-surface p-3">
            <Skeleton className="h-4 w-1/2" />
            <Skeleton className="mt-3 h-3 w-3/4" />
            <Skeleton className="mt-2 h-3 w-2/3" />
          </div>
        ))}
      </div>
    );
  }

  if (rows.length === 0) {
    return (
      <div className="p-4">
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
      </div>
    );
  }

  return (
    <div className={cn('grid gap-3 p-4', grid)}>
      {rows.map((row) => {
        const titleField = fields.find((field) => field.key === 'name') ?? fields[0];
        const title = titleField ? row[titleField.key] : undefined;
        // 副标题用首列（通常是主键）时，正文不再重复展示同一字段。
        const subtitleField = titleField && titleField.key !== 'id' ? fields[0] : undefined;
        const rest = fields.filter(
          (field) => field.key !== titleField?.key && field.key !== subtitleField?.key,
        );
        return (
          <div
            key={String(row.id ?? JSON.stringify(row))}
            tabIndex={0}
            role="button"
            aria-label="查看详情"
            onClick={() => onRowClick(row)}
            onContextMenu={(event) => {
              if (!onRowContextMenu) return;
              event.preventDefault();
              event.stopPropagation();
              onRowContextMenu(event, row);
            }}
            onKeyDown={(event) => {
              if (event.key === 'Enter' || event.key === ' ') {
                event.preventDefault();
                onRowClick(row);
              }
            }}
            className={cn(
              'group relative flex cursor-pointer flex-col rounded-lg border border-line bg-surface p-3',
              'transition-colors hover:border-line-strong hover:bg-surface-2 focus-visible:bg-surface-2',
              loading && 'opacity-60',
            )}
          >
            <div className="flex items-start justify-between gap-2">
              <div className="min-w-0">
                <p className="truncate text-sm font-medium text-fg" title={isBlank(title) ? '' : String(title)}>
                  {isBlank(title) ? '—' : <FieldValue field={titleField!} value={title} />}
                </p>
                {subtitleField ? (
                  <p className="mt-0.5 truncate font-mono text-xs text-muted">
                    <FieldValue field={subtitleField} value={row[subtitleField.key]} />
                  </p>
                ) : null}
              </div>
              {renderActions ? (
                <div className="shrink-0" onClick={(event) => event.stopPropagation()}>
                  {renderActions(row)}
                </div>
              ) : null}
            </div>
            <dl className="mt-3 space-y-1.5 text-xs">
              {rest.map((field) => (
                <div key={field.key} className="flex items-start justify-between gap-3">
                  <dt className="shrink-0 text-muted">{field.label}</dt>
                  <dd className="min-w-0 truncate text-right text-fg">
                    <FieldValue field={field} value={row[field.key]} />
                  </dd>
                </div>
              ))}
            </dl>
          </div>
        );
      })}
    </div>
  );
}
