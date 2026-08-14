import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  acknowledgeOpenTransport,
  ensureTransportMode,
  openTransportConsentRequired,
  resetTransportModeForTests,
  runtimeTransportMode,
  transportModeError,
  transportModeKind
} from './transport'
import { setRuntimeConfigForTests } from '@/config/runtimeEndpoint'

describe('runtime transport mode', () => {
  beforeEach(() => {
    localStorage.clear()
    setRuntimeConfigForTests(undefined)
    resetTransportModeForTests()
    vi.stubGlobal('fetch', vi.fn())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    setRuntimeConfigForTests(undefined)
    resetTransportModeForTests()
  })

  it('reads sealed mode from health and caches it', async () => {
    const fetchMock = vi.mocked(fetch)
    fetchMock.mockResolvedValue(new Response(JSON.stringify({ transport_mode: 'sealed' }), { status: 200 }))

    await expect(ensureTransportMode()).resolves.toBe('sealed')
    await expect(ensureTransportMode()).resolves.toBe('sealed')
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(runtimeTransportMode()).toBe('sealed')
  })

  it('fails closed for unavailable or invalid health data', async () => {
    const fetchMock = vi.mocked(fetch)
    fetchMock.mockResolvedValue(new Response('', { status: 503 }))
    await expect(ensureTransportMode()).rejects.toThrow('HTTP 503')
    expect(runtimeTransportMode()).toBeNull()

    fetchMock.mockResolvedValue(new Response(JSON.stringify({ transport_mode: 'unknown' }), { status: 200 }))
    await expect(ensureTransportMode()).rejects.toThrow('invalid transport mode')
    expect(transportModeError()).toContain('invalid transport mode')
  })

  it('requires explicit consent before first open-relay connection', async () => {
    vi.mocked(fetch).mockImplementation(async () => new Response(JSON.stringify({ transport_mode: 'open' }), { status: 200 }))

    await expect(ensureTransportMode()).rejects.toThrow('Confirm that prompts and responses')
    expect(transportModeKind()).toBe('consent_required')
    expect(openTransportConsentRequired()).toBe(true)
    expect(runtimeTransportMode()).toBeNull()

    acknowledgeOpenTransport()
    await expect(ensureTransportMode()).resolves.toBe('open')
  })

  it('blocks an origin pinned to sealed from downgrading to open', async () => {
    const fetchMock = vi.mocked(fetch)
    fetchMock.mockResolvedValueOnce(new Response(JSON.stringify({ transport_mode: 'sealed' }), { status: 200 }))
    await expect(ensureTransportMode()).resolves.toBe('sealed')

    acknowledgeOpenTransport()
    fetchMock.mockResolvedValueOnce(new Response(JSON.stringify({ transport_mode: 'open' }), { status: 200 }))
    await expect(ensureTransportMode(true)).rejects.toThrow('downgrade blocked')
    expect(runtimeTransportMode()).toBeNull()
  })

  it('rejects open transport unconditionally for a managed Cloud build', async () => {
    setRuntimeConfigForTests({ api_base: 'https://connect.example.cn', managed: true })
    vi.mocked(fetch).mockResolvedValue(new Response(JSON.stringify({ transport_mode: 'open' }), { status: 200 }))

    await expect(ensureTransportMode()).rejects.toThrow('requires sealed transport')
    expect(openTransportConsentRequired()).toBe(false)
    expect(runtimeTransportMode()).toBeNull()
  })
})
