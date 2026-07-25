// NekoNest Service Worker
// Handles push notifications and background sync

const CACHE_NAME = 'nekonest-v1'

// Install event — cache static assets
self.addEventListener('install', (event) => {
  event.waitUntil(
    caches.open(CACHE_NAME).then((cache) => {
      return cache.addAll([
        '/',
        '/static/',
      ]).catch(() => {
        // Ignore cache failures during install
      })
    })
  )
  self.skipWaiting()
})

// Activate event — clean old caches
self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys().then((keys) => {
      return Promise.all(
        keys.filter((key) => key !== CACHE_NAME).map((key) => caches.delete(key))
      )
    })
  )
  self.clients.claim()
})

// Push notification event
self.addEventListener('push', (event) => {
  if (!event.data) return

  let data
  try {
    data = event.data.json()
  } catch {
    data = { title: 'NekoNest', body: event.data.text() }
  }

  const title = data.title || 'NekoNest 🐱'
  const options = {
    body: data.body || '',
    icon: '/static/icon-192.png',
    badge: '/static/badge-72.png',
    vibrate: [200, 100, 200],
    data: {
      url: data.url || '/',
      deviceId: data.device_id || '',
      sessionId: data.session_id || '',
    },
    actions: data.actions || [],
    tag: data.tag || 'nekonest-default',
    renotify: true,
  }

  event.waitUntil(
    self.registration.showNotification(title, options)
  )
})

// Notification click — open the relevant page
self.addEventListener('notificationclick', (event) => {
  event.notification.close()

  const url = event.notification.data?.url || '/'

  event.waitUntil(
    self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then((clients) => {
      // Focus existing window if available
      for (const client of clients) {
        if (client.url.includes(self.location.origin) && 'focus' in client) {
          client.navigate(url)
          return client.focus()
        }
      }
      // Open new window
      return self.clients.openWindow(url)
    })
  )
})

// Fetch event — serve from cache, fallback to network
self.addEventListener('fetch', (event) => {
  // Skip non-GET requests
  if (event.request.method !== 'GET') return

  // Skip API and WebSocket requests
  const url = new URL(event.request.url)
  if (url.pathname.startsWith('/api/') || url.pathname.startsWith('/ws/')) return

  event.respondWith(
    caches.match(event.request).then((cached) => {
      if (cached) return cached
      return fetch(event.request).catch(() => {
        // Offline fallback
        if (event.request.mode === 'navigate') {
          return caches.match('/')
        }
        return new Response('Offline', { status: 503 })
      })
    })
  )
})
