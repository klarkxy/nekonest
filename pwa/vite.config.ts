import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { VitePWA } from 'vite-plugin-pwa'
import { resolve } from 'path'
import packageJson from './package.json'

export default defineConfig({
  define: {
    __NEKONEST_APP_VERSION__: JSON.stringify(packageJson.version)
  },
  test: {
    environment: 'jsdom',
    include: ['src/**/*.{test,spec}.ts']
  },
  plugins: [
    vue(),
    VitePWA({
      strategies: 'injectManifest',
      srcDir: 'src',
      filename: 'sw.ts',
      registerType: 'autoUpdate',
      injectRegister: false,
      manifest: {
        name: 'NekoNest 猫娘乐园',
        short_name: '猫娘乐园',
        description: '按工作目录和猫娘，整理家里电脑上的线团',
        lang: 'zh-CN',
        theme_color: '#F8F1ED',
        background_color: '#F8F1ED',
        display: 'standalone',
        orientation: 'portrait',
        scope: '/',
        start_url: '/',
        icons: [
          {
            src: '/brand/pwa-192x192.png',
            sizes: '192x192',
            type: 'image/png'
          },
          {
            src: '/brand/pwa-512x512.png',
            sizes: '512x512',
            type: 'image/png'
          },
          {
            src: '/brand/pwa-512x512-maskable.png',
            sizes: '512x512',
            type: 'image/png',
            purpose: 'any maskable'
          },
          {
            src: '/brand/apple-touch-icon.png',
            sizes: '180x180',
            type: 'image/png'
          }
        ]
      },
      injectManifest: {
        globPatterns: ['**/*.{js,css,html,ico,png,svg,webp,woff2}'],
        globIgnores: [
          'runtime-config.json',
          'brand/nekonest-mark-1024.png',
          'brand/apple-touch-icon.png',
          'brand/pwa-192x192.png',
          'brand/pwa-512x512.png',
          'brand/pwa-512x512-maskable.png'
        ]
      },
      devOptions: {
        enabled: false
      }
    })
  ],
  resolve: {
    alias: {
      '@': resolve(__dirname, 'src')
    }
  },
  server: {
    host: '127.0.0.1',
    port: 5173,
    proxy: {
      // NEKONEST_DEV_API (e.g. http://172.x.x.x:18080) points at a remote nest
      // such as WSL2-simulated cloud; default remains local loopback :8080.
      '/ws': {
        target: (process.env.NEKONEST_DEV_API || 'http://127.0.0.1:8080').replace(/^http/, 'ws'),
        ws: true
      },
      '/api': {
        target: process.env.NEKONEST_DEV_API || 'http://127.0.0.1:8080'
      }
    }
  },
  build: {
    outDir: 'dist',
    sourcemap: false,
    minify: 'terser',
    rollupOptions: {
      output: {
        manualChunks: {
          'naive-ui': ['naive-ui'],
          'vue-vendor': ['vue', 'vue-router', 'pinia']
        }
      }
    }
  }
})
