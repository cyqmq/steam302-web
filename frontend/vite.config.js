import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import UnoCSS from 'unocss/vite'

export default defineConfig({
  base: './',
  plugins: [vue(), UnoCSS()],
  build: {
    outDir: '../internal/webui/static',
    emptyOutDir: true,
    assetsDir: 'assets',
    target: 'es2018',
    chunkSizeWarningLimit: 1024
  }
})