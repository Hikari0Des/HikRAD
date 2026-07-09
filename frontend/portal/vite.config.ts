import react from '@vitejs/plugin-react'
import { defineConfig } from 'vitest/config'

export default defineConfig({
  plugins: [react()],
  // Caddy serves the portal under /portal with the prefix stripped
  // (handle_path, contract C5) — assets must be referenced at /portal/*.
  base: '/portal/',
  server: {
    // Dev-only convenience: forward API calls to the Compose stack's Caddy
    // (self-signed cert in dev, hence secure: false). Override with
    // VITE_API_PROXY_TARGET, e.g. a bare hikrad-api on http://localhost:8080.
    proxy: {
      '/api': {
        target: process.env.VITE_API_PROXY_TARGET ?? 'https://localhost',
        changeOrigin: true,
        secure: false,
      },
    },
  },
  test: {
    environment: 'jsdom',
    setupFiles: './src/test/setup.ts',
  },
})
