import { beforeEach, describe, expect, it } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useSessionPrefsStore } from './sessionPrefs'

describe('session prefs bulk archive', () => {
  beforeEach(() => {
    localStorage.clear()
    setActivePinia(createPinia())
  })

  it('archives and restores a project session set in one persisted update', () => {
    const prefs = useSessionPrefsStore()

    prefs.archive('already-archived')
    prefs.setArchived(['project-a', 'project-b'], true)

    expect(prefs.isArchived('already-archived')).toBe(true)
    expect(prefs.isArchived('project-a')).toBe(true)
    expect(prefs.isArchived('project-b')).toBe(true)

    prefs.setArchived(['project-a', 'project-b'], false)

    expect(prefs.isArchived('already-archived')).toBe(true)
    expect(prefs.isArchived('project-a')).toBe(false)
    expect(prefs.isArchived('project-b')).toBe(false)
    expect(JSON.parse(localStorage.getItem('nekonest_archived_sessions') || '[]')).toEqual([
      'already-archived'
    ])
  })
})
