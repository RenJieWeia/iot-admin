import { useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { ChevronRight } from 'lucide-react';
import { cn } from '../lib/cn';

export interface RowMenuItem {
  key: string;
  label: string;
  /** 分组标题：与前一项分组不同时渲染分隔线。 */
  section?: string;
  danger?: boolean;
  onSelect?: () => void;
}

export interface RowMenuGroup {
  key: string;
  label: string;
  section?: string;
  children: RowMenuItem[];
}

export type RowMenuEntry = RowMenuItem | RowMenuGroup;

interface RowMenuProps {
  x: number;
  y: number;
  items: RowMenuEntry[];
  onClose: () => void;
}

function isGroup(entry: RowMenuEntry): entry is RowMenuGroup {
  return 'children' in entry;
}

/** 行级右键菜单：按光标定位，支持二级子菜单；点击外部/Esc/滚动关闭。 */
export function RowMenu({ x, y, items, onClose }: RowMenuProps) {
  const ref = useRef<HTMLDivElement>(null);
  const [openGroup, setOpenGroup] = useState<string | null>(null);

  useEffect(() => {
    const onPointerDown = (event: PointerEvent) => {
      if (ref.current && !ref.current.contains(event.target as Node)) onClose();
    };
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose();
    };
    const onScroll = () => onClose();
    document.addEventListener('pointerdown', onPointerDown, true);
    document.addEventListener('keydown', onKey);
    window.addEventListener('scroll', onScroll, true);
    ref.current?.querySelector<HTMLButtonElement>('[role="menuitem"]')?.focus();
    return () => {
      document.removeEventListener('pointerdown', onPointerDown, true);
      document.removeEventListener('keydown', onKey);
      window.removeEventListener('scroll', onScroll, true);
    };
  }, [onClose]);

  const width = 240;
  const left = Math.max(8, Math.min(x, window.innerWidth - width - 8));
  const top = Math.max(8, Math.min(y, window.innerHeight - 360 - 8));
  // 右侧空间不足时子菜单向左展开。
  const submenuSide = left + width * 2 > window.innerWidth ? 'right-full mr-1' : 'left-full ml-1';

  return createPortal(
    <div
      ref={ref}
      role="menu"
      aria-label="对象操作"
      style={{ left, top, width }}
      className="fixed z-50 min-w-44 rounded-lg border border-line bg-surface p-1 shadow-panel"
    >
      {items.map((entry, index) => {
        const previous = items[index - 1];
        const showSection = entry.section && previous?.section !== entry.section;
        const sectionHeader = showSection ? (
          <p className="px-2 pt-2 pb-0.5 text-xs text-muted">{entry.section}</p>
        ) : null;

        if (isGroup(entry)) {
          const expanded = openGroup === entry.key;
          return (
            <div key={entry.key}>
              {sectionHeader}
              <div
                className="relative"
                onMouseEnter={() => setOpenGroup(entry.key)}
                onMouseLeave={() => setOpenGroup((current) => (current === entry.key ? null : current))}
              >
                <button
                  type="button"
                  role="menuitem"
                  aria-haspopup="menu"
                  aria-expanded={expanded}
                  onClick={() => setOpenGroup((current) => (current === entry.key ? null : entry.key))}
                  onKeyDown={(event) => {
                    if (event.key === 'ArrowRight' || event.key === 'Enter' || event.key === ' ') {
                      event.preventDefault();
                      setOpenGroup(entry.key);
                    }
                    if (event.key === 'ArrowLeft') setOpenGroup(null);
                  }}
                  className="flex w-full items-center justify-between gap-2 rounded px-2 py-1.5 text-left text-sm text-fg outline-none hover:bg-surface-2"
                >
                  {entry.label}
                  <ChevronRight className="size-3.5 text-muted" aria-hidden />
                </button>
                {expanded ? (
                  <div
                    role="menu"
                    aria-label={entry.label}
                    className={cn(
                      'absolute top-0 z-10 min-w-44 rounded-lg border border-line bg-surface p-1 shadow-panel',
                      submenuSide,
                    )}
                  >
                    {entry.children.map((child) => (
                      <button
                        key={child.key}
                        type="button"
                        role="menuitem"
                        onClick={() => {
                          child.onSelect?.();
                          onClose();
                        }}
                        className="flex w-full items-center rounded px-2 py-1.5 text-left text-sm text-fg outline-none hover:bg-surface-2"
                      >
                        {child.label}
                      </button>
                    ))}
                    {entry.children.length === 0 ? (
                      <p className="px-2 py-1.5 text-xs text-muted">没有可用的值</p>
                    ) : null}
                  </div>
                ) : null}
              </div>
            </div>
          );
        }

        return (
          <div key={entry.key}>
            {sectionHeader}
            <button
              type="button"
              role="menuitem"
              onClick={() => {
                entry.onSelect?.();
                onClose();
              }}
              className={cn(
                'flex w-full items-center rounded px-2 py-1.5 text-left text-sm outline-none',
                entry.danger ? 'text-negative hover:bg-negative-soft' : 'text-fg hover:bg-surface-2',
              )}
            >
              {entry.label}
            </button>
          </div>
        );
      })}
    </div>,
    document.body,
  );
}
