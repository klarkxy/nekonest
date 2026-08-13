import { pushURL } from '@/config/runtimeEndpoint'
import { authHeaders } from './http'

async function pushFetch(path: string, init: RequestInit = {}): Promise<Response> {
  return fetch(pushURL(path), {
    ...init,
    cache: 'no-store',
    headers: authHeaders(init.headers)
  })
}

/** Request notification permission and subscribe if VAPID is configured. */
export async function ensurePushSubscription(deviceId: string): Promise<boolean> {
  if (!deviceId || !('serviceWorker' in navigator) || !('PushManager' in window)) {
    return false
  }
  try {
    const res = await pushFetch('/api/push/vapid-public-key')
    if (!res.ok) return false
    const data = (await res.json()) as { enabled?: boolean; public_key?: string }
    if (!data.enabled || !data.public_key) return false

    const perm = await Notification.requestPermission()
    if (perm !== 'granted') return false

    const reg = await navigator.serviceWorker.ready
    let sub = await reg.pushManager.getSubscription()
    if (!sub) {
      sub = await reg.pushManager.subscribe({
        userVisibleOnly: true,
        applicationServerKey: urlBase64ToUint8Array(data.public_key)
      })
    }
    const json = sub.toJSON()
    const endpoint = json.endpoint
    const p256dh = json.keys?.p256dh
    const auth = json.keys?.auth
    if (!endpoint || !p256dh || !auth) return false

    const post = await pushFetch('/api/push/subscribe', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        device_id: deviceId,
        endpoint,
        p256dh,
        auth
      })
    })
    return post.ok
  } catch (err) {
    console.warn('[push] subscribe failed:', err)
    return false
  }
}

function urlBase64ToUint8Array(base64String: string): BufferSource {
  const padding = '='.repeat((4 - (base64String.length % 4)) % 4)
  const base64 = (base64String + padding).replace(/-/g, '+').replace(/_/g, '/')
  const raw = atob(base64)
  const out = new Uint8Array(raw.length)
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i)
  return out
}
