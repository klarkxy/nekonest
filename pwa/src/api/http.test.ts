import { beforeEach, describe, expect, it, vi } from 'vitest'
import { apiFetch, completeSetupWithSecret, getPhoneSecret, getPhoneToken, probePhoneCredential, setPhoneSecret } from './http'
import { setRuntimeConfigForTests } from '@/config/runtimeEndpoint'

describe('apiFetch', () => {
  beforeEach(() => {
    localStorage.clear()
    setRuntimeConfigForTests(undefined)
    vi.restoreAllMocks()
  })

  it('always bypasses the browser HTTP cache for dynamic API responses', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response('{}'))

    await apiFetch('/api/devices', { cache: 'force-cache' })

    expect(fetchMock).toHaveBeenCalledWith('/api/devices', expect.objectContaining({
      cache: 'no-store'
    }))
  })

  it('does not import an unscoped legacy credential into managed Cloud', () => {
    localStorage.setItem('nekonest_phone_token', 'old-other-nest-token')
    setRuntimeConfigForTests({ api_base: 'https://connect.example.cn', managed: true })

    expect(getPhoneToken()).toBe('')
  })

  it('probes a candidate secret against GET /api/devices without persisting it', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response('{"devices":[]}', { status: 401 }))

    const res = await probePhoneCredential('candidate-secret')

    expect(res.status).toBe(401)
    expect(getPhoneSecret()).toBe('')
    expect(fetchMock).toHaveBeenCalledWith('/api/devices', expect.objectContaining({
      cache: 'no-store',
      method: 'GET'
    }))
    const headers = fetchMock.mock.calls[0][1]?.headers as Headers
    expect(headers.get('Authorization')).toBe('Bearer candidate-secret')
    expect(headers.get('X-Neko-Secret')).toBe('candidate-secret')
  })

  it('keeps a stored secret when a later probe fails', async () => {
    setPhoneSecret('good-secret')
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response('', { status: 401 }))
    await probePhoneCredential('wrong-secret')
    expect(getPhoneSecret()).toBe('good-secret')
  })

  it('persists a phone secret only after GET /api/devices returns 200', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch')
    fetchMock.mockResolvedValueOnce(new Response('', { status: 401 }))
    await expect(completeSetupWithSecret('wrong')).resolves.toEqual({ ok: false, reason: 'auth', status: 401 })
    expect(getPhoneSecret()).toBe('')
    expect(localStorage.getItem('nekonest_setup_done')).toBeNull()

    fetchMock.mockResolvedValueOnce(new Response('{"devices":[]}', { status: 200 }))
    await expect(completeSetupWithSecret('good')).resolves.toEqual({ ok: true })
    expect(getPhoneSecret()).toBe('good')
    expect(localStorage.getItem('nekonest_setup_done')).toBe('1')
  })
})
