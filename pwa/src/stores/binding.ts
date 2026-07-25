import { ref, computed } from 'vue'
import { defineStore } from 'pinia'

const BOUND_KEY = 'nekonest_bound_devices'
const LAST_KEY = 'nekonest_last_device'

export interface BoundDevice {
  id: string
  name: string
  bound_at: number
}

export const useBindingStore = defineStore('binding', () => {
  const bound = ref<BoundDevice[]>(loadBound())
  const lastDeviceId = ref<string | null>(localStorage.getItem(LAST_KEY))

  const boundIds = computed(() => new Set(bound.value.map(d => d.id)))

  function loadBound(): BoundDevice[] {
    try {
      const raw = localStorage.getItem(BOUND_KEY)
      if (!raw) return []
      return JSON.parse(raw) as BoundDevice[]
    } catch {
      return []
    }
  }

  function persist() {
    localStorage.setItem(BOUND_KEY, JSON.stringify(bound.value))
  }

  function addBinding(id: string, name: string) {
    const existing = bound.value.find(d => d.id === id)
    if (existing) {
      existing.name = name || existing.name
    } else {
      bound.value.push({ id, name: name || id, bound_at: Date.now() })
    }
    lastDeviceId.value = id
    localStorage.setItem(LAST_KEY, id)
    persist()
  }

  function removeBinding(id: string) {
    bound.value = bound.value.filter(d => d.id !== id)
    if (lastDeviceId.value === id) {
      lastDeviceId.value = bound.value[0]?.id ?? null
      if (lastDeviceId.value) localStorage.setItem(LAST_KEY, lastDeviceId.value)
      else localStorage.removeItem(LAST_KEY)
    }
    persist()
  }

  function setLastDevice(id: string) {
    lastDeviceId.value = id
    localStorage.setItem(LAST_KEY, id)
  }

  return { bound, boundIds, lastDeviceId, addBinding, removeBinding, setLastDevice }
})
