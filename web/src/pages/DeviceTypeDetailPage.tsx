import { useEffect, useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useLocation, useNavigate, useParams, useSearchParams } from 'react-router';
import { ApiError, createRow, deleteRow, fetchList, fetchOne, updateRow } from '../api/client';
import { toLabelMap, useFieldOptions } from '../api/entities';
import { useMeta } from '../api/meta';
import type { EntityMeta, ListMeta, RowData } from '../api/types';
import { AppShell } from '../components/AppShell';
import { ConfirmDialog } from '../components/ConfirmDialog';
import { DataTable } from '../components/DataTable';
import { EmptyState } from '../components/EmptyState';
import { ErrorState } from '../components/ErrorState';
import { Pagination } from '../components/Pagination';
import { RecordDrawer, objectLabel } from '../components/RecordDrawer';
import { Toast, type ToastMessage } from '../components/Toast';
import { defaultSort, isWritable, relationTarget } from '../lib/fieldMeta';
import { formatCell } from '../lib/format';
import {
  patchQuery,
  parseQueryState,
  setSort,
  toListQuery,
  toSearchParams,
  type QueryState,
} from '../lib/queryState';
import { rememberScroll, takeScroll } from '../lib/scroll';

/** 父实体与子资源名固定：一个终端类型下的属性列表（`_meta.subresources`）。 */
const PARENT_ENTITY = 'device-types';
const PROPERTY_ENTITY = 'thing-model-properties';
/** 固定过滤列：只注入请求，不进 QueryState，因此筛选区没有可删除的 chip。 */
const LOCK_FIELD = 'type_id';

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
 * 终端类型详情页（task-016）：顶部摘要 + 该类型下的终端属性子列表。
 *
 * - 属性列表复用 DataTable + QueryState（q/sort/order/page 全在 URL）；
 * - `type_id` 是固定条件：只在请求层注入，页面不给它可删除的 chip，URL 也改不掉；
 * - 新建/编辑复用 RecordDrawer，`type_id` 用 lockedFields 钉死；
 * - 滚动记忆键与列表页不同（路径不同），互不覆盖；返回走 history.back()，天然恢复 URL。
 */
export function DeviceTypeDetailPage() {
  const { id = '' } = useParams();
  const navigate = useNavigate();
  const location = useLocation();
  const [searchParams, setSearchParams] = useSearchParams();
  const queryClient = useQueryClient();
  const meta = useMeta();

  const parent = meta.data?.entities.find((item) => item.name === PARENT_ENTITY);
  // 后端把属性从 entities 移到 subresources；过渡期两个位置都认，避免版本错配时页面空白。
  const property: EntityMeta | undefined =
    meta.data?.subresources?.find((item) => item.name === PROPERTY_ENTITY) ??
    meta.data?.entities.find((item) => item.name === PROPERTY_ENTITY);

  const query = useMemo(() => parseQueryState(searchParams), [searchParams]);
  const setQuery = (next: QueryState) => setSearchParams(toSearchParams(next));

  const typeQuery = useQuery({
    queryKey: ['row', PARENT_ENTITY, id],
    queryFn: ({ signal }) => fetchOne(PARENT_ENTITY, id, signal),
    enabled: Boolean(parent) && Boolean(id),
  });

  // 固定条件只在请求层注入：QueryState 里永远没有 type_id，筛选区也就不可能出现可删除的 chip。
  const listQuery = useMemo(
    () => ({
      ...toListQuery(query),
      filters: [{ field: LOCK_FIELD, op: 'eq' as const, values: [id] }],
    }),
    [query, id],
  );

  const list = useQuery({
    queryKey: ['properties', id, listQuery],
    queryFn: ({ signal }) => fetchList(PROPERTY_ENTITY, listQuery, signal),
    enabled: Boolean(property) && Boolean(id),
    placeholderData: (prev) => prev,
  });

  const relationTargets = useMemo(() => {
    if (!parent) return [];
    return Array.from(
      new Set(parent.fields.map((field) => relationTarget(field)).filter((t): t is string => Boolean(t))),
    );
  }, [parent]);
  const options = useFieldOptions(relationTargets);
  const relationOptions = useMemo(() => {
    const map: Record<string, Record<string, string>> = {};
    for (const field of parent?.fields ?? []) {
      const target = relationTarget(field);
      if (target) map[field.key] = toLabelMap(options.data?.[target]);
    }
    return map;
  }, [parent, options.data]);

  const rows = list.data?.rows ?? [];
  const listMeta: ListMeta = list.data?.meta ?? {
    total: 0,
    page: query.page,
    pageSize: query.pageSize,
    sort: query.sort,
    order: query.order,
  };
  const fields = (property?.fields ?? []).filter(
    (field) => field.visible && field.key !== LOCK_FIELD,
  );
  const fallbackSort = property ? property.defaultSort || defaultSort(property) : '';
  const writable = property ? isWritable(property) : false;

  const [drawer, setDrawer] = useState<{ mode: 'new' | 'edit'; row: RowData | null } | null>(null);
  const [editingType, setEditingType] = useState(false);
  const [pendingDelete, setPendingDelete] = useState<RowData | null>(null);
  const [toast, setToast] = useState<ToastMessage | null>(null);

  const saveMutation = useMutation({
    mutationFn: (payload: { mode: 'new' | 'edit'; body: RowData; id: string | null }) =>
      payload.mode === 'new'
        ? createRow(PROPERTY_ENTITY, payload.body)
        : updateRow(PROPERTY_ENTITY, payload.id ?? '', payload.body),
    onSuccess: (_data, payload) => {
      void queryClient.invalidateQueries({ queryKey: ['properties', id] });
      setDrawer(null);
      setToast({ kind: 'ok', text: payload.mode === 'new' ? '已创建属性，列表已刷新。' : '已保存属性，列表已刷新。' });
    },
  });

  const editTypeMutation = useMutation({
    mutationFn: (body: RowData) => updateRow(PARENT_ENTITY, id, body),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['row', PARENT_ENTITY, id] });
      setEditingType(false);
      setToast({ kind: 'ok', text: '已保存终端类型信息。' });
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (rowID: string) => deleteRow(PROPERTY_ENTITY, rowID),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['properties', id] });
      setPendingDelete(null);
      setToast({ kind: 'ok', text: '已删除属性，列表已刷新。' });
    },
  });

  // 详情页的滚动记忆键自带详情路径，与列表页（/device-types?…）不共键，互不覆盖（§8）。
  const scrollKey = `${location.pathname}?${searchParams.toString()}`;
  useEffect(() => {
    if (!list.data) return;
    const y = takeScroll(scrollKey);
    if (y !== null && y > 0) window.scrollTo(0, y);
  }, [scrollKey, list.data]);
  useEffect(() => {
    const onScroll = () => rememberScroll(scrollKey, window.scrollY);
    window.addEventListener('scroll', onScroll, { passive: true });
    return () => {
      window.removeEventListener('scroll', onScroll);
      rememberScroll(scrollKey, window.scrollY);
    };
  }, [scrollKey]);

  const backToList = () => {
    // 有来路时用 history.back()：它天然带回列表页原来的 URL（筛选/排序/分页都在 URL 里），
    // 滚动位置再由列表页自己的 sessionStorage 键恢复。location.key === 'default' 表示
    // 直接深链进入详情页（没有应用内上一页），此时退回设备类型列表。
    if (location.key !== 'default') navigate(-1);
    else navigate(`/${PARENT_ENTITY}`);
  };

  if (!parent || !property) {
    return (
      <AppShell title="页面不可用">
        <ErrorState
          title={meta.isError ? '字段元数据加载失败' : '终端属性元数据缺失'}
          text={
            meta.isError
              ? `${errorDetail(meta.error) ?? '未知错误'}。可重试；若持续失败，检查 /admin/v1/_meta 是否可用。`
              : `当前 _meta 未下发「${PARENT_ENTITY}」或「${PROPERTY_ENTITY}」的字段元数据，无法渲染详情页。请确认后端元数据版本后重试。`
          }
          onRetry={() => (meta.isError ? void meta.refetch() : backToList())}
          retryLabel={meta.isError ? '重试' : '返回列表'}
        />
      </AppShell>
    );
  }

  const typeRow = typeQuery.data;
  const typeName = typeRow?.name ? String(typeRow.name) : id;
  const summaryFields = parent.fields.filter((field) => field.visible);
  const drawerRow: RowData | null =
    drawer?.mode === 'new' ? ({ [LOCK_FIELD]: id } as RowData) : (drawer?.row ?? null);

  return (
    <AppShell
      title={`终端类型 · ${typeName}`}
      actions={
        <div className="flex items-center gap-2">
          <button type="button" className="btn" onClick={backToList}>
            返回列表
          </button>
          {writable && (
            <button
              type="button"
              className="btn"
              data-action="edit-type"
              onClick={() => {
                editTypeMutation.reset();
                setEditingType(true);
              }}
            >
              编辑基本信息
            </button>
          )}
          {writable && (
            <button
              type="button"
              className="btn btn-primary"
              data-action="new-property"
              onClick={() => {
                saveMutation.reset();
                setDrawer({ mode: 'new', row: null });
              }}
            >
              新建属性
            </button>
          )}
        </div>
      }
    >
      <section
        className="mb-4 rounded-md border border-border bg-surface p-4"
        aria-label="终端类型摘要"
      >
        {typeQuery.isError ? (
          <p className="m-0 text-negative-text" role="alert">
            终端类型信息加载失败：{errorDetail(typeQuery.error) ?? '未知错误'}
            <button type="button" className="btn btn-link" onClick={() => void typeQuery.refetch()}>
              重试
            </button>
          </p>
        ) : (
          <dl className="m-0 grid grid-cols-1 gap-2 sm:grid-cols-3">
            {summaryFields.map((field) => {
              const view = formatCell(field, typeRow?.[field.key], relationOptions[field.key] ?? {});
              return (
                <div key={field.key} className="flex flex-col gap-1">
                  <dt className="text-meta text-muted">{field.label}</dt>
                  <dd className="m-0 text-body text-text">{view.text}</dd>
                </div>
              );
            })}
          </dl>
        )}
      </section>

      {/* 固定条件：不是可删除的 chip，只在筛选区位置做一条说明。 */}
      <p className="mb-3 flex flex-wrap items-center gap-2 rounded-control border border-border bg-neutral-bg px-3 py-2 text-sm" role="status">
        <span className="text-muted">固定条件</span>
        <span>
          {property.fields.find((field) => field.key === LOCK_FIELD)?.label ?? '终端类型'} = {typeName}
        </span>
        <span className="text-muted">（不可移除，也不能在地址里改掉）</span>
      </p>

      {list.isError ? (
        <ErrorState
          title="终端属性加载失败"
          text={`${errorDetail(list.error) ?? '未知错误'}。数据未丢失，可重试。`}
          onRetry={() => void list.refetch()}
        />
      ) : list.isPending ? (
        <DataTable
          fields={fields}
          rows={[]}
          state="loading"
          sort={query.sort || fallbackSort}
          order={query.order}
          onSort={() => {}}
          relationOptions={{}}
          writable={writable}
          skeletonRows={query.pageSize}
          caption="当前筛选结果"
        />
      ) : rows.length === 0 ? (
        <EmptyState
          title="该终端类型下还没有属性"
          text="用右上角「新建属性」录入第一条；type_id 会自动锁定为当前终端类型。"
          action={
            writable ? (
              <button type="button" className="btn" onClick={() => setDrawer({ mode: 'new', row: null })}>
                新建{property.title}
              </button>
            ) : undefined
          }
        />
      ) : (
        <>
          <DataTable
            fields={fields}
            rows={rows}
            state="ready"
            sort={query.sort || fallbackSort}
            order={query.order}
            onSort={(field) => setQuery(setSort(query, field))}
            relationOptions={{}}
            writable={writable}
            onEdit={(row) => {
              saveMutation.reset();
              setDrawer({ mode: 'edit', row });
            }}
            onDelete={(row) => {
              deleteMutation.reset();
              setPendingDelete(row);
            }}
            skeletonRows={query.pageSize}
            caption="当前筛选结果"
          />
          <Pagination
            meta={listMeta}
            rangeText={rangeText(listMeta, rows.length)}
            outOfRange={listMeta.total > 0 && rows.length === 0}
            pageSizeOptions={meta.data?.pagination?.options ?? [10, 20, 50]}
            onPage={(page) => setQuery(patchQuery(query, { page }))}
            onPageSize={(pageSize) => setQuery(patchQuery(query, { pageSize }))}
          />
        </>
      )}

      {drawer && (
        <RecordDrawer
          entity={property}
          writable={writable}
          mode={drawer.mode}
          row={drawerRow}
          saving={saveMutation.isPending}
          deleting={deleteMutation.isPending}
          error={errorDetail(saveMutation.error) ?? errorDetail(deleteMutation.error)}
          lockedFields={[LOCK_FIELD]}
          onClose={() => setDrawer(null)}
          onSave={(body) =>
            saveMutation.mutate({
              mode: drawer.mode,
              body,
              id: drawer.mode === 'new' ? null : String(drawer.row?.id ?? ''),
            })
          }
          onDelete={() => setPendingDelete(drawer.row)}
        />
      )}

      {editingType && typeRow && (
        <RecordDrawer
          entity={parent}
          writable
          mode="edit"
          row={typeRow}
          saving={editTypeMutation.isPending}
          deleting={false}
          error={errorDetail(editTypeMutation.error)}
          onClose={() => setEditingType(false)}
          onSave={(body) => editTypeMutation.mutate(body)}
          onDelete={() => {}}
        />
      )}

      <ConfirmDialog
        open={pendingDelete !== null}
        objectName={pendingDelete ? objectLabel(property, pendingDelete) : property.title}
        busy={deleteMutation.isPending}
        onCancel={() => setPendingDelete(null)}
        onConfirm={() => {
          if (pendingDelete) deleteMutation.mutate(String(pendingDelete.id));
        }}
      />

      <Toast message={toast} onDismiss={() => setToast(null)} />
    </AppShell>
  );
}
