import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { resolve } from 'node:path'

const port = Number(process.env.ATTUNE_DEV_PORT ?? 5173)
const foliatePDFStub = {
  name: 'attune-foliate-pdf-stub',
  enforce: 'pre' as const,
  resolveId(source: string, importer?: string) {
    if (source === './pdf.js' && importer?.replace(/\\/g, '/').includes('/node_modules/foliate-js/view.js')) {
      return resolve(import.meta.dirname, 'src/readers/foliate-pdf-unavailable.ts')
    }
  },
}

export default defineConfig({
  plugins: [foliatePDFStub, react()],
  server: {
    host: '127.0.0.1',
    port,
    strictPort: true,
    ...(process.env.ATTUNE_DEV_PORT ? { hmr: { host: '127.0.0.1', clientPort: port } } : {}),
  },
})
