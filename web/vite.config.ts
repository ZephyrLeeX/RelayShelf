import { fileURLToPath, URL } from 'node:url'
import { configDefaults, defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'
import { VitePWA } from 'vite-plugin-pwa'

export default defineConfig({
  plugins: [vue(), VitePWA({
    registerType: 'prompt',
    includeAssets: ['favicon.svg', 'icons/pwa-192x192.png', 'icons/pwa-512x512.png'],
    manifest: {
      name: 'RelayShelf', short_name: 'RelayShelf', display: 'standalone', start_url: '/', scope: '/',
      theme_color: '#5b56d6', background_color: '#f5f6fa',
      icons: [
        { src: '/icons/pwa-192x192.png', sizes: '192x192', type: 'image/png' },
        { src: '/icons/pwa-512x512.png', sizes: '512x512', type: 'image/png' },
      ],
    },
    workbox: {
      globPatterns: ['**/*.{js,css,html,ico,png,svg,webmanifest}'],
      navigateFallback: '/index.html',
      navigateFallbackDenylist: [/^\/api\/v1\//],
      runtimeCaching: [],
      cleanupOutdatedCaches: true,
    },
  })],
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
    exclude: [...configDefaults.exclude, 'e2e/**', 'playwright-report/**', 'test-results/**'],
  },
  resolve: { alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) } },
  server: {
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
        configure(proxy) {
          proxy.on('proxyReq', (request) => request.setHeader('Origin', 'http://localhost:8080'))
        },
      },
    },
  },
})
