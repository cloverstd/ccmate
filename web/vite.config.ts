import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    outDir: '../internal/static/dist',
    emptyOutDir: true,
  },
  server: {
    allowedHosts: true,
    proxy: {
      '/api': 'http://localhost:8880',
      '/webhooks': 'http://localhost:8880',
    },
  },
})
