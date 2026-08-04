import { describe, expect, it } from 'vitest'
import { componentVersionStatus, daemonVersionStatus } from './componentVersions'

describe('componentVersionStatus', () => {
  it('reports a fully aligned release', () => {
    expect(componentVersionStatus({
      frontend: '0.2.0',
      server: '0.2.0'
    })).toEqual({
      refreshRequired: false,
      allKnown: true,
      aligned: true
    })
  })

  it('requests a refresh when the loaded frontend differs from the server', () => {
    expect(componentVersionStatus({
      frontend: '0.2.0',
      server: '0.3.0'
    }).refreshRequired).toBe(true)
  })

  it('keeps an unreported server unknown instead of calling it aligned', () => {
    expect(componentVersionStatus({
      frontend: '0.2.0',
      server: ''
    })).toEqual({
      refreshRequired: false,
      allKnown: false,
      aligned: false
    })
  })
})

describe('daemonVersionStatus', () => {
  it('reports a stale daemon independently for each device', () => {
    expect(daemonVersionStatus('0.3.0', '0.2.0')).toEqual({
      known: true,
      updateRequired: true,
      aligned: false
    })
  })

  it('keeps an unreported legacy daemon unknown', () => {
    expect(daemonVersionStatus('0.3.0', '')).toEqual({
      known: false,
      updateRequired: false,
      aligned: false
    })
  })
})
