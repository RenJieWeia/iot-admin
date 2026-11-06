import { useCallback, useEffect, useMemo } from 'react';
import { useParams, useSearchParams } from 'react-router';
import { ApiError } from '../api/client';
import { useSlotCursorList, useSlotList, type SlotSource } from '../api/exceptions';
import { useMeta } from '../api/meta';
import type { CursorMeta, ListMeta, ReadonlyMeta } from '../api/types';
import { AppShell } from '../components/AppShell';
import { CursorPager } from '../components/CursorPager';
import { DataTable } from '../components/DataTable';
import { EmptyState } from '../components/EmptyState';
import { ErrorState } from '../components/ErrorState';
import { FilterBar } from '../components/FilterBar';
import { Pagination } from '../components/Pagination';
import { defaultSort, isWritable, visibleFields } from '../lib/fieldMeta';
import { isPresetActive, presetsFor, togglePreset } from '../lib/presets';
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

interface Props {
  /** exceptions 与 readonly 两个只读源共用同一实现，仅切换数据源前缀。 */
  source: SlotSource;
}

/**
 * 通用只读页（异常两页 / 最新值 / 小时暂存 / 小时快照）：列、中文、枚举色调全部来自 `_meta`。
 *
 * 分页形态由 `_meta.readonly[].pagination` 决定：
 * - `offset`：页码 + 总数；
 * - `cursor`：「加载更多」，只认 `hasMore`，不显示总数（分区表 count(*) 会随月退化），
 *   游标写进 URL，刷新/后退可以复原同一页。
 *
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

  const isCursor = (holder?.pagination ?? 'offset') === 'cursor';

  const offsetList = useSlotList(source, slot, toListQuery(query), Boolean(holder) && !isCursor);
  const cursorList = useSlotCursorList(
    source,
    slot,
    { q: query.q, filters: toListQuery(query).filters, cursor: query.cursor },
    Boolean(holder) && isCursor,
  );
  const activeList = isCursor ? cursorList : offsetList;

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

  const fields = visibleFields(holder);
  const rows = (isCursor ? cursorList.data?.rows : offsetList.data?.rows) ?? [];
  const cursorMeta: CursorMeta = cursorList.data?.meta ?? {
    hasMore: false,
    nextCursor: null,
    limit: 0,
  };
  const listMeta: ListMeta = offsetList.data?.meta ?? {
    total: 0,
    page: query.page,
    pageSize: query.pageSize,
    sort: query.sort,
    order: query.order,
  };
  const fallbackSort = holder.defaultSort || defaultSort(holder);
  const activeFilters = hasActiveConditions(query);
  const outOfRange = !isCursor && listMeta.total > 0 && rows.length === 0;
  const listState = activeList.isError
    ? 'error'
    : activeList.isPending && rows.length === 0
      ? 'loading'
      : rows.length === 0
        ? activeFilters
          ? 'nomatch'
          : 'empty'
        : 'ready';

  const presets = presetsFor(holder.fields);

  return (
    <AppShell
      title={holder.title}
      actions={<span className="muted">只读页面：不提供新建、编辑或删除入口。</span>}
    >
      {presets.length > 0 && (
        <div className="preset-row">
          {presets.map((preset) => (
            <label key={preset.field}>
              <input
                type="checkbox"
                checked={isPresetActive(query, preset)}
                onChange={(event) => setQuery(togglePreset(query, preset, event.target.checked))}
              />{' '}
              {preset.label}（{preset.hint}）
            </label>
          ))}
        </div>
      )}

      <FilterBar entity={holder} query={query} relationOptions={{}} onChange={setQuery} />

      {listState === 'error' ? (
        <ErrorState
          title="列表加载失败"
          text={`${errorDetail(activeList.error) ?? '未知错误'}。数据未丢失，可重试；若筛选条件非法，清除筛选后重试。`}
          onRetry={() => void activeList.refetch()}
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
          text="这个数据源当前没有任何记录。数据写入后会自动出现在这里，可稍后刷新再看。"
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
            fields={fields}
            rows={rows}
            state={listState === 'loading' ? 'loading' : 'ready'}
            sort={query.sort || fallbackSort}
            order={query.order}
            onSort={(field) => setQuery(setSort(query, field))}
            relationOptions={{}}
            writable={isWritable(holder)}
            sortable={!isCursor}
            skeletonRows={isCursor ? 10 : query.pageSize}
            caption={
              isCursor
                ? `当前页 ${rows.length} 条`
                : listMeta.total === 0
                  ? '共 0 条'
                  : `共 ${listMeta.total} 条，第 ${listMeta.page} 页`
            }
          />
          {isCursor ? (
            <CursorPager
              hasMore={cursorMeta.hasMore}
              loading={cursorList.isFetching}
              limit={cursorMeta.limit}
              loaded={rows.length}
              onMore={() => setQuery(patchQuery(query, { cursor: cursorMeta.nextCursor ?? '' }))}
            />
          ) : (
            <Pagination
              meta={listMeta}
              rangeText={rangeText(listMeta, rows.length)}
              outOfRange={outOfRange}
              pageSizeOptions={metaQuery.data?.pagination?.options ?? [10, 20, 50]}
              onPage={(page) => setQuery(patchQuery(query, { page }))}
              onPageSize={(pageSize) => setQuery(patchQuery(query, { pageSize }))}
            />
          )}
        </>
      )}
    </AppShell>
  );
}
