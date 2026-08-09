import { beforeEach, describe, expect, it } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useLocalThreadsStore } from './localThreads'
import { bindStartOperationIfAllowed } from '@/utils/startCapabilities'

describe('local thread drafts', () => {
  beforeEach(() => {
    localStorage.clear()
    setActivePinia(createPinia())
  })

  it('keeps the selected agent type on a phone-local draft', () => {
    const store = useLocalThreadsStore()
    const draft = store.createDraft('device-a', 'kimi_cli', 'D:\\repo', 'repo')

    expect(draft.agentType).toBe('kimi_cli')
    expect(store.asSessions('device-a')).toEqual([
      expect.objectContaining({
        id: draft.id,
        agent_type: 'kimi_cli',
        project_dir: 'D:\\repo'
      })
    ])
  })

  it('loads legacy Codex-only drafts safely', () => {
    localStorage.setItem('nekonest_local_threads_v1', JSON.stringify([{
      id: 'local_draft_old',
      deviceId: 'device-a',
      agentType: 'codex',
      projectDir: 'D:\\repo',
      project: 'repo',
      summary: '',
      createdAt: 1,
      lastActivity: 1
    }]))
    setActivePinia(createPinia())

    expect(useLocalThreadsStore().get('local_draft_old')?.agentType).toBe('codex')
  })

  it('persists the exact start operation so reload cannot mint a duplicate', () => {
    const store = useLocalThreadsStore()
    const draft = store.createDraft('device-a', 'claude_code', 'D:\\repo', 'repo')
    expect(store.bindStartOperation(draft.id, 'local_start_bound_1')).toBe(true)

    setActivePinia(createPinia())
    const reloaded = useLocalThreadsStore()
    expect(reloaded.get(draft.id)?.startOperationId).toBe('local_start_bound_1')

    reloaded.clearStartOperation(draft.id)
    setActivePinia(createPinia())
    expect(useLocalThreadsStore().get(draft.id)?.startOperationId).toBeUndefined()
  })

  it('does not persist an operation when send-time project validation is stale', () => {
    const store = useLocalThreadsStore()
    const draft = store.createDraft('device-a', 'grok_build', 'D:/stale', 'stale')
    const result = bindStartOperationIfAllowed(
      draft.agentType,
      draft.projectDir,
      [{
        id: 'native-a', device_id: 'device-a', agent_type: 'codex', status: 'idle',
        summary: '', last_activity: 1, project_dir: 'D:/current'
      }],
      [{ agent_type: 'grok_build', available: true, spawn: true }],
      () => store.bindStartOperation(draft.id, 'must_not_persist')
    )

    expect(result).toBe('unavailable')
    setActivePinia(createPinia())
    expect(useLocalThreadsStore().get(draft.id)?.startOperationId).toBeUndefined()
  })
})
