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
  },
});
