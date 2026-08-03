import { describe, expect, it } from 'vitest'
import { componentVersionStatus } from './componentVersions'

describe('componentVersionStatus', () => {
  it('reports a fully aligned release', () => {
    expect(componentVersionStatus({
      frontend: '0.2.0',
      server: '0.2.0',
      daemon: '0.2.0'
    })).toEqual({
      refreshRequired: false,
      daemonUpdateRequired: false,
      allKnown: true,
      aligned: true
    })
  })

  it('requests a refresh when the loaded frontend differs from the server', () => {
    expect(componentVersionStatus({
      frontend: '0.2.0',
      server: '0.3.0',
      daemon: '0.3.0'
    }).refreshRequired).toBe(true)
  })

  it('reports an independently stale daemon', () => {
    const status = componentVersionStatus({
      frontend: '0.3.0',
      server: '0.3.0',
      daemon: '0.2.0'
    })
    expect(status.refreshRequired).toBe(false)
    expect(status.daemonUpdateRequired).toBe(true)
    expect(status.aligned).toBe(false)
  })

  it('keeps an unreported legacy daemon unknown instead of calling it aligned', () => {
    expect(componentVersionStatus({
      frontend: '0.2.0',
      server: '0.2.0',
      daemon: ''
    })).toEqual({
      refreshRequired: false,
      daemonUpdateRequired: false,
      allKnown: false,
      aligned: false
    })
  })
})
