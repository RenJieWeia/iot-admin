import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import type { CSSProperties } from 'react';
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
} from '@tanstack/react-table';
import type { FieldMeta, RowData } from '../api/types';
import type { WidthStrategy } from '../api/types';
import { cn } from '../lib/cn';
import { scrollEdges } from '../lib/scrollEdges';
import { formatCell } from '../lib/format';
import * as Tooltip from '@radix-ui/react-tooltip';
import { Table } from '@radix-ui/themes';
import { SkeletonCell } from './Skeleton';
import {
  ACTION_DANGER,
  ACTION_LINK,
  TABLE_WRAPPER,
  TD_STICKY_LEFT,
  TD_STICKY_RIGHT,
  TH_STICKY_LEFT,
  TH_STICKY_RIGHT,
  TR_ROW,
  tdCell,
  thCell,
} from './tableStyles';
import { StatusTag } from './StatusTag';

export type TableState = 'loading' | 'ready';

/**
 * 渲染层尺寸（P4 / A3）：**表头 40px、数据行 48px**（原来是 46.5 / 44）。
 * 视觉口径仍取自 tableStyles 的单元格模板，只把高度作为参数传进去；
 * 绑定列表那张自建表格继续用 44，不受影响。
 */
const TH_CLASS = thCell('h-10');
const TD_CLASS = tdCell('h-12');

/**
 * Radix 单元格的 padding / 最小高度 / 行阴影都来自 `.rt-TableRoot` 上定义的 CSS 变量。
 * 这里**不用** `Table.Root`：它内部套了一层 Radix ScrollArea（`.rt-ScrollAreaViewport`），
 * 实测横向滚动会发生在那个 viewport 里（sw 1060 / cw 952 / scrollLeft 108），
 * 而 `.table-shell` 的 sw==cw 永远是 0 → `data-scroll-*` 投影与 sticky 判定全部失效。
 * 所以滚动容器仍是 `.table-shell`，只借用 Radix 的 Header/Body/Row/Cell 组件；
 * 这三个变量在此显式归零，避免将来有人补回 Root 时出现双倍 padding 与双行线。
 */
const CELL_VARS: CSSProperties = {
  ['--table-cell-padding' as string]: '0',
  ['--table-cell-min-height' as string]: '0',
  ['--table-row-box-shadow' as string]: 'none',
  ['--table-row-background-color' as string]: 'transparent',
};


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
  /** 可选的「进入详情」入口（如终端类型 → 终端属性详情页）；不传则不出按钮。 */
  onOpen?: (row: RowData, trigger: HTMLButtonElement) => void;
  openLabel?: string;
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
  onOpen,
  openLabel = '查看',
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
        // 单行截断靠 CSS，单元格永不换行，行高稳定（§6）。
        // 完整值放 data-full，由表格根节点统一驱动一个 Tooltip（不给每格包 Tooltip.Root）；
        // title 保留，作为无 JS/读屏的兜底。
        const className = cn(
          'block overflow-hidden text-ellipsis whitespace-nowrap',
          view.kind === 'mono' && 'font-mono text-sm',
          view.kind === 'muted' && 'text-muted',
          // 数字列右对齐 + 等宽数字：位数不同也能纵向对齐（空值占位符同样是数字列，一起右对齐）。
          field.type === 'number' && 'text-right tabular-nums',
        );
        // 不再带原生 title：自定义提示 300ms 出现后，浏览器 title 约 1s 会再叠一个。
        // 完整值只放 data-full，由表格级 Tooltip 统一消费。
        return (
          <span className={className} data-full={view.full}>
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
            {onOpen && (
              <button
                type="button"
                className={cn('touch-hit', ACTION_LINK)}
                data-action="open"
                data-row-id={String(row.original.id ?? '')}
                onClick={(event) => onOpen(row.original, event.currentTarget)}
              >
                {openLabel}
              </button>
            )}
            <button
              type="button"
              className={cn('touch-hit', ACTION_LINK)}
              data-action="edit"
              data-row-id={String(row.original.id ?? '')}
              onClick={(event) => onEdit?.(row.original, event.currentTarget)}
            >
              编辑
            </button>
            <button
              type="button"
              className={cn('touch-hit', ACTION_DANGER)}
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
  }, [fields, relationOptions, writable, onEdit, onDelete, onOpen, openLabel]);

  const table = useReactTable({
    data: rows,
    columns,
    getCoreRowModel: getCoreRowModel(),
    manualPagination: true,
    manualSorting: true,
  });

  const headerGroups = table.getHeaderGroups();

  // 骨架行数 = min(当前每页条数, 上一屏实际行数)，没有上一屏时用 10（§6.4）：
  // 选 50 条但服务端只回 10 行时，骨架不会先撑出 50 行再骤降。
  // 上一屏行数在组件内部用 ref 记录：DataTable 是纯展示组件，不为此改调用方签名
  // （EntityListPage 也不在本批写作用域内）。
  const lastRowCount = useRef(0);
  useEffect(() => {
    if (state === 'ready') lastRowCount.current = rows.length;
  }, [state, rows.length]);
  const skeletonCount = Math.max(1, Math.min(skeletonRows, lastRowCount.current || 10));

  // 横向滚动状态：决定 sticky 两侧投影是否显示（不滚动时不该出现"脏边"）。
  // 用 data-* 属性交给 global.css 的选择器，TSX 里不出现视觉值。
  const wrapperRef = useRef<HTMLDivElement | null>(null);
  const [edges, setEdges] = useState({ left: false, right: false });

  useLayoutEffect(() => {
    const el = wrapperRef.current;
    if (!el) return;
    const update = () => {
      const next = scrollEdges(el);
      // 只在真正变化时 setState，避免滚动过程中每帧重渲染整张表。
      setEdges((prev) => (prev.left === next.left && prev.right === next.right ? prev : next));
    };
    update();
    el.addEventListener('scroll', update, { passive: true });
    // 数据行数、列宽、窗口尺寸变化都会改变"是否溢出"，用 ResizeObserver 统一兜住。
    const observer = new ResizeObserver(update);
    observer.observe(el);
    return () => {
      el.removeEventListener('scroll', update);
      observer.disconnect();
    };
  }, [rows.length, fields.length, state]);

  // 完整值提示：表格级只挂一个受控 Tooltip，单元格只输出 data-full。
  // 不给每格包 Tooltip.Root——500 行 × 10 列就是 5000 个 Root，hover 会明显掉帧。
  const [tip, setTip] = useState<{ text: string; left: number; top: number; height: number } | null>(null);
  const tipTimer = useRef<number | null>(null);
  // 触屏交互后的一小段时间内不弹提示（触摸会连带产生 mouseover）。
  const suppressRef = useRef(false);
  const suppressTimer = useRef<number | null>(null);

  const hideTip = useCallback(() => {
    if (tipTimer.current !== null) {
      window.clearTimeout(tipTimer.current);
      tipTimer.current = null;
    }
    setTip(null);
  }, []);

  useEffect(() => hideTip, [hideTip]);

  const showTipFor = useCallback((target: HTMLElement) => {
    if (suppressRef.current) return;
    // 只有真的被截断才提示；未截断的单元格不该出现浮层。
    if (target.scrollWidth <= target.clientWidth) return;
    // 只用 scrollWidth 判定截断：CSS 截断不改 textContent，拿 textContent 比较永远相等，
    // 那样会把所有截断单元格都判成"不需要提示"。
    const full = target.dataset.full ?? '';
    if (full === '') return;
    const rect = target.getBoundingClientRect();
    if (tipTimer.current !== null) window.clearTimeout(tipTimer.current);
    // 300ms 延迟：扫过表格时不至于满屏跳提示（与 Radix 默认 delayDuration 对齐）。
    tipTimer.current = window.setTimeout(() => {
      setTip({ text: full, left: rect.left, top: rect.top, height: rect.height });
      tipTimer.current = null;
    }, 300);
  }, []);

  const cellFromEvent = (event: React.SyntheticEvent<HTMLElement>): HTMLElement | null => {
    const node = event.target as HTMLElement | null;
    const cell = node?.closest?.('[data-full]');
    return cell instanceof HTMLElement ? cell : null;
  };

  const onPointerOver = (event: React.MouseEvent<HTMLDivElement>) => {
    const cell = cellFromEvent(event);
    if (cell) showTipFor(cell);
  };
  const onPointerOut = (event: React.MouseEvent<HTMLDivElement>) => {
    if (cellFromEvent(event)) hideTip();
  };
  const onTouchStart = () => {
    suppressRef.current = true;
    if (suppressTimer.current !== null) window.clearTimeout(suppressTimer.current);
    suppressTimer.current = window.setTimeout(() => {
      suppressRef.current = false;
    }, 600);
  };

  return (
    <Tooltip.Provider delayDuration={300}>
    <div
      ref={wrapperRef}
      className={TABLE_WRAPPER}
      data-scroll-left={edges.left ? 'true' : undefined}
      data-scroll-right={edges.right ? 'true' : undefined}
      onMouseOver={onPointerOver}
      onMouseOut={onPointerOut}
      onTouchStart={onTouchStart}
    >
      {state === 'loading' && (
        // 加载态必须有可读文案（四态文案互不相同），视觉上仍是与真实行高一致的骨架。
        <p className="visually-hidden" role="status">
          正在加载列表…
        </p>
      )}
            {/* table-fixed + w-full：列宽由 colgroup 决定，内容超宽时**截断并出提示**，
          而不是把表格撑宽（上一版用 w-max 按内容撑开，导致列宽推导与完整值提示全部失效——
          列宽被内容撑开、单元格永远不截断）。表格总宽小于列宽之和时，外层容器横向滚动。 */}
      <table className="table-fixed w-full border-collapse" style={CELL_VARS}>
        <caption className="pb-2 text-left text-sm text-muted">{caption}</caption>
        <colgroup>
          {fields.map((field) => (
            // 宽度优先用 _meta 的推导结论（后端按字段类型算好）；缺省时回落到 widthStrategy 的 CSS 类。
            <col
              key={field.key}
              className={COLUMN_CLASS[field.widthStrategy]}
              style={field.width ? { width: field.width, minWidth: field.width } : undefined}
            />
          ))}
          {writable && <col className="col-actions" />}
        </colgroup>
        <Table.Header>
          {headerGroups.map((group) => (
            <Table.Row key={group.id}>
              {group.headers.map((header) => {
                const field = fields.find((f) => f.key === header.id);
                const canSort = sortable && Boolean(field?.sortable);
                const active = field?.key === sort;
                const ariaSort = active ? (order === 'asc' ? 'ascending' : 'descending') : 'none';
                return (
                  <Table.ColumnHeaderCell
                    key={header.id}
                    scope="col"
                    // 首列与 body 的首列一起固定，横滚时列名不会和数据错位（§5.2）。
                    className={cn(
                      TH_CLASS,
                      field?.key === fields[0]?.key && TH_STICKY_LEFT,
                      // 操作列表头与表体操作列一起吸右，否则横滚时列名跟着走、与下面的按钮错位。
                      header.column.id === '__actions' && TH_STICKY_RIGHT,
                      // 数字列表头右对齐，与单元格一致。
                      field?.type === 'number' && 'text-right',
                    )}
                    aria-sort={active ? ariaSort : canSort ? 'none' : undefined}
                  >
                    {canSort && field ? (
                      <button
                        type="button"
                        className="touch-hit inherit-font inline-flex min-h-10 cursor-pointer items-center gap-1 border-0 bg-transparent p-0"
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
                  </Table.ColumnHeaderCell>
                );
              })}
            </Table.Row>
          ))}
        </Table.Header>
        <Table.Body>
          {state === 'loading'
            ? Array.from({ length: skeletonCount }).map((_, rowIndex) => (
                <Table.Row key={`skeleton-${rowIndex}`} aria-hidden="true">
                  {fields.map((field, cellIndex) => (
                    <Table.Cell
                      key={field.key}
                      className={cn(TD_CLASS, cellIndex === 0 && TD_STICKY_LEFT)}
                    >
                      <SkeletonCell short={cellIndex % 3 === 2} />
                    </Table.Cell>
                  ))}
                  {writable && (
                    <Table.Cell className={cn(TD_CLASS, TD_STICKY_RIGHT)}>
                      <SkeletonCell />
                    </Table.Cell>
                  )}
                </Table.Row>
              ))
            : table.getRowModel().rows.map((row) => (
                <Table.Row key={String(row.original.id ?? row.id)} className={TR_ROW}>
                  {row.getVisibleCells().map((cell) => (
                    <Table.Cell
                      key={cell.id}
                      className={cn(
                        TD_CLASS,
                        cell.column.id === fields[0]?.key && TD_STICKY_LEFT,
                        cell.column.id === '__actions' && TD_STICKY_RIGHT,
                        // 数字列整列右对齐（单元格内部的 span 也带 text-right，占位符不会跳基线）。
                        fields.find((f) => f.key === cell.column.id)?.type === 'number' && 'text-right',
                      )}
                    >
                      {flexRender(cell.column.columnDef.cell, cell.getContext())}
                    </Table.Cell>
                  ))}
                </Table.Row>
              ))}
        </Table.Body>
      </table>

      {/* 唯一的 Tooltip：锚点跟着被悬停/聚焦的单元格走（虚拟锚点，不占布局、不可聚焦、不可交互）。 */}
      <Tooltip.Root open={tip !== null}>
        <Tooltip.Trigger asChild>
          <span
            aria-hidden="true"
            tabIndex={-1}
            data-testid="table-tooltip-anchor"
            style={{
              position: 'fixed',
              left: tip?.left ?? 0,
              top: tip ? tip.top + tip.height : 0,
              width: 1,
              height: 1,
              pointerEvents: 'none',
            }}
          />
        </Tooltip.Trigger>
        <Tooltip.Portal>
          <Tooltip.Content side="top" sideOffset={4} className="tooltip-content" aria-hidden="true">
            {tip?.text}
          </Tooltip.Content>
        </Tooltip.Portal>
      </Tooltip.Root>
    </div>
    </Tooltip.Provider>
  );
}
