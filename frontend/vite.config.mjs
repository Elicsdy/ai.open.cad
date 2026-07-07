import { fileURLToPath, URL } from 'node:url'
import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vite'

const appBase = '/ai/open/cad'
const frontendRoot = fileURLToPath(new URL('.', import.meta.url))
const distDir = fileURLToPath(new URL('../dist', import.meta.url))
const cacheDir = fileURLToPath(new URL('../.cache/vite', import.meta.url))

export default defineConfig({
  root: frontendRoot,
  base: './',
  plugins: [vue()],
  cacheDir,
  publicDir: false,
  esbuild: {
    keepNames: true,
  },
  build: {
    outDir: distDir,
    assetsDir: '.',
    emptyOutDir: true,
    minify: false,
  },
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    port: 5173,
    proxy: {
      [`^${appBase}/cascade/.*$`]: {
        target: 'http://localhost:15566',
        changeOrigin: true,
      },
      [`^${appBase}/(health|generate-cad|repair-cad|refine-cad|generate-cad-async|repair-cad-async|refine-cad-async|jobs|projects)(/.*)?$`]: {
        target: 'http://localhost:15566',
        changeOrigin: true,
      },
    },
  },
})
