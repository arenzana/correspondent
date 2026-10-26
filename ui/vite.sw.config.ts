// Builds the service worker as one classic script at /sw.js (module
// service workers aren't available on every Safari Correspondent supports).
// Its cache version is a hash of the built app shell, so a new version is
// offered only when the app actually changed.
import { defineConfig } from 'vite'
import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

const shell = readFileSync(resolve(import.meta.dirname, '../web/dist/index.html'))
const buildID = createHash('sha256').update(shell).digest('hex').slice(0, 12)

export default defineConfig({
  define: { __BUILD_ID__: JSON.stringify(buildID) },
  build: {
    outDir: '../web/dist',
    emptyOutDir: false,
    target: 'es2022',
    copyPublicDir: false,
    lib: {
      entry: resolve(import.meta.dirname, 'src/sw.ts'),
      formats: ['iife'],
      name: 'sw',
      fileName: () => 'sw.js',
    },
  },
})
