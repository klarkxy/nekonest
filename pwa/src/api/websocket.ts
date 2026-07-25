import type { NekoMessage } from '@/types/protocol'
import { getPhoneSecret } from './http'

type MessageHandler = (msg: NekoMessage) => void
type StatusHandler = (status: 'connecting' | 'connected' | 'disconnected' | 'auth_error') => void

/**
 * NekoNest WebSocket 客户端
 * 首包必须 subscribe（带 device_id + secret），否则 Server 会断开。
 */
class NekoWebSocket {
  private ws: WebSocket | null = null
  private url: string = ''
  private handlers = new Map<string, MessageHandler>()
  private statusHandlers = new Map<string, StatusHandler>()
  private subscribedDevice: string | null = null
  private reconnectTimer: number | null = null
  private reconnectAttempts = 0
  private intentionalClose = false

  connect(serverUrl?: string) {
    if (serverUrl) this.url = serverUrl
    if (!this.url) {
      const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:'
      this.url = `${protocol}//${location.host}/ws/phone`
    }

    // Need a device to subscribe on first message
    if (!this.subscribedDevice) {
      this.setStatus('disconnected')
      return
    }

    this.intentionalClose = false
    this.setStatus('connecting')

    try {
      // Close previous socket cleanly
      if (this.ws) {
        try {
          this.ws.onclose = null
          this.ws.close()
        } catch {
          /* ignore */
        }
      }

      this.ws = new WebSocket(this.url)

      this.ws.onopen = () => {
        this.reconnectAttempts = 0
        // First message MUST be subscribe with device_id (+ secret)
        this.sendSubscribe()
        this.setStatus('connected')
      }

      this.ws.onmessage = (event) => {
        try {
          const msg: NekoMessage = JSON.parse(event.data)
          if (msg.type === 'error') {
            const m = (msg.payload as { message?: string })?.message || ''
            if (m.includes('unauthorized')) {
              this.setStatus('auth_error')
            }
          }
          this.handlers.forEach(h => h(msg))
        } catch (err) {
          console.error('[ws] parse error:', err)
        }
      }

      this.ws.onclose = () => {
        this.setStatus('disconnected')
        if (!this.intentionalClose) this.scheduleReconnect()
      }

      this.ws.onerror = () => {
        this.setStatus('disconnected')
      }
    } catch (err) {
      console.error('[ws] connect error:', err)
      this.scheduleReconnect()
    }
  }

  /** Subscribe to a device; connects if needed. */
  subscribe(deviceId: string) {
    const changed = this.subscribedDevice !== deviceId
    this.subscribedDevice = deviceId

    if (this.ws?.readyState === WebSocket.OPEN) {
      if (changed) this.sendSubscribe()
    } else if (this.ws?.readyState !== WebSocket.CONNECTING) {
      this.connect()
    }
  }

  private sendSubscribe() {
    if (!this.subscribedDevice) return
    const secret = getPhoneSecret()
    this.send({
      type: 'subscribe',
      device_id: this.subscribedDevice,
      timestamp: Math.floor(Date.now() / 1000),
      payload: secret ? { secret } : {}
    })
  }

  send(msg: Partial<NekoMessage>) {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(msg))
    }
  }

  addHandler(id: string, handler: MessageHandler) {
    this.handlers.set(id, handler)
  }

  removeHandler(id: string) {
    this.handlers.delete(id)
  }

  /** Register a named status listener (replaces previous handler with same id). */
  onStatusChange(id: string, handler: StatusHandler) {
    this.statusHandlers.set(id, handler)
  }

  removeStatusHandler(id: string) {
    this.statusHandlers.delete(id)
  }

  getSubscribedDevice(): string | null {
    return this.subscribedDevice
  }

  isConnected(): boolean {
    return this.ws?.readyState === WebSocket.OPEN
  }

  disconnect() {
    this.intentionalClose = true
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer)
      this.reconnectTimer = null
    }
    this.ws?.close()
    this.ws = null
    this.setStatus('disconnected')
  }

  private setStatus(status: 'connecting' | 'connected' | 'disconnected' | 'auth_error') {
    this.statusHandlers.forEach(h => h(status))
  }

  private scheduleReconnect() {
    if (this.reconnectTimer || this.intentionalClose) return
    if (!this.subscribedDevice) return
    this.reconnectAttempts++
    const delay = Math.min(this.reconnectAttempts * 2000, 30000)
    this.reconnectTimer = window.setTimeout(() => {
      this.reconnectTimer = null
      this.connect()
    }, delay)
  }
}

let instance: NekoWebSocket | null = null

export function nekoWS(): NekoWebSocket {
  if (!instance) {
    instance = new NekoWebSocket()
    // Do not connect until subscribe(deviceId) is called
  }
  return instance
}
