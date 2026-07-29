import { beforeEach, describe, expect, it } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useBindingStore } from './binding'

describe('binding store', () => {
  beforeEach(() => {
    localStorage.clear()
    setActivePinia(createPinia())
  })

  it('remembers an explicitly empty binding list after the last device is removed', () => {
    const store = useBindingStore()
    expect(store.bindingConfigured).toBe(false)

    store.addBinding('device-a', 'A')
    store.removeBinding('device-a')

    expect(store.bound).toEqual([])
    expect(store.bindingConfigured).toBe(true)
    expect(localStorage.getItem('nekonest_bound_devices')).toBe('[]')

    setActivePinia(createPinia())
    const reloaded = useBindingStore()
    expect(reloaded.bound).toEqual([])
    expect(reloaded.bindingConfigured).toBe(true)
  })
})
