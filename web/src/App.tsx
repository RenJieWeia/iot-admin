import { Navigate, Route, Routes, useParams } from 'react-router-dom';
import { AppShell, firstPath } from './components/AppShell';
import MetaGate from './components/MetaGate';
import { useMetaState } from './meta';
import type { ListKind } from './api/client';
import { ListPage } from './pages/ListPage';
import { SnapshotPlanPage } from './pages/SnapshotPlanPage';
import { DeviceTypeDetailPage } from './pages/DeviceTypeDetailPage';

/** 路由参数绑到 ListPage 的 props（路由声明仍是 /e/:name、/r/:slot、/x/:slot）。 */
function ListRoute({ kind, param }: { kind: ListKind; param: 'name' | 'slot' }) {
  const params = useParams();
  return <ListPage kind={kind} name={params[param] ?? ''} />;
}

export default function App() {
  const { meta, loading, error, reload } = useMetaState();

  if (!meta) return <MetaGate loading={loading} error={error} onRetry={reload} />;

  const home = firstPath(meta);

  return (
    <Routes>
      <Route element={<AppShell />}>
        <Route
          index
          element={
            home ? (
              <Navigate to={home} replace />
            ) : (
              <p className="text-sm text-muted">当前没有可访问的页面。</p>
            )
          }
        />
        {/* 快照计划是固定六档的配置页，不进通用列表（分组 + 开关 + 固定操作列）。 */}
        <Route path="e/snapshot-slots" element={<SnapshotPlanPage />} />
        {/* 终端类型详情：字段绑定 + 终端实例 */}
        <Route path="e/device-types/:id" element={<DeviceTypeDetailPage />} />
        <Route path="e/:name" element={<ListRoute kind="entity" param="name" />} />
        <Route path="r/:slot" element={<ListRoute kind="readonly" param="slot" />} />
        <Route path="x/:slot" element={<ListRoute kind="exception" param="slot" />} />
        <Route path="*" element={<p className="text-sm text-muted">页面不存在。</p>} />
      </Route>
    </Routes>
  );
}
