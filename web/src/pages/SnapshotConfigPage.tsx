import { useMeta } from '../api/meta';
import { AppShell } from '../components/AppShell';
import { ErrorState } from '../components/ErrorState';
import { SnapshotBindingsSection } from '../components/SnapshotBindingsSection';
import { SnapshotRetentionSection } from '../components/SnapshotRetentionSection';
import { SnapshotSlotConfigSection } from '../components/SnapshotSlotConfigSection';

/**
 * 快照配置页（导航仍是「快照计划」一项）：自上而下三段
 *   1) 六档配置（全局、六行固定，保存即全站生效）
 *   2) 存储与保留（表级三行，复用既有区块）
 *   3) 绑定列表（可增删；专用表单做作用域 → 目标 → 数据项 的级联）
 * 页面不渲染任何「新增档位」入口：档位是固定枚举，不是可增长的行集。
 */
export function SnapshotConfigPage() {
  const meta = useMeta();
  const entity = meta.data?.entities.find((item) => item.name === 'snapshot-slots');

  if (meta.isError || !entity) {
    return (
      <AppShell title="快照计划">
        <ErrorState
          title={meta.isError ? '字段元数据加载失败' : '快照计划页元数据缺失'}
          text={
            meta.isError
              ? '无法加载 /admin/v1/_meta，请重试。'
              : '当前 _meta 里没有「快照计划」实体（snapshot-slots），请确认后端版本后重试。'
          }
          onRetry={() => void meta.refetch()}
        />
      </AppShell>
    );
  }

  return (
    <AppShell title={entity.title} subtitle="全局六档 · 存储与保留 · 数据项绑定">
      <SnapshotSlotConfigSection />
      <div className="mb-4">
        <SnapshotRetentionSection />
      </div>
      <SnapshotBindingsSection />
    </AppShell>
  );
}
