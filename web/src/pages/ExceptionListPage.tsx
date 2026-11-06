import { useCallback, useEffect, useMemo } from 'react';
import { useParams, useSearchParams } from 'react-router';
import { ApiError } from '../api/client';
import { useExceptionList } from '../api/exceptions';
import { useMeta } from '../api/meta';
import type { ListMeta } from '../api/types';
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
 * 异常事件 / 异常区间：只读列表（R-019 独立端点，不进 admin.Entities）。
 * 刻意不提供任何写入口：没有新建按钮、没有操作列、不解析 drawer 参数、不发写请求。
 */
export function ExceptionListPage() {
  const params = useParams();
  const slot = params.slot ?? '';
  const [searchParams, setSearchParams] = useSearchParams();
  const metaQuery = useMeta();
  const exception = metaQuery.data?.exceptions.find((item) => item.slot === slot);
  const query = useMemo(() => parseQueryState(searchParams), [searchParams]);

  const list = useExceptionList(slot, toListQuery(query), Boolean(exception));

  const setQuery = useCallback(
    (next: QueryState, replace = false) => {
      setSearchParams(toSearchParams(next), { replace });
    },
    [setSearchParams],
  );

  useEffect(() => {
    document.title = exception ? `${exception.title} · IoT 管理后台` : 'IoT 管理后台';
  }, [exception]);

  if (metaQuery.isPending) {
    return (
      <AppShell title="加载中…">
        <p className="muted">正在加载字段元数据…</p>
      </AppShell>
    );
  }

  if (metaQuery.isError) {
    return (
      <AppShell title="管理后台">
        <ErrorState
          title="字段元数据加载失败"
          text="页面无法确定列与筛选能力，请重试或检查 /admin/v1/_meta 是否可用。"
          onRetry={() => void metaQuery.refetch()}
        />
      </AppShell>
    );
  }

  // 元数据未就绪/失败由 MetaGate 在外层处理，这里只会拿到已就绪的 meta。
  if (!exception) {
    return (
      <AppShell title="页面不存在">
        <ErrorState
          title="未知异常页"
          text={`当前地址的异常页「${slot}」不存在，只提供 events 与 intervals。请从左侧导航重新进入。`}
          onRetry={() => window.history.back()}
          retryLabel="返回上一页"
        />
      </AppShell>
    );
  }

  const fields = exception.fields.filter((field) => field.visible);
  const rows = list.data?.rows ?? [];
  const listMeta: ListMeta = list.data?.meta ?? {
    total: 0,
    page: query.page,
    pageSize: query.pageSize,
    sort: query.sort,
    order: query.order,
  };
  const fallbackSort = exception.defaultSort || defaultSort(exception);
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

  return (
    <AppShell
      title={exception.title}
      actions={<span className="muted">只读页面：不提供新建、编辑或删除入口。</span>}
    >
      <FilterBar
        entity={exception}
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
          title={`暂无${exception.title}`}
          text="该时间范围内没有产生异常记录。可放宽时间范围或清除筛选后再看。"
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
            title={exception.title}
            fields={fields}
            rows={rows}
            state={listState === 'loading' ? 'loading' : 'ready'}
            sort={query.sort || fallbackSort}
            order={query.order}
            onSort={(field) => setQuery(setSort(query, field))}
            relationOptions={{}}
            writable={isWritable(exception)}
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
