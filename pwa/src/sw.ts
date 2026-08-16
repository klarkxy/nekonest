/// <reference lib="webworker" />
import { clientsClaim } from 'workbox-core'
import { precacheAndRoute } from 'workbox-precaching'
import { NavigationRoute, registerRoute } from 'workbox-routing'
import { NetworkFirst } from 'workbox-strategies'
import { notificationTag } from './utils/notification'

declare let self: ServiceWorkerGlobalScope & {
  __WB_MANIFEST: Array<string | { url: string; revision?: string | null }>
}

precacheAndRoute(self.__WB_MANIFEST)
clientsClaim()
self.skipWaiting()

// SPA navigations: network first; offline falls back to precached index.html
const navHandler = new NetworkFirst({
  cacheName: 'nekonest-pages',
  networkTimeoutSeconds: 5
})
registerRoute(
  new NavigationRoute(
    async (options) => {
      try {
        const res = await navHandler.handle(options)
        if (res) return res
      } catch {
        /* fall through */
      }
      const cached = await caches.match('/index.html') || await caches.match('/')
      if (cached) return cached
      return Response.error()
    },
    {
      denylist: [/^\/api\//, /^\/ws\//, /^\/health(?:\?|$)/, /^\/runtime-config\.json(?:\?|$)/]
    }
  )
)

self.addEventListener('push', (event) => {
  if (!event.data) return
  let data: {
    title?: string
    body?: string
    url?: string
    device_id?: string
    session_id?: string
    tag?: string
  }
  try {
    data = event.data.json()
  } catch {
    data = { title: 'NekoNest', body: event.data.text() }
  }
  const title = data.title || 'NekoNest'
  const tag = notificationTag(data)
  event.waitUntil(
    self.registration.showNotification(title, {
      body: data.body || '',
      icon: '/brand/pwa-192x192.png',
      badge: '/brand/notification-badge.png',
      data: {
        url: data.url || '/',
        deviceId: data.device_id || '',
        sessionId: data.session_id || ''
      },
      tag
    })
  )
})

function safeNotificationURL(raw: string | undefined): string {
  const value = String(raw || '').trim() || '/'
  try {
    const parsed = new URL(value, self.location.origin)
    if (parsed.origin !== self.location.origin) {
      return '/'
    }
    return `${parsed.pathname}${parsed.search}${parsed.hash}` || '/'
  } catch {
    return '/'
  }
}

self.addEventListener('notificationclick', (event) => {
  event.notification.close()
  const url = safeNotificationURL((event.notification.data as { url?: string } | undefined)?.url)
  event.waitUntil(
    self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then((clientList) => {
      for (const client of clientList) {
        if ('focus' in client) {
          const wc = client as WindowClient
          if ('navigate' in wc) {
            void wc.navigate(url)
          }
          return wc.focus()
        }
      }
      return self.clients.openWindow(url)
    })
  )
})
