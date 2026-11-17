import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// In dev the API is proxied so the browser sees one origin (same as production
// behind the Render rewrite). VITE_PROXY_TARGET lets docker-compose point the
// proxy at the backend container.
const proxyTarget = process.env.VITE_PROXY_TARGET ?? 'http://localhost:8080'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    host: true,
    port: 5173,
    strictPort: true,
    // Bind mounts from Windows/macOS hosts don't deliver file events to Docker.
    watch: process.env.VITE_POLL ? { usePolling: true, interval: 300 } : undefined,
    proxy: {
      '/api': { target: proxyTarget, changeOrigin: false },
    },
  },
})
