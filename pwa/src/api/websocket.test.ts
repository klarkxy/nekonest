import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { NekoWebSocket } from './websocket'

type SocketHandler<T = Event> = ((event: T) => void) | null

class FakeWebSocket {
  static readonly CONNECTING = 0
  static readonly OPEN = 1
  static readonly CLOSING = 2
  static readonly CLOSED = 3

  static instances: FakeWebSocket[] = []

  readonly url: string
  readyState = FakeWebSocket.CONNECTING
  onopen: SocketHandler = null
  onmessage: SocketHandler<MessageEvent> = null
  onerror: SocketHandler = null
  onclose: SocketHandler<CloseEvent> = null
  sent: string[] = []

  constructor(url: string) {
    this.url = url
    FakeWebSocket.instances.push(this)
  }

  send(data: string) {
    if (this.readyState !== FakeWebSocket.OPEN) throw new Error('socket not open')
    this.sent.push(data)
  }

  close() {
    this.readyState = FakeWebSocket.CLOSED
  }

  open() {
    this.readyState = FakeWebSocket.OPEN
    this.onopen?.(new Event('open'))
  }

  message(data: unknown) {
    this.onmessage?.(new MessageEvent('message', { data: JSON.stringify(data) }))
  }

  serverClose() {
    this.readyState = FakeWebSocket.CLOSED
    this.onclose?.(new CloseEvent('close'))
  }
}

function latestSubscription(socket: FakeWebSocket) {
  const frame = JSON.parse(socket.sent[socket.sent.length - 1])
  return {
    deviceId: frame.device_id as string,
    subscriptionId: frame.payload.subscription_id as string
  }
}

function acknowledge(socket: FakeWebSocket, deviceId: string, subscriptionId: string) {
  socket.message({
    type: 'subscribe_ack',
    device_id: deviceId,
    timestamp: 1,
    payload: { subscription_id: subscriptionId }
  })
}

describe('NekoWebSocket lifecycle', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    FakeWebSocket.instances = []
    vi.stubGlobal('WebSocket', FakeWebSocket)
    localStorage.clear()
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('cancels a scheduled reconnect when a manual connect succeeds first', () => {
    const client = new NekoWebSocket()
    client.subscribe('device-a')
    const first = FakeWebSocket.instances[0]
    first.open()
    const subscription = latestSubscription(first)
    first.message({ type: 'session_list', device_id: 'device-a', timestamp: 1, payload: {} })
    expect(client.isConnected()).toBe(false)
    acknowledge(first, subscription.deviceId, subscription.subscriptionId)
    expect(client.isConnected()).toBe(true)

    first.serverClose()
    expect(client.getStatus()).toBe('disconnected')

    client.connect()
    expect(FakeWebSocket.instances).toHaveLength(2)
    vi.advanceTimersByTime(5000)
    expect(FakeWebSocket.instances).toHaveLength(2)
  })

  it('ignores events retained from a replaced socket', () => {
    const client = new NekoWebSocket()
    client.subscribe('device-a')
    const first = FakeWebSocket.instances[0]
    first.open()
    const subscription = latestSubscription(first)
    const staleMessage = first.onmessage
    const staleClose = first.onclose

    client.connect()
    expect(client.getStatus()).toBe('connecting')

    staleMessage?.(new MessageEvent('message', {
      data: JSON.stringify({
        type: 'subscribe_ack',
        device_id: 'device-a',
        timestamp: 1,
        payload: { subscription_id: subscription.subscriptionId }
      })
    }))
    staleClose?.(new CloseEvent('close'))
    expect(client.getStatus()).toBe('connecting')

    vi.advanceTimersByTime(5000)
    expect(FakeWebSocket.instances).toHaveLength(2)
  })

  it('accepts only the latest device and subscription id during a switch', () => {
    const client = new NekoWebSocket()
    client.subscribe('device-a')
    const socket = FakeWebSocket.instances[0]
    socket.open()
    const first = latestSubscription(socket)

    client.subscribe('device-b')
    const second = latestSubscription(socket)
    expect(second.subscriptionId).not.toBe(first.subscriptionId)
    expect(client.getStatus()).toBe('connecting')

    acknowledge(socket, 'device-a', first.subscriptionId)
    expect(client.isConnected()).toBe(false)

    // A current nonce attached to the wrong device is also rejected.
    acknowledge(socket, 'device-a', second.subscriptionId)
    expect(client.isConnected()).toBe(false)

    socket.message({
      type: 'session_list',
      device_id: 'device-b',
      timestamp: 2,
      payload: { sessions: [] }
    })
    expect(client.isConnected()).toBe(false)

    acknowledge(socket, 'device-b', second.subscriptionId)
    expect(client.isConnected()).toBe(true)
  })

  it('retries a rejected handshake when subscribing to the same device', () => {
    const client = new NekoWebSocket()
    client.subscribe('device-a')
    const socket = FakeWebSocket.instances[0]
    socket.open()
    const first = latestSubscription(socket)
    socket.message({
      type: 'error',
      device_id: 'device-a',
      timestamp: 1,
      payload: { message: 'temporary subscribe failure' }
    })
    expect(client.isConnected()).toBe(false)
    expect(client.getStatus()).toBe('disconnected')
    vi.advanceTimersByTime(5000)
    expect(FakeWebSocket.instances).toHaveLength(1)

    client.subscribe('device-a')
    const second = latestSubscription(socket)
    expect(second.subscriptionId).not.toBe(first.subscriptionId)

    acknowledge(socket, 'device-a', first.subscriptionId)
    expect(client.isConnected()).toBe(false)
    acknowledge(socket, 'device-a', second.subscriptionId)
    expect(client.isConnected()).toBe(true)
  })
})
