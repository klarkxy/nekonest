import { defineStore } from 'pinia'
import type { AttachmentRef } from '@/utils/attachments'

const DRAFTS_KEY = 'nekonest_input_drafts'
const MAX_DRAFTS = 80

export type InputDraft = {
  text: string
  attachments: Array<{
    id: string
    url: string
    name: string
    mime: string
    size: number
    key?: string
  }>
  updatedAt: number
}

type DraftMap = Record<string, InputDraft>

function draftKey(deviceId: string, sessionId: string) {
  return `${deviceId}::${sessionId}`
}

function loadAll(): DraftMap {
  try {
    const raw = localStorage.getItem(DRAFTS_KEY)
    if (!raw) return {}
    const obj = JSON.parse(raw) as DraftMap
    return obj && typeof obj === 'object' ? obj : {}
  } catch {
    return {}
  }
}

function saveAll(map: DraftMap) {
  // prune oldest if too many
  const entries = Object.entries(map)
  if (entries.length > MAX_DRAFTS) {
    entries.sort((a, b) => (a[1].updatedAt || 0) - (b[1].updatedAt || 0))
    const drop = entries.length - MAX_DRAFTS
    for (let i = 0; i < drop; i++) {
      delete map[entries[i][0]]
    }
  }
  try {
    localStorage.setItem(DRAFTS_KEY, JSON.stringify(map))
  } catch {
    // quota — drop half
    const entries2 = Object.entries(map).sort((a, b) => (a[1].updatedAt || 0) - (b[1].updatedAt || 0))
    const half = Math.floor(entries2.length / 2)
    const next: DraftMap = {}
    for (let i = half; i < entries2.length; i++) {
      next[entries2[i][0]] = entries2[i][1]
    }
    try {
      localStorage.setItem(DRAFTS_KEY, JSON.stringify(next))
    } catch { /* give up */ }
  }
}

function toStorable(atts: AttachmentRef[]): InputDraft['attachments'] {
  return atts
    .filter(a => a.url && a.id)
    .map(a => ({
      id: a.id,
      url: a.url,
      name: a.name,
      mime: a.mime,
      size: a.size,
      key: a.key
    }))
}

/** Per-session input drafts (text + uploaded attachment refs). Local only. */
export const useDraftStore = defineStore('drafts', () => {
  function get(deviceId: string, sessionId: string): InputDraft | null {
    if (!deviceId || !sessionId) return null
    const d = loadAll()[draftKey(deviceId, sessionId)]
    if (!d) return null
    return {
      text: d.text || '',
      attachments: Array.isArray(d.attachments) ? d.attachments : [],
      updatedAt: d.updatedAt || 0
    }
  }

  function set(
    deviceId: string,
    sessionId: string,
    text: string,
    attachments: AttachmentRef[] = []
  ) {
    if (!deviceId || !sessionId) return
    const key = draftKey(deviceId, sessionId)
    const map = loadAll()
    const trimmed = text // keep spaces user is typing
    const atts = toStorable(attachments)
    if (!trimmed && atts.length === 0) {
      if (map[key]) {
        delete map[key]
        saveAll(map)
      }
      return
    }
    map[key] = {
      text: trimmed,
      attachments: atts,
      updatedAt: Date.now()
    }
    saveAll(map)
  }

  function clear(deviceId: string, sessionId: string) {
    if (!deviceId || !sessionId) return
    const map = loadAll()
    const key = draftKey(deviceId, sessionId)
    if (map[key]) {
      delete map[key]
      saveAll(map)
    }
  }

  return { get, set, clear }
})
