import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { Theme } from '@radix-ui/themes';
import { App } from './App';
import { applyTheme, readStoredTheme } from './lib/theme';
import './styles/global.css';
import './styles/tailwind.css';

/**
 * Radix Themes 基座配置（1107016）。
 *
 * - appearance 固定 light：本轮不开深色，也不跟随系统。
 * - accentColor 固定 blue（电蓝）；grayColor 固定 slate（冷灰，与既有蓝灰底一致）。
 * - radius 取 medium，理由见 commit message（控件 6px = --radius-3 精确命中）。
 * - hasBackground=false：背景由 body 的 --color-bg 负责，避免主题容器多刷一层底色。
 */
export const RADIX_THEME_PROPS = {
  appearance: 'light',
  accentColor: 'blue',
  grayColor: 'slate',
  radius: 'medium',
  scaling: '100%',
  hasBackground: false,
} as const;

// 首帧防闪烁：必须在 createRoot().render() **之前**同步落主题。
// 不用 useEffect（会先渲染一帧默认主题再切换），也不写内联脚本（CSP script-src 'self'）。
applyTheme(readStoredTheme());

/**
 * 令牌解析域：`--accent-1…12` / `--gray-1…12` 只由 `[data-accent-color]` / `.radix-themes` 声明，
 * 而 `.radix-themes` 是 #root 内的子元素。tokens.css 的语义变量挂在 `:root` 上，
 * html/body 与 portal（挂在 body 下、Theme 子树之外）取不到这些值会解析成空。
 * 因此把同样的两组属性也钉在 <html> 上：Theme 子树内外拿到同一套调色板，抽屉/toast 不变色。
 */
document.documentElement.setAttribute('data-accent-color', RADIX_THEME_PROPS.accentColor);
document.documentElement.setAttribute('data-gray-color', RADIX_THEME_PROPS.grayColor);

const root = document.getElementById('root');
if (!root) {
  throw new Error('缺少 #root 挂载点');
}

createRoot(root).render(
  <StrictMode>
    <Theme {...RADIX_THEME_PROPS}>
      <App />
    </Theme>
  </StrictMode>,
);
