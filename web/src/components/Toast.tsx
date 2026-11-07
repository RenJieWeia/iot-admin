import { useCallback, useEffect, useInsertionEffect, useRef, useState } from 'react';
import { cn } from '../lib/cn';

export interface ToastMessage {
  kind: 'ok' | 'error';
  text: string;
}

interface Props {
  message: ToastMessage | null;
  onDismiss: () => void;
}

/** 成功提示在屏幕上停留的时长（到点后先播退场，不是直接消失）。 */
const AUTO_DISMISS_MS = 4000;
/** 退场时长：播完再卸载，保证「计时到点」不会变成「瞬间消失」。 */
const EXIT_MS = 160;
/**
 * 入场时长：优先取动效令牌 `--dur-base`（由样式线在 tokens.css 落地），
 * 令牌尚未进分支时用同档兜底 200ms——两条路都 ≤200ms，不依赖落地顺序。
 */
const ENTER_DURATION = 'var(--dur-base, 200ms)';

const REDUCED_MOTION_QUERY = '(prefers-reduced-motion: reduce)';

/**
 * 是否要求「减少动态效果」。jsdom / 老环境没有 matchMedia 时按「不减弱」处理，
 * 与 AppShell 的窄屏判定同一套兜底策略。
 */
function prefersReducedMotion(): boolean {
  return (
    typeof window !== 'undefined' &&
    typeof window.matchMedia === 'function' &&
    window.matchMedia(REDUCED_MOTION_QUERY).matches
  );
}

/**
 * 动效关键帧：只动 transform / opacity（不触发布局，也不引入颜色字面量）。
 * 用 useInsertionEffect 注入，保证首帧之前就已生效，不会「先跳到终态再补播」。
 */
const MOTION_CSS = `
@keyframes toast-in {
  from { opacity: 0; transform: translateY(5px); }
  to { opacity: 1; transform: translateY(0); }
}
@keyframes toast-out {
  from { opacity: 1; transform: translateY(0); }
  to { opacity: 0; transform: translateY(5px); }
}
`;

/**
 * 轻提示：不打断、不遮挡，aria-live 播报。
 * 成功 4 秒后自动消失；失败必须留在屏幕上等用户处理（§15：失败要给原因与恢复路径，不能消失）。
 *
 * 动效约定（1107022）：
 * - 入场：translateY(5px) + opacity 0 → 0 / 1，时长 `var(--dur-base, 200ms)`；
 * - 退场：先打上 `data-state="leaving"` 播 160ms 淡出，**播完再卸载**；
 *   自动消失与手动关闭（✕）走同一条退场路径；
 * - `prefers-reduced-motion: reduce` 时两个动画都不播，直接出现 / 直接卸载。
 */
export function Toast({ message, onDismiss }: Props) {
  // 用 ref 存最新回调：父组件重渲染会换 onDismiss 引用，但它不该成为计时依赖，
  // 否则 4 秒会被反复重置。计时只锚定 message 对象本身（setToast 每次产生新对象）。
  const dismissRef = useRef(onDismiss);
  useEffect(() => {
    dismissRef.current = onDismiss;
  });

  // 父组件把 message 置空后，本组件还要留 160ms 播退场，所以自己持有一份「正在显示的内容」。
  const [shown, setShown] = useState<ToastMessage | null>(message);
  const [leaving, setLeaving] = useState(false);
  const shownRef = useRef<ToastMessage | null>(message);
  const exitTimerRef = useRef<number | null>(null);

  useInsertionEffect(() => {
    const style = document.createElement('style');
    style.setAttribute('data-toast-motion', '');
    style.textContent = MOTION_CSS;
    document.head.appendChild(style);
    return () => style.remove();
  }, []);

  const put = useCallback((next: ToastMessage | null) => {
    shownRef.current = next;
    setShown(next);
  }, []);

  const clearExitTimer = useCallback(() => {
    if (exitTimerRef.current !== null) {
      window.clearTimeout(exitTimerRef.current);
      exitTimerRef.current = null;
    }
  }, []);

  /** 结束显示：退场播完（或 reduced-motion 跳过动画）之后才真正卸载。 */
  const finish = useCallback(
    (notify: boolean) => {
      clearExitTimer();
      setLeaving(false);
      put(null);
      if (notify) dismissRef.current();
    },
    [clearExitTimer, put],
  );

  /** 开始退场：先播 160ms 再卸载；reduced-motion 下不播动画，直接结束。 */
  const startExit = useCallback(
    (notify: boolean) => {
      if (prefersReducedMotion()) {
        finish(notify);
        return;
      }
      if (exitTimerRef.current !== null) return; // 已在退场中，不重入
      setLeaving(true);
      exitTimerRef.current = window.setTimeout(() => finish(notify), EXIT_MS);
    },
    [finish],
  );

  // 4 秒自动消失：到点先退场，退场结束才通知父组件。
  useEffect(() => {
    if (!message || message.kind !== 'ok') return undefined;
    const timer = window.setTimeout(() => startExit(true), AUTO_DISMISS_MS);
    return () => window.clearTimeout(timer);
  }, [message, startExit]);

  // 新消息 → 立即显示并取消进行中的退场；父组件外部清空 → 也走退场再卸载。
  useEffect(() => {
    if (message) {
      clearExitTimer();
      setLeaving(false);
      put(message);
      return;
    }
    if (shownRef.current) startExit(false);
  }, [message, clearExitTimer, put, startExit]);

  useEffect(() => clearExitTimer, [clearExitTimer]);

  if (!shown) {
    return <div role="status" aria-live="polite" className="visually-hidden" />;
  }

  const isError = shown.kind === 'error';
  const animate = !prefersReducedMotion();
  const motion = animate
    ? leaving
      ? { animation: `toast-out ${EXIT_MS}ms ease-in forwards` }
      : { animation: `toast-in ${ENTER_DURATION} ease-out` }
    : undefined;

  return (
    <div
      // 内容换了就换 key：让它重新挂载，入场动画会重播（同一条提示不会重播）。
      key={`${shown.kind}:${shown.text}`}
      role={isError ? 'alert' : 'status'}
      aria-live={isError ? 'assertive' : 'polite'}
      data-state={leaving ? 'leaving' : 'open'}
      style={motion}
      className={cn(
        'toast-box fixed right-6 top-6 flex items-start gap-3 rounded-sm border px-4 py-3',
        isError
          ? 'border-negative-text bg-negative-bg text-negative-text'
          : 'border-positive-text bg-positive-bg text-positive-text',
      )}
    >
      <span>{shown.text}</span>
      {isError && (
        <button
          type="button"
          className="shrink-0 cursor-pointer border-0 bg-transparent px-1 leading-none"
          aria-label="关闭提示"
          onClick={() => startExit(true)}
        >
          ✕
        </button>
      )}
    </div>
  );
}
