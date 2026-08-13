import { beforeEach, describe, expect, it, vi } from 'vitest'
import { exchangeHandoffFromFragment, exchangeHandoffTicket, takeHandoffTicketFromFragment } from './handoff'
import { getPhoneId, getPhoneToken, getRouteHandle } from './http'

vi.mock('@/crypto/identity', () => ({
  loadOrCreatePhoneIdentity: vi.fn(async () => ({
    ed25519_public: 'ed-public',
    x25519_public: 'x-public',
    fingerprint: 'fingerprint'
  }))
}))

describe('Cloud PWA handoff', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    history.replaceState({}, '', '/')
    vi.restoreAllMocks()
  })

  it('clears the fragment before exchanging and stores independent phone credentials', async () => {
    history.replaceState({}, '', '/#handoff=ticket_abcdefghijklmnopqrstuvwxyz')
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation(async (_url, init) => {
      expect(location.hash).toBe('')
      const submitted = JSON.parse(String(init?.body)) as { ticket: string }
      expect(submitted.ticket).toBe('ticket_abcdefghijklmnopqrstuvwxyz')
      return new Response(JSON.stringify({
        phone_id: 'phone-cloud',
        phone_token: 'phone-token',
        route_handle: 'route-handle'
      }), { status: 200, headers: { 'Content-Type': 'application/json' } })
    })

    await expect(exchangeHandoffFromFragment()).resolves.toMatchObject({ state: 'exchanged' })
    expect(fetchMock).toHaveBeenCalledOnce()
    expect(getPhoneId()).toBe('phone-cloud')
    expect(getPhoneToken()).toBe('phone-token')
    expect(getRouteHandle()).toBe('route-handle')
  })

  it('does not persist or replay the single-use ticket after a failed exchange', async () => {
    history.replaceState({}, '', '/#handoff=ticket_abcdefghijklmnopqrstuvwxyz')
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({
      error_code: 'handoff_expired',
      message: 'The link expired.'
    }), { status: 410, headers: { 'Content-Type': 'application/json' } }))

    await expect(exchangeHandoffFromFragment()).rejects.toThrow('expired')
    expect(location.hash).toBe('')
    await expect(exchangeHandoffFromFragment()).resolves.toEqual({ state: 'none' })
  })

  it('can clear the fragment before runtime config is fetched', async () => {
    history.replaceState({}, '', '/#handoff=ticket_abcdefghijklmnopqrstuvwxyz')
    const ticket = takeHandoffTicketFromFragment()
    expect(ticket).toBe('ticket_abcdefghijklmnopqrstuvwxyz')
    expect(location.hash).toBe('')

    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({
      phone_id: 'phone-cloud',
      phone_token: 'phone-token',
      route_handle: 'route-handle'
    }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
    await expect(exchangeHandoffTicket(ticket)).resolves.toMatchObject({ state: 'exchanged' })
  })

  it('retries the same in-memory ticket when the committed response is lost', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockRejectedValueOnce(new TypeError('response lost'))
      .mockResolvedValueOnce(new Response(JSON.stringify({
        phone_id: 'phone-stable',
        phone_token: 'phone-token-stable',
        route_handle: 'route-handle-stable'
      }), { status: 200, headers: { 'Content-Type': 'application/json' } }))

    await expect(exchangeHandoffTicket('ticket_abcdefghijklmnopqrstuvwxyz')).resolves.toEqual({
      state: 'exchanged',
      phoneId: 'phone-stable'
    })
    expect(fetchMock).toHaveBeenCalledTimes(2)
    const first = JSON.parse(String(fetchMock.mock.calls[0]?.[1]?.body)) as { ticket: string }
    const second = JSON.parse(String(fetchMock.mock.calls[1]?.[1]?.body)) as { ticket: string }
    expect(first.ticket).toBe('ticket_abcdefghijklmnopqrstuvwxyz')
    expect(second.ticket).toBe(first.ticket)
  })
})
