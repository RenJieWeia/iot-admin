import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';

// base 固定 /ui/：产物由 Go 进程在 /ui/ 下静态服务，资源前缀必须与路由一致。
// 产物直接落进 internal/ui/dist 并入仓（R-016），生产构建不依赖 node。
export default defineConfig({
  base: '/ui/',
  plugins: [react()],
  build: {
    outDir: '../internal/ui/dist',
    emptyOutDir: true,
    sourcemap: false,
  },
  server: {
    proxy: {
      '/admin/v1': 'http://127.0.0.1:8080',
    },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/test/setup.ts'],
    include: ['src/**/*.test.{ts,tsx}'],
    css: false,
    restoreMocks: true,
  },
});
