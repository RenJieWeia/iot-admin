import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';

// 生产构建产物直接写进 Go embed 目录；dev 期 /admin 代理到本机服务。
export default defineConfig({
  base: '/ui/',
  plugins: [react(), tailwindcss()],
  server: {
    port: 5173,
    proxy: { '/admin': 'http://127.0.0.1:8080' },
  },
  build: {
    outDir: '../internal/ui/dist',
    emptyOutDir: true,
  },
});
