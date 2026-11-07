import type { CSSProperties, ReactNode } from 'react';
import { Card as RadixCard } from '@radix-ui/themes';
import { cn } from '../lib/cn';

/**
 * 内容层卡片（1107019 起由 Radix `<Card size="3">` 承载）：
 * 底色/边框/圆角/内边距全部来自 Radix 主题（--card-padding = --space-5 = 20px、
 * --card-border-radius = --radius-5 = 12px、::before 画底色、::after 画 1px 内边框）。
 *
 * 硬约束（1107010 不变）：**卡片之间平级、不嵌套**。页面外壳（背景层）不画卡片，
 * 面包屑/标题/描述/页面级操作/列表摘要行都留在背景层。
 * 卡片内部只用「小标题 + 分隔线」分区，不再起第二个容器。
 *
 * 统一打 `data-card`，由 card-hierarchy.test.tsx 守卫「无嵌套 / 每页数量」。
 */
export function Card({
  children,
  className,
  tone = 'default',
  label,
}: {
  children: ReactNode;
  className?: string;
  /** 有未保存修改/提交失败时给整张卡一个可见的状态边框。 */
  tone?: 'default' | 'warning';
  /** 卡片的可访问名（读屏与用例用它定位这张卡，如「六档配置」）。 */
  label?: string;
}) {
  /**
   * 警告边框：Radix Card 的边框是 `::after` 上的 `box-shadow: var(--base-card-surface-box-shadow)`，
   * 直接写 `border-*` 会变成第二层边框（双边框）。这里只改这个变量，仍是一层。
   */
  const warningStyle: CSSProperties | undefined =
    tone === 'warning'
      ? ({ ['--base-card-surface-box-shadow' as string]: 'inset 0 0 0 1px var(--color-warning)' } as CSSProperties)
      : undefined;

  return (
    <RadixCard
      size="3"
      data-card
      role={label ? 'group' : undefined}
      aria-label={label}
      className={cn(className)}
      style={warningStyle}
    >
      {children}
    </RadixCard>
  );
}

/** 卡片头：标题 + 说明 + 右侧动作位（保存/新建放这里）。 */
export function CardHeader({
  title,
  description,
  actions,
}: {
  title: string;
  description?: ReactNode;
  actions?: ReactNode;
}) {
  return (
    <header className="mb-3 flex flex-wrap items-center gap-3">
      <h2 className="m-0 text-lg">{title}</h2>
      {description && <span className="text-sm text-muted">{description}</span>}
      {actions && <div className="ml-auto flex items-center gap-2">{actions}</div>}
    </header>
  );
}
