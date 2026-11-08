import * as Dialog from '@radix-ui/react-dialog';
import { X } from 'lucide-react';
import type { ReactNode } from 'react';

export interface DrawerProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: ReactNode;
  description?: string;
  children: ReactNode;
  footer?: ReactNode;
}

// Radix 的 Presence 只等 animationend、不等 transition，所以进出场用 keyframes；
// 系统开启"减少动效"时整段媒体查询不生效，元素直接出现/消失。
const motionCSS = `
@keyframes iot-drawer-in { from { transform: translateX(100%); } to { transform: translateX(0); } }
@keyframes iot-drawer-out { from { transform: translateX(0); } to { transform: translateX(100%); } }
@keyframes iot-overlay-in { from { opacity: 0; } to { opacity: 1; } }
@keyframes iot-overlay-out { from { opacity: 1; } to { opacity: 0; } }
@media (prefers-reduced-motion: no-preference) {
  .iot-drawer-overlay[data-state="open"] { animation: iot-overlay-in 200ms ease-out; }
  .iot-drawer-overlay[data-state="closed"] { animation: iot-overlay-out 200ms ease-in; }
  .iot-drawer-panel[data-state="open"] { animation: iot-drawer-in 200ms cubic-bezier(0.32, 0.72, 0, 1); }
  .iot-drawer-panel[data-state="closed"] { animation: iot-drawer-out 200ms ease-in; }
}
`;

/** 右侧抽屉：唯一浮层容器，记录查看/编辑都复用它。 */
export function Drawer({ open, onOpenChange, title, description, children, footer }: DrawerProps) {
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <style>{motionCSS}</style>
      <Dialog.Portal>
        <Dialog.Overlay className="iot-drawer-overlay fixed inset-0 z-40 bg-black/40" />
        <Dialog.Content
          className="iot-drawer-panel fixed inset-y-0 right-0 z-50 flex h-full w-full flex-col border-l border-line bg-surface shadow-panel outline-none sm:w-[520px]"
          {...(description ? {} : { 'aria-describedby': undefined })}
        >
          <div className="flex items-start justify-between gap-4 border-b border-line px-5 py-4">
            <div className="min-w-0">
              <Dialog.Title className="truncate text-base font-semibold text-fg">{title}</Dialog.Title>
              {description && (
                <Dialog.Description className="mt-1 text-xs text-muted">{description}</Dialog.Description>
              )}
            </div>
            <Dialog.Close asChild>
              <button
                type="button"
                aria-label="关闭"
                className="shrink-0 rounded-md p-1 text-muted hover:bg-surface-2 hover:text-fg"
              >
                <X size={16} />
              </button>
            </Dialog.Close>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">{children}</div>
          {footer && (
            <div className="flex items-center justify-end gap-2 border-t border-line px-5 py-3">{footer}</div>
          )}
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
