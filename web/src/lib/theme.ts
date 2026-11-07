/**
 * 配色主题：三套（深青 teal / 电蓝 blue / 石墨 graphite）。
 *
 * 主题只覆盖 tokens.css 里那 14 个变量；语义色（成功/警告/危险/信息）与图表色
 * 是三套共享的，不随皮肤变——红永远是危险，琥珀永远是电。
 *
 * 应用时机很关键：必须在 `createRoot().render()` **之前**同步设置
 * `document.documentElement.dataset.theme`，否则首帧会闪默认主题。
 */
export const THEMES = ['teal', 'blue', 'graphite'] as const;
export type Theme = (typeof THEMES)[number];

export const DEFAULT_THEME: Theme = 'teal';
/** localStorage 键名（稳定契约，别改）。 */
export const THEME_STORAGE_KEY = 'iot-theme';

/** 顶栏切换控件的选项（顺序即展示顺序）。 */
export const THEME_OPTIONS: ReadonlyArray<{ value: Theme; label: string }> = [
  { value: 'teal', label: '深青' },
  { value: 'blue', label: '电蓝' },
  { value: 'graphite', label: '石墨' },
];

/**
 * 纯函数：把 localStorage 里的原始值解析成合法主题。
 * 非法值/缺失/大小写不符一律回落到默认的 teal（不抛错，保证首帧永远有主题）。
 */
export function resolveTheme(raw: string | null | undefined): Theme {
  if (typeof raw !== 'string') return DEFAULT_THEME;
  return (THEMES as readonly string[]).includes(raw) ? (raw as Theme) : DEFAULT_THEME;
}

/** 读偏好：隐私模式/禁用存储时 localStorage 会抛错，一律当“没有偏好”。 */
export function readStoredTheme(): Theme {
  try {
    return resolveTheme(window.localStorage.getItem(THEME_STORAGE_KEY));
  } catch {
    return DEFAULT_THEME;
  }
}

/** 应用到 DOM（只改 dataset，不触发 React 重渲染）。 */
export function applyTheme(theme: Theme): void {
  document.documentElement.dataset.theme = theme;
}

/** 切换并持久化；即使存储不可用，视觉切换也必须生效。 */
export function setTheme(theme: Theme): void {
  applyTheme(theme);
  try {
    window.localStorage.setItem(THEME_STORAGE_KEY, theme);
  } catch {
    // 存储不可用时只切当前会话，不中断交互。
  }
}

/**
 * 首帧取主题：优先看 DOM 上已有的 dataset（例如服务端/测试预置），
 * 否则读 localStorage；两者都非法时用默认值。
 */
export function initialTheme(): Theme {
  const fromDom = document.documentElement.dataset.theme;
  return fromDom ? resolveTheme(fromDom) : readStoredTheme();
}
