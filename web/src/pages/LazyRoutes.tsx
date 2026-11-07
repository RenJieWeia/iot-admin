import { Suspense, lazy } from 'react';
import { useParams, useSearchParams } from 'react-router';
import { useMeta } from '../api/meta';
import type { SlotSource } from '../api/exceptions';
import { AppShell } from '../components/AppShell';
import { DataTable } from '../components/DataTable';
import { MetaGate, PagePlaceholder } from '../components/MetaGate';
import { visibleFields } from '../lib/fieldMeta';
import { parseQueryState } from '../lib/queryState';
import { groupCrumb } from '../lib/navGroups';

/** 页面按路由懒加载：首屏只下载外壳 + 表格骨架，抽屉（Radix/zod）随页面分包。 */
const EntityListPage = lazy(() =>
  import('./EntityListPage').then((module) => ({ default: module.EntityListPage })),
);
const ReadonlySlotPage = lazy(() =>
  import('./ReadonlySlotPage').then((module) => ({ default: module.ReadonlySlotPage })),
);
const SnapshotConfigPage = lazy(() =>
  import('./SnapshotConfigPage').then((module) => ({ default: module.SnapshotConfigPage })),
);
const DeviceTypeDetailPage = lazy(() =>
  import('./DeviceTypeDetailPage').then((module) => ({ default: module.DeviceTypeDetailPage })),
);

interface FallbackProps {
  title: string;
  /** 骨架也属于某个导航分组：分组名与最终页面保持一致，避免加载瞬间面包屑跳一下。 */
  group: 'entities' | 'data' | 'exceptions';
  fields: ReturnType<typeof visibleFields>;
  writable: boolean;
  sort: string;
  order: 'asc' | 'desc';
  skeletonRows: number;
}

/**
 * Suspense fallback：直接复用 DataTable 的加载态，保证列数、列宽（widthStrategy）与
 * 真实表格一致、行高等于 `--row-h`（§6 布局稳定性）；不新造加载态组件。
 */
function TableFallback({ title, group, fields, writable, sort, order, skeletonRows }: FallbackProps) {
  return (
    <AppShell title={title} breadcrumb={groupCrumb(group)}>
      <DataTable
        fields={fields}
        rows={[]}
        state="loading"
        sort={sort}
        order={order}
        onSort={() => {}}
        relationOptions={{}}
        writable={writable}
        skeletonRows={skeletonRows}
        caption="正在加载…"
      />
    </AppShell>
  );
}

export function LazyEntityPage() {
  const params = useParams();
  const [searchParams] = useSearchParams();
  const meta = useMeta();
  const entity = meta.data?.entities.find((item) => item.name === params.entity);
  const pageSize = parseQueryState(searchParams).pageSize;

  return (
    <MetaGate>
      <Suspense
        fallback={
          entity ? (
            <TableFallback
              title={entity.title}
              group="entities"
              fields={visibleFields(entity)}
              writable={!entity.readOnly}
              sort={entity.defaultSort}
              order={entity.defaultOrder}
              skeletonRows={pageSize}
            />
          ) : (
            <AppShell title="IoT 管理后台">
              <PagePlaceholder status="正在加载页面组件…" />
            </AppShell>
          )
        }
      >
        <EntityListPage />
      </Suspense>
    </MetaGate>
  );
}

/** 两个只读源共用同一个页面实现，只有数据源前缀不同。 */
function LazySlotPage({ source }: { source: SlotSource }) {
  const params = useParams();
  const [searchParams] = useSearchParams();
  const meta = useMeta();
  const holder =
    source === 'exceptions'
      ? meta.data?.exceptions.find((item) => item.slot === params.slot)
      : meta.data?.readonly?.find((item) => item.slot === params.slot);
  const pageSize = parseQueryState(searchParams).pageSize;

  return (
    <MetaGate>
      <Suspense
        fallback={
          holder ? (
            <TableFallback
              title={holder.title}
              group={source === 'exceptions' ? 'exceptions' : 'data'}
              fields={visibleFields(holder)}
              writable={false}
              sort={holder.defaultSort}
              order={holder.defaultOrder}
              skeletonRows={pageSize}
            />
          ) : (
            <AppShell title="IoT 管理后台">
              <PagePlaceholder status="正在加载页面组件…" />
            </AppShell>
          )
        }
      >
        <ReadonlySlotPage source={source} />
      </Suspense>
    </MetaGate>
  );
}

export function LazyExceptionPage() {
  return <LazySlotPage source="exceptions" />;
}

export function LazyReadonlyPage() {
  return <LazySlotPage source="readonly" />;
}

/** 终端类型详情页（task-016）：懒加载 + 与真实表格同形的骨架。 */
export function LazyDeviceTypeDetailPage() {
  const [searchParams] = useSearchParams();
  const meta = useMeta();
  const property = meta.data?.subresources?.find((item) => item.name === 'thing-model-properties');
  const pageSize = parseQueryState(searchParams).pageSize;

  return (
    <MetaGate>
      <Suspense
        fallback={
          property ? (
            <TableFallback
              title="终端类型"
              group="entities"
              fields={visibleFields(property)}
              writable={!property.readOnly}
              sort={property.defaultSort}
              order={property.defaultOrder}
              skeletonRows={pageSize}
            />
          ) : (
            <AppShell title="IoT 管理后台">
              <PagePlaceholder status="正在加载页面组件…" />
            </AppShell>
          )
        }
      >
        <DeviceTypeDetailPage />
      </Suspense>
    </MetaGate>
  );
}

/** 快照配置页（导航「快照计划」）：三段式，懒加载与页面骨架对齐。 */
export function LazySnapshotConfigPage() {
  return (
    <MetaGate>
      <Suspense
        fallback={
          <AppShell title="快照计划" breadcrumb={groupCrumb('entities')}>
            <PagePlaceholder status="正在加载快照配置…" />
          </AppShell>
        }
      >
        <SnapshotConfigPage />
      </Suspense>
    </MetaGate>
  );
}
