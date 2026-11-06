import type { ReactNode } from 'react';
import { useMeta } from '../api/meta';
import { AppShell } from './AppShell';
import { ErrorState } from './ErrorState';
import { SkeletonCell } from './Skeleton';

/**
 * 页面级占位：还不知道对象中文名时（`_meta` 未就绪、地址无效）用它。
 * §16.4：页面级加载态用骨架、不写「加载中…」；状态只给屏幕阅读器。
 */
export function PagePlaceholder({ status }: { status: string }) {
  return (
    <>
      <p className="visually-hidden" role="status">
        {status}
      </p>
      <div className="flex flex-col gap-3" aria-hidden="true">
        <SkeletonCell />
        <SkeletonCell short />
        <SkeletonCell short />
      </div>
    </>
  );
}

interface Props {
  children: ReactNode;
}

/**
 * 字段元数据闸门：`_meta` 未就绪前不渲染任何页面主体（含 lazy 的 Suspense 边界）。
 * 这样 lazy 的 fallback 一定拿得到列数，骨架表头/列数与真实表格一致，不会出现列数跳变。
 * 这一层属于**外壳态**，不是列表四态之一（四态从表格挂载后才开始判定）。
 */
export function MetaGate({ children }: Props) {
  const meta = useMeta();

  if (meta.isPending) {
    return (
      <AppShell title="IoT 管理后台">
        <PagePlaceholder status="正在加载字段元数据…" />
      </AppShell>
    );
  }

  if (meta.isError) {
    return (
      <AppShell title="管理后台">
        <ErrorState
          title="字段元数据加载失败"
          text="页面无法确定列与筛选能力，请重试或检查 /admin/v1/_meta 是否可用。"
          onRetry={() => void meta.refetch()}
        />
      </AppShell>
    );
  }

  return <>{children}</>;
}
