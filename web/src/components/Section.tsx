import type { ReactNode } from 'react';
import { cn } from '../lib/cn';

interface SectionProps {
  title: string;
  description?: string;
  /** 右侧动作位（最多 1 个主按钮 + 次级动作）。 */
  actions?: ReactNode;
  children: ReactNode;
}

/**
 * 页面分区外壳：统一圆角/边框/底色/内边距与分区之间的间距。
 * 页面里所有顶层分区都用它，避免各写一套 padding/margin 导致间距不一致。
 */
export function Section({ title, description, actions, children }: SectionProps) {
  return (
    <section className="mb-4 rounded-md border border-border bg-surface p-4" aria-label={title}>
      <header className="mb-3 flex flex-wrap items-center gap-3">
        <h2 className="m-0 text-lg">{title}</h2>
        {description && <span className="text-meta text-muted">{description}</span>}
        {actions && <div className="ml-auto flex items-center gap-2">{actions}</div>}
      </header>
      <div className="flex flex-col gap-4">{children}</div>
    </section>
  );
}

interface SubsectionProps {
  title: string;
  description?: string;
  /** 子块右上角动作位（保存按钮放这里）。 */
  actions?: ReactNode;
  /** 有未保存修改/提交失败时给子块一个可见的状态边框。 */
  tone?: 'default' | 'warning';
  children: ReactNode;
}

/** 分区内的子块：比 Section 轻一档（更浅的边框、更小的标题），保存按钮在其右上角。 */
export function Subsection({ title, description, actions, tone = 'default', children }: SubsectionProps) {
  return (
    <div
      role="group"
      aria-label={title}
      className={cn(
        'rounded-md border p-3',
        tone === 'warning' ? 'border-warning' : 'border-border-subtle',
      )}
    >
      <header className="mb-2 flex flex-wrap items-center gap-3">
        <h3 className="m-0 text-section">{title}</h3>
        {description && <span className="text-meta text-muted">{description}</span>}
        {actions && <div className="ml-auto flex items-center gap-2">{actions}</div>}
      </header>
      {children}
    </div>
  );
}
