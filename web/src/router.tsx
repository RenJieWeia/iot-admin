import { Navigate, Route, Routes } from 'react-router';
import { useMeta } from './api/meta';
import { AppShell } from './components/AppShell';
import { ErrorState } from './components/ErrorState';
import { StatePanel } from './components/StatePanel';
import { LazyEntityPage, LazyExceptionPage, LazyReadonlyPage } from './pages/LazyRoutes';

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
      <Route path="/:entity" element={<LazyEntityPage />} />
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}
