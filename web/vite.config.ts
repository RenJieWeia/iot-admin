import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';

// base 固定 /ui/：产物由 Go 进程在 /ui/ 下静态服务，资源前缀必须与路由一致。
// 产物直接落进 internal/ui/dist 并入仓（R-016），生产构建不依赖 node。
export default defineConfig({
  base: '/ui/',
  plugins: [react(), tailwindcss()],
  build: {
    outDir: '../internal/ui/dist',
    emptyOutDir: true,
    sourcemap: false,
    rollupOptions: {
      output: {
        // vendor 分组按「变更频率」切，不按体积切：框架/组件库半年不动，业务代码天天动。
        // 收益是长缓存与警告消除，不降首屏字节；首屏改善来自路由级 lazy。
        manualChunks(id: string) {
          if (!id.includes('node_modules')) return undefined;
          if (id.includes('react-router')) return 'vendor-router';
          if (id.includes('@tanstack')) return 'vendor-tanstack';
          if (id.includes('@radix-ui')) return 'vendor-radix';
          if (id.includes('/zod/')) return 'vendor-zod';
          if (id.includes('react-dom') || id.includes('/react/') || id.includes('scheduler')) {
            return 'vendor-react';
          }
          return 'vendor-misc';
        },
      },
    },
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
