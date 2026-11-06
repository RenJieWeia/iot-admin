import { Suspense, lazy } from 'react';
import { useParams, useSearchParams } from 'react-router';
import { useMeta } from '../api/meta';
import { AppShell } from '../components/AppShell';
import { DataTable } from '../components/DataTable';
import { MetaGate } from '../components/MetaGate';
import { StatePanel } from '../components/StatePanel';
import { visibleFields } from '../lib/fieldMeta';
import { parseQueryState } from '../lib/queryState';

/** 页面按路由懒加载：首屏只下载外壳 + 表格骨架，抽屉（Radix/zod）随页面分包。 */
const EntityListPage = lazy(() =>
  import('./EntityListPage').then((module) => ({ default: module.EntityListPage })),
);
const ExceptionListPage = lazy(() =>
  import('./ExceptionListPage').then((module) => ({ default: module.ExceptionListPage })),
);

interface FallbackProps {
  title: string;
  fields: ReturnType<typeof visibleFields>;
  writable: boolean;
  sort: string;
  order: 'asc' | 'desc';
  skeletonRows: number;
}

/**
 * Suspense fallback：直接复用 DataTable 的加载态，保证列数、列宽（widthStrategy）与
 * 真实表格一致、行高等于 `--row-height`（§6 布局稳定性）；不新造加载态组件。
 */
function TableFallback({ title, fields, writable, sort, order, skeletonRows }: FallbackProps) {
  return (
    <AppShell title={title}>
      <DataTable
        title={title}
        fields={fields}
        rows={[]}
        state="loading"
        sort={sort}
        order={order}
        onSort={() => {}}
        relationOptions={{}}
        writable={writable}
        skeletonRows={skeletonRows}
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
              fields={visibleFields(entity)}
              writable={!entity.readOnly}
              sort={entity.defaultSort}
              order={entity.defaultOrder}
              skeletonRows={pageSize}
            />
          ) : (
            <AppShell title="加载中…">
              <StatePanel title="正在准备页面" text="页面组件加载中，完成后自动显示。" />
            </AppShell>
          )
        }
      >
        <EntityListPage />
      </Suspense>
    </MetaGate>
  );
}

export function LazyExceptionPage() {
  const params = useParams();
  const [searchParams] = useSearchParams();
  const meta = useMeta();
  const exception = meta.data?.exceptions.find((item) => item.slot === params.slot);
  const pageSize = parseQueryState(searchParams).pageSize;

  return (
    <MetaGate>
      <Suspense
        fallback={
          exception ? (
            <TableFallback
              title={exception.title}
              fields={visibleFields(exception)}
              writable={false}
              sort={exception.defaultSort}
              order={exception.defaultOrder}
              skeletonRows={pageSize}
            />
          ) : (
            <AppShell title="加载中…">
              <StatePanel title="正在准备页面" text="页面组件加载中，完成后自动显示。" />
            </AppShell>
          )
        }
      >
        <ExceptionListPage />
      </Suspense>
    </MetaGate>
  );
}
