import { createApp } from 'vue'
import { createPinia } from 'pinia'
import router from './router'
import App from './App.vue'
import i18n, { applyDocumentLang } from './i18n'
import { applyTheme, watchSystemTheme } from './i18n/theme'
import { registerAppServiceWorker } from './utils/serviceWorker'
import { exchangeHandoffTicket, saveHandoffFailure, takeHandoffTicketFromFragment } from './api/handoff'
import { isManagedRuntime, loadRuntimeConfig } from './config/runtimeEndpoint'

import './styles/neko.css'

applyDocumentLang()
applyTheme()
watchSystemTheme(() => {})

async function startApp() {
  let handoffFailed = false
  // Remove the one-time capability before runtime-config or any other fetch.
  const handoffTicket = takeHandoffTicketFromFragment()
  try {
    await loadRuntimeConfig()
    if (handoffTicket) await exchangeHandoffTicket(handoffTicket)
  } catch (error) {
    if (handoffTicket || isManagedRuntime()) {
      saveHandoffFailure(error)
      handoffFailed = true
    }
  }

  const app = createApp(App)
  app.use(createPinia())
  app.use(i18n)
  app.use(router)
  await router.isReady()
  app.mount('#app')
  if (handoffFailed) await router.replace({ name: 'handoff-error' })
}

void startApp()

// PWA: Workbox injectManifest SW (precache + push) — see src/sw.ts
if (import.meta.env.PROD && 'serviceWorker' in navigator) {
  window.addEventListener('load', () => {
    // vite-plugin-pwa emits sw.js from src/sw.ts in production builds
    registerAppServiceWorker(navigator.serviceWorker)
      .then((reg) => {
        console.log('[sw] registered, scope:', reg.scope)
      })
      .catch((err) => {
        console.warn('[sw] registration failed:', err)
      })
  })
}
