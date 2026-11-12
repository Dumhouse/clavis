import { fileURLToPath, URL } from 'node:url'

import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// The Go server proxies SPA requests to this dev server (M0-29). In return,
// Vite proxies API calls to the Go server so both workflows work.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      '/api': 'http://127.0.0.1:8080',
      '/healthz': 'http://127.0.0.1:8080',
      '/readyz': 'http://127.0.0.1:8080',
      '/metrics': 'http://127.0.0.1:8080',
    },
  },
  build: {
    // dist is embedded into the server binary; set CLAVIS_SOURCEMAP=1 to debug a build locally.
    sourcemap: process.env.CLAVIS_SOURCEMAP === '1',
    target: 'es2022',
    // The single-binary SPA ships one JS bundle; gzip stays around 145 kB.
    chunkSizeWarningLimit: 700,
  },
})
