import { createApp } from 'vue'
import { createPinia } from 'pinia'
import router from './router'
import App from './App.vue'

import './styles/neko.css'

const app = createApp(App)
app.use(createPinia())
app.use(router)
app.mount('#app')

// P2-C: Register Service Worker for offline support + push notifications
if ('serviceWorker' in navigator) {
  window.addEventListener('load', () => {
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
