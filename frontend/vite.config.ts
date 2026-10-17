import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

const port = Number(process.env.ATTUNE_DEV_PORT ?? 5173)

export default defineConfig({
  plugins: [react()],
  server: {
    host: '127.0.0.1',
    port,
    strictPort: true,
    ...(process.env.ATTUNE_DEV_PORT ? { hmr: { host: '127.0.0.1', clientPort: port } } : {}),
  },
})
