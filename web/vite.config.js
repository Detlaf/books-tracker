import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// The Go server listens on :8080 by default (see internal/config). Proxying in
// dev keeps the frontend on same-origin relative URLs, so VITE_API_BASE only
// has to be set for deployments that split the two hosts.
export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    proxy: {
      '/auth': 'http://localhost:8080',
      '/books': 'http://localhost:8080',
      '/library': 'http://localhost:8080',
      '/me': 'http://localhost:8080',
      '/health': 'http://localhost:8080',
    },
  },
})
