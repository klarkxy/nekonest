import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { APP_VERSION } from '@/config/version'
import { NekoWebSocket } from './websocket'
import { resetTransportModeForTests } from './transport'

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
    subscriptionId: frame.payload.subscription_id as string,
    pwaVersion: frame.payload.pwa_version as string
    , transportMode: frame.transport_mode as string,
    refreshSessions: frame.payload.refresh_sessions as boolean
  }
}

function acknowledge(socket: FakeWebSocket, deviceId: string, subscriptionId: string, protocolVersion = '1.2') {
  socket.message({
    protocol_version: protocolVersion,
    type: 'subscribe_ack',
    device_id: deviceId,
    timestamp: 1,
    payload: { subscription_id: subscriptionId, protocol_version: protocolVersion }
  })
}

describe('NekoWebSocket lifecycle', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    FakeWebSocket.instances = []
    vi.stubGlobal('WebSocket', FakeWebSocket)
    resetTransportModeForTests('open')
    localStorage.clear()
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
    resetTransportModeForTests()
  })

  it('cancels a scheduled reconnect when a manual connect succeeds first', () => {
    const client = new NekoWebSocket()
    client.subscribe('device-a')
    const first = FakeWebSocket.instances[0]
    first.open()
    const subscription = latestSubscription(first)
    expect(subscription.pwaVersion).toBe(APP_VERSION)
    expect(subscription.transportMode).toBe('open')
    expect(subscription.refreshSessions).toBe(true)
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

  it('does not construct a socket until health provides a valid transport mode', async () => {
    resetTransportModeForTests()
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ transport_mode: 'sealed' }), { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)
    const client = new NekoWebSocket()

    client.subscribe('device-a')
    expect(FakeWebSocket.instances).toHaveLength(0)
    await vi.runAllTimersAsync()
    await Promise.resolve()

    expect(fetchMock).toHaveBeenCalledWith('/health', { cache: 'no-store' })
    expect(FakeWebSocket.instances).toHaveLength(1)
    const socket = FakeWebSocket.instances[0]
    socket.open()
    expect(latestSubscription(socket).transportMode).toBe('sealed')
  })

  it('blocks websocket construction when health fails', async () => {
    resetTransportModeForTests()
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('', { status: 503 })))
    const client = new NekoWebSocket()
    client.subscribe('device-a')
    expect(FakeWebSocket.instances).toHaveLength(0)
    await vi.waitFor(() => expect(client.getStatus()).toBe('transport_error'))
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

  it('does not mint a second subscribe while the same device handshake is still pending', () => {
    const client = new NekoWebSocket()
    client.subscribe('device-a')
    const socket = FakeWebSocket.instances[0]
    socket.open()
    expect(socket.sent).toHaveLength(1)
    client.subscribe('device-a')
    expect(socket.sent).toHaveLength(1)
    const first = latestSubscription(socket)
    acknowledge(socket, 'device-a', first.subscriptionId)
    expect(client.isConnected()).toBe(true)
    client.subscribe('device-a', { force: true })
    expect(socket.sent).toHaveLength(2)
    expect(client.isConnected()).toBe(false)
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

  it('uses the negotiated 1.1 version for subsequent frames', () => {
    const client = new NekoWebSocket()
    client.subscribe('device-a')
    const socket = FakeWebSocket.instances[0]
    socket.open()
    const subscription = latestSubscription(socket)
    acknowledge(socket, 'device-a', subscription.subscriptionId, '1.1')

    expect(client.getProtocolVersion()).toBe('1.1')
    expect(client.send({ type: 'heartbeat', device_id: 'device-a' })).toBe(true)
    const frame = JSON.parse(socket.sent[socket.sent.length - 1])
    expect(frame.protocol_version).toBe('1.1')
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

  it('honors bounded retry hints only for approved retryable service errors', () => {
    const client = new NekoWebSocket()
    client.subscribe('device-a')
    const socket = FakeWebSocket.instances[0]
    socket.open()
    socket.message({
      type: 'error',
      device_id: 'device-a',
      timestamp: 1,
      payload: {
        error_code: 'route_unavailable',
        message: '正在切换区域',
        retryable: true,
        retry_after_seconds: 7
      }
    })
    socket.serverClose()

    expect(client.getStatus()).toBe('disconnected')
    expect(client.getTransportError()).toBe('正在切换区域')
    vi.advanceTimersByTime(6999)
    expect(FakeWebSocket.instances).toHaveLength(1)
    vi.advanceTimersByTime(1)
    expect(FakeWebSocket.instances).toHaveLength(2)
  })

  it('fails closed on unknown structured errors and exposes only HTTPS action URLs', () => {
    const client = new NekoWebSocket()
    client.subscribe('device-a')
    const socket = FakeWebSocket.instances[0]
    socket.open()
    socket.message({
      type: 'error',
      device_id: 'device-a',
      timestamp: 1,
      payload: {
        error_code: 'future_policy',
        message: '请检查账户',
        retryable: true,
        action_url: 'https://cloud.example/account'
      }
    })
    socket.serverClose()

    expect(client.getStatus()).toBe('transport_error')
    expect(client.getTransportError()).toBe('请检查账户')
    expect(client.getServiceActionURL()).toBe('https://cloud.example/account')
    vi.advanceTimersByTime(60_000)
    expect(FakeWebSocket.instances).toHaveLength(1)
  })
})
