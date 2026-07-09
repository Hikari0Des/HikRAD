import react from '@vitejs/plugin-react'
import { defineConfig } from 'vitest/config'

// @hikrad/shared is a source package (consumers compile src/ directly), so
// this config exists for Vitest only — there is no bundling step here.
export default defineConfig({
  plugins: [react()],
  test: {
    environment: 'jsdom',
    setupFiles: './src/test/setup.ts',
  },
})
