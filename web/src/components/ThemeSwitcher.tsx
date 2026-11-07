import { useState } from 'react';
import { cn } from '../lib/cn';
import { THEME_OPTIONS, initialTheme, setTheme, type Theme } from '../lib/theme';

/**
 * 顶栏配色主题切换：分段控件（三个按钮共用一个圆角容器，中间 1px 分隔线）。
 *
 * - `role="group" aria-label="配色主题"` + 每个按钮 `aria-pressed` 标当前项；
 * - 点击只改 `document.documentElement.dataset.theme` 并写 localStorage，**不刷新页面**；
 * - 原生 button 即可键盘可达（Tab + Enter/Space），不额外加键盘处理。
 */
export function ThemeSwitcher() {
  const [theme, setCurrent] = useState<Theme>(initialTheme);

  const choose = (next: Theme) => {
    setTheme(next);
    setCurrent(next);
  };

  return (
    <div
      role="group"
      aria-label="配色主题"
      className="inline-flex shrink-0 items-center overflow-hidden rounded-sm border border-border bg-surface"
    >
      {THEME_OPTIONS.map((option, index) => {
        const active = option.value === theme;
        return (
          <button
            key={option.value}
            type="button"
            aria-pressed={active}
            onClick={() => choose(option.value)}
            className={cn(
              'h-8 px-3 text-sm',
              index > 0 && 'border-l border-border',
              active ? 'bg-primary-weak font-medium text-primary' : 'text-muted hover:bg-surface-2',
            )}
          >
            {option.label}
          </button>
        );
      })}
    </div>
  );
}
