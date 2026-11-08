import { Navigate, Route, Routes, useParams } from 'react-router-dom';
import { AppShell, firstPath } from './components/AppShell';
import MetaGate from './components/MetaGate';
import { useMetaState } from './meta';
import type { ListKind } from './api/client';
import { ListPage } from './pages/ListPage';
import { SnapshotPlanPage } from './pages/SnapshotPlanPage';
import { DeviceTypeDetailPage } from './pages/DeviceTypeDetailPage';
import { TerminalDetailPage } from './pages/TerminalDetailPage';
import { SnapshotQueryListPage } from './pages/SnapshotQueryListPage';
import { SnapshotQueryEditPage } from './pages/SnapshotQueryEditPage';
import { SnapshotQueryResultPage } from './pages/SnapshotQueryResultPage';

/** 旧路径兼容：/e/device-types[/:id] → /e/thing-models[/:id]。 */
function LegacyDeviceTypes({ detail = false }: { detail?: boolean }) {
  const params = useParams();
  const target = detail && params.id ? `/e/thing-models/${params.id}` : '/e/thing-models';
  return <Navigate to={target} replace />;
}

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
        {/* 快照查询：模板卡片 → 编辑 → 结果（取代旧的粗表只读页） */}
        <Route path="e/snapshot-queries" element={<SnapshotQueryListPage />} />
        <Route path="e/snapshot-queries/new" element={<SnapshotQueryEditPage />} />
        <Route path="e/snapshot-queries/:id/edit" element={<SnapshotQueryEditPage />} />
        <Route path="e/snapshot-queries/:id/query" element={<SnapshotQueryResultPage />} />
        {/* 物模型详情：字段绑定 + 终端实例；旧 device-types 路径做重定向 */}
        <Route path="e/device-types" element={<LegacyDeviceTypes />} />
        <Route path="e/device-types/:id" element={<LegacyDeviceTypes detail />} />
        <Route path="e/thing-models/:id" element={<DeviceTypeDetailPage />} />
        {/* 终端详情：基础信息 + 类型字段 + 最新值 */}
        <Route path="e/terminals/:id" element={<TerminalDetailPage />} />
        <Route path="e/:name" element={<ListRoute kind="entity" param="name" />} />
        <Route path="r/:slot" element={<ListRoute kind="readonly" param="slot" />} />
        <Route path="x/:slot" element={<ListRoute kind="exception" param="slot" />} />
        <Route path="*" element={<p className="text-sm text-muted">页面不存在。</p>} />
      </Route>
    </Routes>
  );
}
