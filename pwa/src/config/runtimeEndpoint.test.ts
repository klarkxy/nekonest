import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  apiURL,
  attachmentURL,
  endpointOrigin,
  handoffExchangePath,
  loadRuntimeConfig,
  pushURL,
  setRuntimeConfigForTests,
  websocketURL
} from './runtimeEndpoint'

afterEach(() => setRuntimeConfigForTests(undefined))

describe('runtime endpoint', () => {
  it('keeps self-hosted API calls relative and WebSocket calls on the page origin', () => {
    expect(apiURL('/api/devices')).toBe('/api/devices')
    expect(endpointOrigin()).toBe(location.origin)
    expect(websocketURL()).toBe(`${location.protocol === 'https:' ? 'wss:' : 'ws:'}//${location.host}/ws/phone`)
  })

  it('routes every managed data-plane request to the configured stable origin', () => {
    setRuntimeConfigForTests({ api_base: 'https://connect.example.cn', managed: true })
    expect(apiURL('/health')).toBe('https://connect.example.cn/health')
    expect(endpointOrigin()).toBe('https://connect.example.cn')
    expect(websocketURL()).toBe('wss://connect.example.cn/ws/phone')
  })

  it('uses explicit attachment and push origins without weakening origin checks', () => {
    setRuntimeConfigForTests({
      api_base: 'https://connect.example.cn',
      attachment_base: 'https://media.example.cn',
      push_base: 'https://push.example.cn'
    })
    expect(attachmentURL('/api/attachments/a?k=b')).toBe('https://media.example.cn/api/attachments/a?k=b')
    expect(pushURL('/api/push/subscribe')).toBe('https://push.example.cn/api/push/subscribe')
    expect(() => attachmentURL('https://connect.example.cn/api/attachments/a')).toThrow('attachment URL escaped')
    expect(() => pushURL('https://connect.example.cn/api/push/subscribe')).toThrow('push URL escaped')
  })

  it('rejects endpoint bases that contain credentials or paths', () => {
    setRuntimeConfigForTests({ api_base: 'https://user@example.cn/path' })
    expect(() => endpointOrigin()).toThrow('Invalid NekoNest API base')
  })

  it('refuses an API or attachment URL that exposes another backend origin', () => {
    setRuntimeConfigForTests({ api_base: 'https://connect.example.cn', managed: true })
    expect(() => apiURL('https://tenant-7.internal.example/api/attachments/a')).toThrow('escaped')
    expect(apiURL('https://connect.example.cn/api/attachments/a')).toBe('https://connect.example.cn/api/attachments/a')
  })

  it('loads strict deploy-time config without service-worker caching', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({
      api_base: 'https://connect.example.cn',
      managed: true
    }), { headers: { 'content-type': 'application/json' } }))

    await expect(loadRuntimeConfig()).resolves.toMatchObject({ managed: true })
    expect(fetchMock).toHaveBeenCalledWith('/runtime-config.json', { cache: 'no-store', credentials: 'omit' })
    expect(endpointOrigin()).toBe('https://connect.example.cn')
    fetchMock.mockRestore()
  })

  it('rejects protocol-relative and backslash URLs that would escape the service origin', () => {
    setRuntimeConfigForTests({
      api_base: 'https://connect.example.cn',
      attachment_base: 'https://media.example.cn',
      push_base: 'https://push.example.cn',
      managed: true
    })
    expect(() => apiURL('//evil.test/x')).toThrow('escaped')
    expect(() => attachmentURL('//evil.test/x')).toThrow('escaped')
    expect(() => pushURL('https:\\evil.test/x')).toThrow('escaped')
    expect(() => websocketURL('//evil.test/ws')).toThrow('Invalid NekoNest WebSocket path')
  })

  it('rejects a handoff path that is not a same-origin absolute path', () => {
    setRuntimeConfigForTests({
      api_base: 'https://connect.example.cn',
      handoff_exchange_path: '//evil.test/handoff'
    })
    expect(() => handoffExchangePath()).toThrow('Invalid NekoNest handoff path')
    setRuntimeConfigForTests({
      api_base: 'https://connect.example.cn',
      handoff_exchange_path: '/api/pwa/handoff/exchange'
    })
    expect(handoffExchangePath()).toBe('/api/pwa/handoff/exchange')
  })

  it('fails open for an invalid self-hosted runtime config', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({
      api_base: 'https://connect.example.cn',
      unexpected: true
    }), { headers: { 'content-type': 'application/json' } }))

    await expect(loadRuntimeConfig()).resolves.toEqual({})
    expect(endpointOrigin()).toBe(location.origin)
    fetchMock.mockRestore()
  })

  it('derives WebSocket from deploy-time api_base instead of a baked WS origin', () => {
    setRuntimeConfigForTests({ api_base: 'https://connect.example.cn', managed: true })
    expect(websocketURL()).toBe('wss://connect.example.cn/ws/phone')
  })

  it('fails closed for a managed config without a stable API origin', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({
      managed: true
    }), { headers: { 'content-type': 'application/json' } }))

    await expect(loadRuntimeConfig()).rejects.toThrow('requires api_base')
    fetchMock.mockRestore()
  })
})
