import { useMemo } from 'react';
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
} from '@tanstack/react-table';
import type { FieldMeta, RowData } from '../api/types';
import type { WidthStrategy } from '../api/types';
import { formatCell } from '../lib/format';
import { SkeletonCell } from './Skeleton';
import { StatusTag } from './StatusTag';
import styles from './DataTable.module.css';

export type TableState = 'loading' | 'ready';

/** widthStrategy → 列宽类名：宽度写死在 CSS 里，避免内联 style（CSP 不允许）。 */
const COLUMN_CLASS: Record<WidthStrategy, string> = {
  fixed: styles.colFixed ?? '',
  min: styles.colMin ?? '',
  flex: styles.colFlex ?? '',
};

interface Props {
  title: string;
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
  /** 第二个参数是触发按钮：抽屉关闭后焦点要回到它。 */
  onEdit?: (row: RowData, trigger: HTMLButtonElement) => void;
  onDelete?: (row: RowData, trigger: HTMLButtonElement) => void;
  skeletonRows?: number;
}

/**
 * 元数据驱动的表格：列、标签颜色、格式化全部来自 FieldMeta；
 * 排序/分页由服务端执行，这里只发语义化事件，不做任何本地过滤（table-query §1、§7）。
 */
export function DataTable({
  title,
  fields,
  rows,
  state,
  sort,
  order,
  onSort,
  relationOptions,
  writable,
  onEdit,
  onDelete,
  skeletonRows = 10,
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
        const className =
          view.kind === 'mono'
            ? `${styles.cell} ${styles.mono}`
            : view.kind === 'muted'
              ? `${styles.cell} ${styles.muted}`
              : styles.cell;
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
          <span className={styles.actions}>
            <button
              type="button"
              className="btn"
              data-action="edit"
              data-row-id={String(row.original.id ?? '')}
              onClick={(event) => onEdit?.(row.original, event.currentTarget)}
            >
              编辑
            </button>
            <button
              type="button"
              className="btn btn-danger"
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
    <div className={styles.container}>
      {state === 'loading' && (
        // 加载态必须有可读文案（四态文案互不相同），视觉上仍是与真实行高一致的骨架。
        <p className="visually-hidden" role="status">
          正在加载列表…
        </p>
      )}
      <table className={styles.table}>
        <caption className={styles.captionText}>
          {title}列表：当前页 {rows.length} 行，排序与分页由服务端执行
        </caption>
        <colgroup>
          {fields.map((field) => (
            <col key={field.key} className={COLUMN_CLASS[field.widthStrategy]} />
          ))}
          {writable && <col className={styles.colActions} />}
        </colgroup>
        <thead>
          {headerGroups.map((group) => (
            <tr key={group.id}>
              {group.headers.map((header) => {
                const field = fields.find((f) => f.key === header.id);
                const sortable = Boolean(field?.sortable);
                const active = field?.key === sort;
                const ariaSort = active ? (order === 'asc' ? 'ascending' : 'descending') : 'none';
                return (
                  <th
                    key={header.id}
                    scope="col"
                    className={styles.th}
                    aria-sort={sortable ? ariaSort : undefined}
                  >
                    {sortable && field ? (
                      <button
                        type="button"
                        className={styles.sortButton}
                        onClick={() => onSort(field.key)}
                        aria-label={`按${field.label}排序`}
                      >
                        {field.label}
                        <span className={styles.sortMark} aria-hidden="true">
                          {active ? (order === 'asc' ? '↑' : '↓') : '↕'}
                        </span>
                      </button>
                    ) : (
                      flexRender(header.column.columnDef.header, header.getContext())
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
                    <td key={field.key} className={styles.td}>
                      <SkeletonCell short={cellIndex % 3 === 2} />
                    </td>
                  ))}
                  {writable && (
                    <td className={styles.td}>
                      <SkeletonCell />
                    </td>
                  )}
                </tr>
              ))
            : table.getRowModel().rows.map((row) => (
                <tr key={String(row.original.id ?? row.id)}>
                  {row.getVisibleCells().map((cell) => (
                    <td
                      key={cell.id}
                      className={
                        cell.column.id === '__actions'
                          ? `${styles.td} ${styles.tdActions}`
                          : styles.td
                      }
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
