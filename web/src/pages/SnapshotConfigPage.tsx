import { useMeta } from '../api/meta';
import { AppShell } from '../components/AppShell';
import { ErrorState } from '../components/ErrorState';
import { Section } from '../components/Section';
import { SnapshotBindingsSection } from '../components/SnapshotBindingsSection';
import { SnapshotRetentionSection } from '../components/SnapshotRetentionSection';
import { SnapshotSlotConfigSection } from '../components/SnapshotSlotConfigSection';

/**
 * 快照配置页（导航仍是「快照计划」一项）：两段式
 *   1) 存储策略：子块「六档配置」+ 子块「保留期」，各自右上角一个保存（分段批量）
 *   2) 数据项绑定：专用表单做作用域 → 目标 → 字段 的级联
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
    <AppShell title={entity.title} subtitle="存储策略 · 数据项绑定">
      {/* 两段式：存储策略（六档 + 保留期两个子块） / 数据项绑定。 */}
      <Section title="存储策略" description="全局六档配置与表级保留期，各自批量保存">
        <SnapshotSlotConfigSection />
        <hr className="m-0 border-0 border-t border-border-subtle" aria-hidden="true" />
        <SnapshotRetentionSection />
      </Section>
      <SnapshotBindingsSection />
    </AppShell>
  );
}
