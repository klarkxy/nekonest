import { createApp } from 'vue'
import { createPinia } from 'pinia'
import router from './router'
import App from './App.vue'

import './styles/neko.css'

const app = createApp(App)
app.use(createPinia())
app.use(router)
app.mount('#app')

// PWA: Workbox injectManifest SW (precache + push) — see src/sw.ts
if ('serviceWorker' in navigator) {
  window.addEventListener('load', () => {
    // vite-plugin-pwa emits sw.js from src/sw.ts in production builds
    navigator.serviceWorker
      .register('/sw.js')
      .then((reg) => {
        console.log('[sw] registered, scope:', reg.scope)
      })
      .catch((err) => {
        console.warn('[sw] registration failed:', err)
      })
  })
}
