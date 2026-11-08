import { createRoot } from 'react-dom/client';
import { BrowserRouter } from 'react-router-dom';
import { MetaProvider } from './meta';
import { ToastProvider } from './components/Toast';
import { applyStoredTheme } from './components/AppShell';
import App from './App';
import './styles.css';

// 首帧前定主题，避免暗色用户看到白闪。
applyStoredTheme();

const root = document.getElementById('root');
if (!root) throw new Error('缺少 #root 挂载点');

createRoot(root).render(
  <BrowserRouter basename={import.meta.env.BASE_URL.replace(/\/$/, '')}>
    <MetaProvider>
      <ToastProvider>
        <App />
      </ToastProvider>
    </MetaProvider>
  </BrowserRouter>,
);
