import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { App } from './App';
import { applyTheme, readStoredTheme } from './lib/theme';
import './styles/global.css';
import './styles/tailwind.css';

// 首帧防闪烁：必须在 createRoot().render() **之前**同步落主题。
// 不用 useEffect（会先渲染一帧默认主题再切换），也不写内联脚本（CSP script-src 'self'）。
applyTheme(readStoredTheme());

const root = document.getElementById('root');
if (!root) {
  throw new Error('缺少 #root 挂载点');
}

createRoot(root).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
