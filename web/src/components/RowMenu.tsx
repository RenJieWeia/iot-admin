import { useEffect, useRef } from 'react';
import { createPortal } from 'react-dom';
import { cn } from '../lib/cn';

export interface RowMenuItem {
  key: string;
  label: string;
  /** 分组标题：与前一项分组不同时渲染分隔线。 */
  section?: string;
  danger?: boolean;
  onSelect: () => void;
}

interface RowMenuProps {
  x: number;
  y: number;
  items: RowMenuItem[];
  onClose: () => void;
}

/** 行级右键菜单：按光标定位，点击外部/Esc/滚动关闭。 */
export function RowMenu({ x, y, items, onClose }: RowMenuProps) {
  const ref = useRef<HTMLDivElement>(null);

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

  // 简单夹取，避免菜单超出视口。
  const width = 240;
  const height = Math.min(items.length * 34 + 24, 360);
  const left = Math.max(8, Math.min(x, window.innerWidth - width - 8));
  const top = Math.max(8, Math.min(y, window.innerHeight - height - 8));

  const menu = (
    <div
      ref={ref}
      role="menu"
      aria-label="对象操作"
      style={{ left, top, width }}
      className="fixed z-50 min-w-44 rounded-lg border border-line bg-surface p-1 shadow-panel"
    >
      {items.map((item, index) => (
        <div key={item.key}>
          {item.section && items[index - 1]?.section !== item.section ? (
            <p className="px-2 pt-2 pb-0.5 text-xs text-muted">{item.section}</p>
          ) : null}
          <button
            type="button"
            role="menuitem"
            onClick={() => {
              item.onSelect();
              onClose();
            }}
            className={cn(
              'flex w-full items-center rounded px-2 py-1.5 text-left text-sm outline-none',
              item.danger ? 'text-negative hover:bg-negative-soft' : 'text-fg hover:bg-surface-2',
            )}
          >
            {item.label}
          </button>
        </div>
      ))}
    </div>
  );

  // 页面容器有入场动画（transform），fixed 会被当成相对该容器定位；挂到 body 才与光标对齐。
  return createPortal(menu, document.body);
}
