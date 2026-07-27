import type { NekoMessage } from '@/types/protocol'
import { getPhoneSecret } from './http'

type MessageHandler = (msg: NekoMessage) => void
type StatusHandler = (status: 'connecting' | 'connected' | 'disconnected' | 'auth_error') => void

/**
 * NekoNest WebSocket 客户端
 * send() 仅表示帧已交给浏览器 socket；业务 ACK 由 prompt_sent 等完成。
 * 不自动重试用户消息（避免重复 client_msg_id）；断线由上层 outbox 原样重发。
 */
export class NekoWebSocket {
  private ws: WebSocket | null = null
  private url: string = ''
  private handlers = new Map<string, MessageHandler>()
  private statusHandlers = new Map<string, StatusHandler>()
  private subscribedDevice: string | null = null
  private reconnectTimer: number | null = null
  private reconnectAttempts = 0
  private intentionalClose = false
  private status: 'connecting' | 'connected' | 'disconnected' | 'auth_error' = 'disconnected'
  private sessionReady = false
  private onReady: Array<() => void> = []
  /** Invalidates callbacks from sockets that have already been replaced. */
  private generation = 0
  private subscriptionSequence = 0
  private pendingSubscription: {
    id: string
    deviceId: string
    socketGeneration: number
  } | null = null

  connect(serverUrl?: string) {
    if (serverUrl) this.url = serverUrl
    if (!this.url) {
      const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:'
      this.url = `${protocol}//${location.host}/ws/phone`
    }

    if (this.reconnectTimer !== null) {
      window.clearTimeout(this.reconnectTimer)
      this.reconnectTimer = null
    }

    if (!this.subscribedDevice) {
      this.setStatus('disconnected')
      return
    }

    this.intentionalClose = false
    this.sessionReady = false
    this.pendingSubscription = null
    this.setStatus('connecting')

    const generation = ++this.generation
    try {
      const previous = this.ws
      if (previous) {
        try {
          previous.onopen = null
          previous.onmessage = null
          previous.onerror = null
          previous.onclose = null
          previous.close()
        } catch {
          /* ignore */
        }
      }

      const socket = new WebSocket(this.url)
      this.ws = socket

      socket.onopen = () => {
        if (!this.isCurrentSocket(socket, generation)) return
        this.reconnectAttempts = 0
        this.sendSubscribe()
      }

      socket.onmessage = (event) => {
        if (!this.isCurrentSocket(socket, generation)) return
        try {
          const msg: NekoMessage = JSON.parse(event.data)
          if (msg.type === 'subscribe_ack') {
            if (!this.isCurrentSubscriptionAck(msg, generation)) return
            if (!this.sessionReady) {
              this.pendingSubscription = null
              this.sessionReady = true
              this.setStatus('connected')
              const cbs = this.onReady.splice(0, this.onReady.length)
              cbs.forEach(cb => cb())
            }
            this.handlers.forEach(h => h(msg))
            return
          }
          if (msg.type === 'error') {
            const m = (msg.payload as { message?: string })?.message || ''
            if (m.includes('unauthorized')) {
              this.sessionReady = false
              this.pendingSubscription = null
              this.setStatus('auth_error')
              this.handlers.forEach(h => h(msg))
              return
            }
            // Surface handshake errors without treating them as a successful subscribe.
            if (!this.sessionReady) {
              this.pendingSubscription = null
              this.setStatus('disconnected')
              this.handlers.forEach(h => h(msg))
              return
            }
          }
          // Snapshots and delayed frames cannot complete the subscribe handshake.
          if (!this.sessionReady) {
            return
          }
          this.handlers.forEach(h => h(msg))
        } catch (err) {
          console.error('[ws] parse error:', err)
        }
      }

      socket.onclose = () => {
        if (!this.isCurrentSocket(socket, generation)) return
        this.ws = null
        this.sessionReady = false
        if (this.status !== 'auth_error') {
          this.setStatus('disconnected')
        }
        if (!this.intentionalClose) this.scheduleReconnect()
      }

      socket.onerror = () => {
        if (!this.isCurrentSocket(socket, generation)) return
        this.sessionReady = false
        if (this.status !== 'auth_error') {
          this.setStatus('disconnected')
        }
      }
    } catch (err) {
      if (generation === this.generation) {
        this.ws = null
      }
      console.error('[ws] connect error:', err)
      this.scheduleReconnect()
    }
  }

  /** Called once when the latest subscribe request is explicitly acknowledged. */
  whenReady(cb: () => void) {
    if (this.sessionReady) cb()
    else this.onReady.push(cb)
  }

  subscribe(deviceId: string) {
    const changed = this.subscribedDevice !== deviceId
    this.subscribedDevice = deviceId

    if (this.ws?.readyState === WebSocket.OPEN) {
      if (changed || !this.sessionReady) {
        this.sessionReady = false
        this.pendingSubscription = null
        this.setStatus('connecting')
        this.sendSubscribe()
      }
    } else if (this.ws?.readyState !== WebSocket.CONNECTING) {
      this.connect()
    }
  }

  private sendSubscribe() {
    if (!this.subscribedDevice) return
    const subscriptionId = [
      Date.now().toString(36),
      this.generation.toString(36),
      (++this.subscriptionSequence).toString(36),
      Math.random().toString(36).slice(2, 10)
    ].join('_')
    this.pendingSubscription = {
      id: subscriptionId,
      deviceId: this.subscribedDevice,
      socketGeneration: this.generation
    }
    const secret = getPhoneSecret()
    this.trySend({
      type: 'subscribe',
      device_id: this.subscribedDevice,
      timestamp: Math.floor(Date.now() / 1000),
      payload: {
        ...(secret ? { secret } : {}),
        subscription_id: subscriptionId
      }
    })
  }

  private isCurrentSubscriptionAck(msg: NekoMessage, socketGeneration: number): boolean {
    const pending = this.pendingSubscription
    const subscriptionId = (msg.payload?.subscription_id as string | undefined)?.trim()
    return !!(
      pending &&
      subscriptionId &&
      socketGeneration === this.generation &&
      pending.socketGeneration === socketGeneration &&
      pending.id === subscriptionId &&
      pending.deviceId === this.subscribedDevice &&
      msg.device_id === pending.deviceId
    )
  }

  /**
   * Attempt to send. Returns true only if socket accepted the frame.
   * Does NOT mean server processed the message — wait for prompt_sent etc.
   * Never auto-retries (caller owns outbox + same client_msg_id).
   */
  send(msg: Partial<NekoMessage>): boolean {
    if (!this.subscribedDevice && !msg.device_id) {
      console.warn('[ws] send dropped: no subscribed device')
      return false
    }
    if (!msg.device_id && this.subscribedDevice) {
      msg.device_id = this.subscribedDevice
    }
    if (!msg.timestamp) {
      msg.timestamp = Math.floor(Date.now() / 1000)
    }

    if (this.ws?.readyState !== WebSocket.OPEN || !this.sessionReady) {
      if (this.subscribedDevice && this.ws?.readyState !== WebSocket.CONNECTING) {
        this.connect()
      }
      return false
    }
    if (msg.device_id && this.subscribedDevice && msg.device_id !== this.subscribedDevice) {
      return false
    }
    return this.trySend(msg)
  }

  private trySend(msg: Partial<NekoMessage>): boolean {
    try {
      if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return false
      this.ws.send(JSON.stringify(msg))
      return true
    } catch (err) {
      console.error('[ws] send error:', err)
      return false
    }
  }

  addHandler(id: string, handler: MessageHandler) {
    this.handlers.set(id, handler)
  }

  removeHandler(id: string) {
    this.handlers.delete(id)
  }

  onStatusChange(id: string, handler: StatusHandler) {
    this.statusHandlers.set(id, handler)
    handler(this.status)
  }

  removeStatusHandler(id: string) {
    this.statusHandlers.delete(id)
  }

  getSubscribedDevice(): string | null {
    return this.subscribedDevice
  }

  getStatus() {
    return this.status
  }

  isConnected(): boolean {
    return this.ws?.readyState === WebSocket.OPEN && this.sessionReady
  }

  disconnect() {
    this.intentionalClose = true
    this.generation++
    if (this.reconnectTimer !== null) {
      window.clearTimeout(this.reconnectTimer)
      this.reconnectTimer = null
    }
    this.sessionReady = false
    this.pendingSubscription = null
    this.onReady = []
    const socket = this.ws
    this.ws = null
    if (socket) {
      socket.onopen = null
      socket.onmessage = null
      socket.onerror = null
      socket.onclose = null
      socket.close()
    }
    this.setStatus('disconnected')
  }

  private isCurrentSocket(socket: WebSocket, generation: number): boolean {
    return this.ws === socket && this.generation === generation
  }

  private setStatus(status: 'connecting' | 'connected' | 'disconnected' | 'auth_error') {
    this.status = status
    this.statusHandlers.forEach(h => h(status))
  }

  private scheduleReconnect() {
    if (this.reconnectTimer || this.intentionalClose) return
    if (!this.subscribedDevice) return
    if (this.status === 'auth_error') return
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
  }
  return instance
}
