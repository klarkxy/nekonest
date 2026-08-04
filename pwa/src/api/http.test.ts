import { beforeEach, describe, expect, it, vi } from 'vitest'
import { apiFetch } from './http'

describe('apiFetch', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.restoreAllMocks()
  })

  it('always bypasses the browser HTTP cache for dynamic API responses', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response('{}'))

    await apiFetch('/api/devices', { cache: 'force-cache' })

    expect(fetchMock).toHaveBeenCalledWith('/api/devices', expect.objectContaining({
      cache: 'no-store'
    }))
  })
})
