import * as ToastPrimitive from '@radix-ui/react-toast';
import { AlertCircle, CheckCircle2, Info, X } from 'lucide-react';
import { createContext, useCallback, useContext, useMemo, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { cn } from '../lib/cn';

type ToastTone = 'success' | 'error' | 'info';

interface ToastItem {
  id: number;
  tone: ToastTone;
  message: string;
}

export interface ToastApi {
  success: (message: string) => void;
  error: (message: string) => void;
  info: (message: string) => void;
}

const ToastContext = createContext<ToastApi | null>(null);

const toneIcon: Record<ToastTone, ReactNode> = {
  success: <CheckCircle2 size={16} className="text-positive" />,
  error: <AlertCircle size={16} className="text-negative" />,
  info: <Info size={16} className="text-primary" />,
};

const toneBorder: Record<ToastTone, string> = {
  success: 'border-l-positive',
  error: 'border-l-negative',
  info: 'border-l-primary',
};

/** 轻提示：成功/失败/信息都走这里，不打断当前操作。 */
export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<ToastItem[]>([]);
  const nextId = useRef(0);

  const remove = useCallback((id: number) => {
    setItems((list) => list.filter((item) => item.id !== id));
  }, []);

  const push = useCallback((tone: ToastTone, message: string) => {
    // 只留最近 5 条，避免连续报错时堆满视口。
    setItems((list) => [...list.slice(-4), { id: (nextId.current += 1), tone, message }]);
  }, []);

  const api = useMemo<ToastApi>(
    () => ({
      success: (message) => push('success', message),
      error: (message) => push('error', message),
      info: (message) => push('info', message),
    }),
    [push],
  );

  return (
    <ToastContext.Provider value={api}>
      <ToastPrimitive.Provider swipeDirection="right" duration={4000}>
        {items.map((item) => (
          <ToastPrimitive.Root
            key={item.id}
            open
            onOpenChange={(open) => {
              if (!open) remove(item.id);
            }}
            className={cn(
              'flex items-start gap-2 rounded-md border border-l-2 border-line bg-surface p-3 shadow-panel',
              toneBorder[item.tone],
            )}
          >
            <span className="mt-0.5 shrink-0">{toneIcon[item.tone]}</span>
            <ToastPrimitive.Title className="flex-1 text-sm leading-5 text-fg">
              {item.message}
            </ToastPrimitive.Title>
            <ToastPrimitive.Close
              aria-label="关闭提示"
              className="shrink-0 rounded p-0.5 text-muted hover:bg-surface-2 hover:text-fg"
            >
              <X size={14} />
            </ToastPrimitive.Close>
          </ToastPrimitive.Root>
        ))}
        <ToastPrimitive.Viewport className="fixed bottom-0 right-0 z-[80] m-0 flex w-[380px] max-w-[100vw] list-none flex-col gap-2 p-4 outline-none" />
      </ToastPrimitive.Provider>
      {children}
    </ToastContext.Provider>
  );
}

export function useToast(): ToastApi {
  const api = useContext(ToastContext);
  if (!api) throw new Error('useToast 必须在 ToastProvider 内使用');
  return api;
}
