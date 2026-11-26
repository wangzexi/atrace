import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// The page is served at / by atrace; in development the API is proxied to a
// running atrace (ATRACE_URL defaults to the local Go service).
const target = process.env.ATRACE_URL ?? 'http://127.0.0.1:8183'

export default defineConfig({
  base: '/',
  build: { outDir: 'build' },
  plugins: [react()],
  server: {
    // Bind to localhost; set VITE_HOST to a Tailscale address to open it from another machine.
    host: process.env.VITE_HOST ?? '127.0.0.1',
    strictPort: true,
    proxy: {
      '/api': target,
      '/files': target,
      '/manage': target,
      '/login': {
        target,
        // GET stays in the Vite app; only the form submission goes to Go.
        bypass: (request) =>
          request.method === 'GET' || request.method === 'HEAD' ? '/index.html' : undefined,
      },
      '/logout': target,
      '/dev': target,
    },
  },
})
