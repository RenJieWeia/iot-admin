import { useEffect } from 'react';
import { Navigate, Route, Routes, useNavigate } from 'react-router';
import { useMeta } from './api/meta';
import { AppShell } from './components/AppShell';
import { ErrorState } from './components/ErrorState';
import { StatePanel } from './components/StatePanel';
import {
  LazyDeviceTypeDetailPage,
  LazySnapshotConfigPage,
  LazyEntityPage,
  LazyExceptionPage,
  LazyReadonlyPage,
} from './pages/LazyRoutes';

/**
 * 旧深链：属性曾是独立实体页（/ui/thing-model-properties）。
 * 合并进「终端类型详情页」后该地址只剩历史价值——replace 跳到终端类型列表，
 * 并用一次中性提示说明去向，不留无效历史（只处理这一个已知地址，不做通用映射表）。
 */
export const LEGACY_PROPERTIES_PATH = '/thing-model-properties';
export const LEGACY_PROPERTIES_NOTICE = '终端属性已并入终端类型详情页：请在终端类型列表点「查看属性」进入。';

function LegacyPropertiesRedirect() {
  const navigate = useNavigate();
  useEffect(() => {
    navigate('/device-types', { replace: true, state: { notice: LEGACY_PROPERTIES_NOTICE } });
  }, [navigate]);
  return null;
}

/** 根路径跳到第一个实体：目标来自 /_meta，避免在前端硬编码实体名。 */
function IndexRedirect() {
  const meta = useMeta();
  const first = meta.data?.entities[0];
  if (first) return <Navigate to={`/${first.name}`} replace />;

  // 尚未确定目标实体时也给完整的壳与说明，不用裸 div 内联样式（§2/§6/§18）。
  return (
    <AppShell title="IoT 管理后台">
      {meta.isError ? (
        <ErrorState
          title="字段元数据加载失败"
          text="无法确定可访问的实体，请重试；若持续失败，检查 /admin/v1/_meta 是否可用。"
          onRetry={() => void meta.refetch()}
        />
      ) : (
        <StatePanel
          title="正在准备管理后台"
          text="字段元数据加载完成后会自动进入第一个实体，无需手动操作。"
        />
      )}
    </AppShell>
  );
}

/**
 * 路由表挂在 /ui 下（BrowserRouter basename），Go 侧对 /ui/* 做 index.html fallback，
 * 深链刷新可用。声明式路由足够：我们只用 useParams / useSearchParams / Navigate。
 */
export function AppRoutes() {
  return (
    <Routes>
      <Route path="/" element={<IndexRedirect />} />
      <Route path="/exceptions/:slot" element={<LazyExceptionPage />} />
      <Route path="/readonly/:slot" element={<LazyReadonlyPage />} />
      {/* 快照计划：专用三段式页面，先于 /:entity 声明；实体名已改为 snapshot-slots。 */}
      <Route path="/snapshot-slots" element={<LazySnapshotConfigPage />} />
      {/* 老链接兼容：/ui/snapshot-plans 永久转到 /ui/snapshot-slots（replace，不进历史）。 */}
      <Route path="/snapshot-plans" element={<Navigate to="/snapshot-slots" replace />} />
      {/* 终端类型详情页：两段路径，先于 /:entity 声明（/:entity 只吃一段）。 */}
      <Route path="/device-types/:id" element={<LazyDeviceTypeDetailPage />} />
      {/* 旧深链 → 终端类型列表（replace，不留无效历史）。 */}
      <Route path={LEGACY_PROPERTIES_PATH} element={<LegacyPropertiesRedirect />} />
      <Route path="/:entity" element={<LazyEntityPage />} />
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}
