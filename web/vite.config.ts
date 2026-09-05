import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    // The dev server stands in for the Edge, so /api reaches the BFF without
    // running nginx. Under docker compose the Edge does this instead.
    proxy: { '/api': { target: 'http://localhost:8081', changeOrigin: true } },
  },
  test: {
    environment: 'jsdom',
    setupFiles: './src/test-setup.ts',
    globals: true,
    coverage: {
      provider: 'v8',
      reporter: ['text-summary', 'lcov'],
      // Entry points and icon markup carry no logic worth asserting on.
      exclude: ['src/main.tsx', 'src/test-setup.ts', 'src/components/icons.tsx', '**/*.test.*'],
      thresholds: { statements: 80, branches: 75, functions: 75, lines: 80 },
    },
  },
});
