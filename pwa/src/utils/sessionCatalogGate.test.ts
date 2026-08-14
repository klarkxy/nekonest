import { describe, expect, it } from 'vitest'
import { sessionCatalogGate, sessionInDeviceCatalog } from './sessionCatalogGate'

describe('sessionCatalogGate', () => {
  it('never blocks a local draft behind catalog loading', () => {
    expect(sessionCatalogGate({
      isLocalDraft: true,
      catalogReady: false,
      targetInCatalog: false
    })).toEqual({ catalogLoading: false, hiddenOrRemoved: false })
  })

  it('opens a native thread already present in the in-memory list without waiting for WebSocket ready', () => {
    expect(sessionCatalogGate({
      isLocalDraft: false,
      catalogReady: false,
      targetInCatalog: true
    })).toEqual({ catalogLoading: false, hiddenOrRemoved: false })
  })

  it('keeps the loading wall when the target is missing and the catalog is not ready', () => {
    expect(sessionCatalogGate({
      isLocalDraft: false,
      catalogReady: false,
      targetInCatalog: false
    })).toEqual({ catalogLoading: true, hiddenOrRemoved: false })
  })

  it('reports hidden only after a ready catalog omits the target', () => {
    expect(sessionCatalogGate({
      isLocalDraft: false,
      catalogReady: true,
      targetInCatalog: false
    })).toEqual({ catalogLoading: false, hiddenOrRemoved: true })
  })
})

describe('sessionInDeviceCatalog', () => {
  it('matches a row for this device and ignores a row owned by another host', () => {
    expect(sessionInDeviceCatalog(
      [{ id: 's1', device_id: 'dev-a' }, { id: 's2', device_id: 'dev-b' }],
      's1',
      'dev-a'
    )).toBe(true)
    expect(sessionInDeviceCatalog(
      [{ id: 's1', device_id: 'dev-b' }],
      's1',
      'dev-a'
    )).toBe(false)
    expect(sessionInDeviceCatalog(
      [{ id: 's1' }],
      's1',
      'dev-a'
    )).toBe(true)
  })
})
