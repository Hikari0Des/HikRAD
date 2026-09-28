import react from '@vitejs/plugin-react'
import { defineConfig } from 'vitest/config'

export default defineConfig({
  plugins: [react()],
  // Panel is served at the site root by Caddy (contract C5).
  base: '/',
  // FR-126: closed-source distribution — never ship source maps. Vite
  // already defaults to false, but a frozen contract shouldn't rely on an
  // upstream default that could change across a Vite major bump; the
  // release-manifest CI gate (scripts/lint-release-manifest.sh) is what
  // actually enforces this, this is the config-side statement of intent.
  build: {
    sourcemap: false,
  },
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
