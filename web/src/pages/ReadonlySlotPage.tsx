import { useCallback, useEffect, useMemo } from 'react';
import { useParams, useSearchParams } from 'react-router';
import { ApiError } from '../api/client';
import { useSlotList, type SlotSource } from '../api/exceptions';
import { useMeta } from '../api/meta';
import type { FieldMeta, ListMeta, ReadonlyMeta } from '../api/types';
import type { ActiveFilter } from '../lib/queryState';
import { AppShell } from '../components/AppShell';
import { DataTable } from '../components/DataTable';
import { EmptyState } from '../components/EmptyState';
import { ErrorState } from '../components/ErrorState';
import { FilterBar } from '../components/FilterBar';
import { Pagination } from '../components/Pagination';
import { defaultSort, isWritable } from '../lib/fieldMeta';
import {
  clearFilters,
  hasActiveConditions,
  parseQueryState,
  patchQuery,
  setSort,
  toListQuery,
  toSearchParams,
  type QueryState,
} from '../lib/queryState';

function rangeText(meta: ListMeta, rowCount: number): string {
  if (meta.total === 0) return '共 0 条';
  if (rowCount === 0) return `第 ${meta.page} 页无数据，共 ${meta.total} 条`;
  const start = (meta.page - 1) * meta.pageSize + 1;
  return `第 ${start}–${start + rowCount - 1} 条，共 ${meta.total} 条`;
}

function errorDetail(error: unknown): string | null {
  if (!error) return null;
  if (error instanceof ApiError) return error.detail;
  return String(error);
}

/**
 * 「仅看有问题」的语义来自 task-009 §2：`filter.quality.gte=1`（不新增 SQL 操作符）。
 * 只有当前只读页确实下发了 `quality` 数值字段（且支持 gte）时才渲染该开关，
 * 其余只读页不受影响；文案与阈值是本批固定的产品语义，不来自 _meta。
 */
const QUALITY_FIELD_KEY = 'quality';
const TROUBLE_THRESHOLD = '1';

function qualityPresetField(fields: FieldMeta[]): FieldMeta | undefined {
  return fields.find(
    (field) => field.key === QUALITY_FIELD_KEY && field.filterable && field.operators.includes('gte'),
  );
}

function isTroubleOnly(query: QueryState): boolean {
  return (
    query.filters.find((f) => f.field === QUALITY_FIELD_KEY && f.op === 'gte')?.values.includes(
      TROUBLE_THRESHOLD,
    ) ?? false
  );
}

interface Props {
  /** exceptions 与 readonly 两个只读源共用同一实现，仅切换数据源前缀。 */
  source: SlotSource;
}

/**
 * 通用只读页（异常两页 + task-009 的最新值）：列、中文、枚举色调全部来自 `_meta`。
 * 刻意不提供任何写入口：没有新建/编辑/删除、不解析 drawer、不发写请求。
 */
export function ReadonlySlotPage({ source }: Props) {
  const params = useParams();
  const slot = params.slot ?? '';
  const [searchParams, setSearchParams] = useSearchParams();
  const metaQuery = useMeta();
  const holder: ReadonlyMeta | undefined =
    source === 'exceptions'
      ? metaQuery.data?.exceptions.find((item) => item.slot === slot)
      : metaQuery.data?.readonly?.find((item) => item.slot === slot);
  const query = useMemo(() => parseQueryState(searchParams), [searchParams]);

  const list = useSlotList(source, slot, toListQuery(query), Boolean(holder));

  const setQuery = useCallback(
    (next: QueryState, replace = false) => {
      setSearchParams(toSearchParams(next), { replace });
    },
    [setSearchParams],
  );

  useEffect(() => {
    document.title = holder ? `${holder.title} · IoT 管理后台` : 'IoT 管理后台';
  }, [holder]);

  // 元数据未就绪/失败由 MetaGate 在外层处理，这里只会拿到已就绪的 meta。
  if (!holder) {
    return (
      <AppShell title="页面不存在">
        <ErrorState
          title="未知只读页"
          text={`当前地址的只读页「${slot}」不在字段元数据里。请从左侧导航重新进入。`}
          onRetry={() => window.history.back()}
          retryLabel="返回上一页"
        />
      </AppShell>
    );
  }

  const fields = holder.fields.filter((field) => field.visible);
  const rows = list.data?.rows ?? [];
  const listMeta: ListMeta = list.data?.meta ?? {
    total: 0,
    page: query.page,
    pageSize: query.pageSize,
    sort: query.sort,
    order: query.order,
  };
  const fallbackSort = holder.defaultSort || defaultSort(holder);
  const activeFilters = hasActiveConditions(query);
  const outOfRange = listMeta.total > 0 && rows.length === 0;
  const listState = list.isError
    ? 'error'
    : list.isPending
      ? 'loading'
      : rows.length === 0
        ? activeFilters
          ? 'nomatch'
          : 'empty'
        : 'ready';

  const presetField = qualityPresetField(holder.fields);
  const troubleOnly = isTroubleOnly(query);

  const toggleTroubleOnly = (checked: boolean) => {
    const rest = query.filters.filter((f) => f.field !== QUALITY_FIELD_KEY);
    const next: ActiveFilter[] = checked
      ? [...rest, { field: QUALITY_FIELD_KEY, op: 'gte', values: [TROUBLE_THRESHOLD] }]
      : rest;
    setQuery(patchQuery(query, { filters: next }));
  };

  return (
    <AppShell
      title={holder.title}
      actions={<span className="muted">只读页面：不提供新建、编辑或删除入口。</span>}
    >
      {presetField && (
        <div className="preset-row">
          <label>
            <input
              type="checkbox"
              checked={troubleOnly}
              onChange={(event) => toggleTroubleOnly(event.target.checked)}
            />{' '}
            仅看有问题（{presetField.label} ≥ {TROUBLE_THRESHOLD}）
          </label>
        </div>
      )}

      <FilterBar
        entity={holder}
        query={query}
        relationOptions={{}}
        onChange={setQuery}
      />

      {listState === 'error' ? (
        <ErrorState
          title="列表加载失败"
          text={`${errorDetail(list.error) ?? '未知错误'}。数据未丢失，可重试；若筛选条件非法，清除筛选后重试。`}
          onRetry={() => void list.refetch()}
          extra={
            activeFilters ? (
              <button type="button" className="btn" onClick={() => setQuery(clearFilters(query))}>
                清除筛选
              </button>
            ) : undefined
          }
        />
      ) : listState === 'empty' ? (
        <EmptyState
          title={`暂无${holder.title}`}
          text="这个数据源当前没有任何记录。接入端上报后会自动出现在这里，可稍后刷新再看。"
        />
      ) : listState === 'nomatch' ? (
        <EmptyState
          title="没有匹配的记录"
          text="当前关键词或筛选条件排除了全部结果。可逐个移除条件，或清除全部筛选后重新查询。"
          action={
            <button type="button" className="btn" onClick={() => setQuery(clearFilters(query))}>
              清除筛选
            </button>
          }
        />
      ) : (
        <>
          <DataTable
            title={holder.title}
            fields={fields}
            rows={rows}
            state={listState === 'loading' ? 'loading' : 'ready'}
            sort={query.sort || fallbackSort}
            order={query.order}
            onSort={(field) => setQuery(setSort(query, field))}
            relationOptions={{}}
            writable={isWritable(holder)}
            skeletonRows={query.pageSize}
          />
          <Pagination
            meta={listMeta}
            rangeText={rangeText(listMeta, rows.length)}
            outOfRange={outOfRange}
            pageSizeOptions={metaQuery.data?.pagination?.options ?? [10, 20, 50]}
            onPage={(page) => setQuery(patchQuery(query, { page }))}
            onPageSize={(pageSize) => setQuery(patchQuery(query, { pageSize }))}
          />
        </>
      )}
    </AppShell>
  );
}
