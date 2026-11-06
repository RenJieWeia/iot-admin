import { useMemo } from 'react';
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
} from '@tanstack/react-table';
import type { FieldMeta, RowData } from '../api/types';
import type { WidthStrategy } from '../api/types';
import { cn } from '../lib/cn';
import { formatCell } from '../lib/format';
import { SkeletonCell } from './Skeleton';
import { StatusTag } from './StatusTag';

export type TableState = 'loading' | 'ready';

/** widthStrategy → 列宽类名：宽度集中在 tailwind.css 的 @utility，TSX 不出现 arbitrary value。 */
const COLUMN_CLASS: Record<WidthStrategy, string> = {
  fixed: 'col-fixed',
  min: 'col-min',
  flex: 'col-flex',
};

interface Props {
  fields: FieldMeta[];
  rows: RowData[];
  state: TableState;
  sort: string;
  order: 'asc' | 'desc';
  onSort: (field: string) => void;
  /** 关联列的 value→label 映射，键是字段名。 */
  relationOptions: Record<string, Record<string, string>>;
  /** 只读表（异常两页）不渲染操作列。 */
  writable: boolean;
  /**
   * 是否提供排序入口。游标分页页（快照）后端固定排序，传 sort/order 会 400，
   * 因此不渲染排序按钮，只保留默认排序列的静态方向标识。
   */
  sortable?: boolean;
  /** 第二个参数是触发按钮：抽屉关闭后焦点要回到它。 */
  onEdit?: (row: RowData, trigger: HTMLButtonElement) => void;
  onDelete?: (row: RowData, trigger: HTMLButtonElement) => void;
  skeletonRows?: number;
  /** 表格说明：只讲使用信息（总数与页号），不出现实现细节。 */
  caption: string;
}

/**
 * 元数据驱动的表格：列、标签颜色、格式化全部来自 FieldMeta；
 * 排序与分页由查询层发出语义化事件后交给后端，这里不做任何本地过滤（table-query §1、§7）。
 */
export function DataTable({
  fields,
  rows,
  state,
  sort,
  order,
  onSort,
  relationOptions,
  writable,
  sortable = true,
  onEdit,
  onDelete,
  skeletonRows = 10,
  caption,
}: Props) {
  const columns = useMemo<ColumnDef<RowData>[]>(() => {
    const fieldColumns: ColumnDef<RowData>[] = fields.map((field) => ({
      id: field.key,
      header: field.label,
      accessorFn: (row: RowData) => row[field.key],
      cell: ({ row }) => {
        const view = formatCell(field, row.original[field.key], relationOptions[field.key] ?? {});
        if (view.kind === 'tag') {
          return <StatusTag label={view.text} tone={view.tone} title={view.full} />;
        }
        // 单行截断靠 CSS，完整值放 title；单元格永不换行，行高稳定（§6）。
        const className = cn(
          'block overflow-hidden text-ellipsis whitespace-nowrap',
          view.kind === 'mono' && 'font-mono text-sm',
          view.kind === 'muted' && 'text-muted',
        );
        return (
          <span className={className} title={view.full}>
            {view.text}
          </span>
        );
      },
    }));
    if (!writable) return fieldColumns;
    return [
      ...fieldColumns,
      {
        id: '__actions',
        header: '操作',
        enableSorting: false,
        cell: ({ row }) => (
          <span className="inline-flex items-center justify-end gap-2">
            <button
              type="button"
              className="inline-flex h-8 items-center rounded-control border border-border bg-surface px-3 text-body text-text hover:border-border-strong"
              data-action="edit"
              data-row-id={String(row.original.id ?? '')}
              onClick={(event) => onEdit?.(row.original, event.currentTarget)}
            >
              编辑
            </button>
            <button
              type="button"
              className="inline-flex h-8 items-center rounded-control px-2 text-body text-muted hover:bg-danger-weak hover:text-danger"
              data-action="delete"
              data-row-id={String(row.original.id ?? '')}
              onClick={(event) => onDelete?.(row.original, event.currentTarget)}
            >
              删除
            </button>
          </span>
        ),
      },
    ];
  }, [fields, relationOptions, writable, onEdit, onDelete]);

  const table = useReactTable({
    data: rows,
    columns,
    getCoreRowModel: getCoreRowModel(),
    manualPagination: true,
    manualSorting: true,
  });

  const headerGroups = table.getHeaderGroups();
  const skeletonCount = Math.min(skeletonRows, 10);

  return (
    <div className="overflow-x-auto rounded-md border border-border bg-surface">
      {state === 'loading' && (
        // 加载态必须有可读文案（四态文案互不相同），视觉上仍是与真实行高一致的骨架。
        <p className="visually-hidden" role="status">
          正在加载列表…
        </p>
      )}
      <table className="table-fixed w-max min-w-full border-collapse">
        <caption className="pb-2 text-left text-sm text-muted">{caption}</caption>
        <colgroup>
          {fields.map((field) => (
            <col key={field.key} className={COLUMN_CLASS[field.widthStrategy]} />
          ))}
          {writable && <col className="col-actions" />}
        </colgroup>
        <thead>
          {headerGroups.map((group) => (
            <tr key={group.id}>
              {group.headers.map((header) => {
                const field = fields.find((f) => f.key === header.id);
                const canSort = sortable && Boolean(field?.sortable);
                const active = field?.key === sort;
                const ariaSort = active ? (order === 'asc' ? 'ascending' : 'descending') : 'none';
                return (
                  <th
                    key={header.id}
                    scope="col"
                    className="h-11 whitespace-nowrap border-b border-border bg-surface-2 px-3 text-left text-meta font-medium text-muted"
                    aria-sort={active ? ariaSort : canSort ? 'none' : undefined}
                  >
                    {canSort && field ? (
                      <button
                        type="button"
                        className="inherit-font inline-flex min-h-11 cursor-pointer items-center gap-1 border-0 bg-transparent p-0"
                        onClick={() => onSort(field.key)}
                        aria-label={`按${field.label}排序`}
                      >
                        {field.label}
                        <span className="text-sm" aria-hidden="true">
                          {active ? (order === 'asc' ? '↑' : '↓') : '↕'}
                        </span>
                      </button>
                    ) : (
                      <>
                        {flexRender(header.column.columnDef.header, header.getContext())}
                        {active && (
                          // 服务端固定排序：只给方向标识，不做可点击入口。
                          <span className="text-sm" aria-hidden="true">
                            {order === 'asc' ? ' ↑' : ' ↓'}
                          </span>
                        )}
                      </>
                    )}
                  </th>
                );
              })}
            </tr>
          ))}
        </thead>
        <tbody>
          {state === 'loading'
            ? Array.from({ length: skeletonCount }).map((_, rowIndex) => (
                <tr key={`skeleton-${rowIndex}`} aria-hidden="true">
                  {fields.map((field, cellIndex) => (
                    <td
                      key={field.key}
                      className="h-11 overflow-hidden border-b border-border px-3 align-middle"
                    >
                      <SkeletonCell short={cellIndex % 3 === 2} />
                    </td>
                  ))}
                  {writable && (
                    <td className="h-11 overflow-hidden border-b border-border px-3 align-middle">
                      <SkeletonCell />
                    </td>
                  )}
                </tr>
              ))
            : table.getRowModel().rows.map((row) => (
                <tr key={String(row.original.id ?? row.id)} className="group hover:bg-surface-2">
                  {row.getVisibleCells().map((cell) => (
                    <td
                      key={cell.id}
                      className={cn(
                        'h-11 overflow-hidden border-b border-border px-3 align-middle',
                        cell.column.id === fields[0]?.key &&
                          'sticky left-0 z-10 bg-surface group-hover:bg-surface-2',
                        cell.column.id === '__actions' &&
                          'sticky right-0 z-10 whitespace-nowrap bg-surface text-right group-hover:bg-surface-2',
                      )}
                    >
                      {flexRender(cell.column.columnDef.cell, cell.getContext())}
                    </td>
                  ))}
                </tr>
              ))}
        </tbody>
      </table>
    </div>
  );
}
