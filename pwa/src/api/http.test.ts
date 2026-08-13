import { beforeEach, describe, expect, it, vi } from 'vitest'
import { apiFetch, getPhoneToken } from './http'
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
})
