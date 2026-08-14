/** Session-page catalog overlay. WebSocket `ready` stays the authority for hide/remove. */

export function sessionCatalogGate(input: {
  isLocalDraft: boolean
  catalogReady: boolean
  targetInCatalog: boolean
}): { catalogLoading: boolean; hiddenOrRemoved: boolean } {
  if (input.isLocalDraft) {
    return { catalogLoading: false, hiddenOrRemoved: false }
  }
  return {
    catalogLoading: !input.catalogReady && !input.targetInCatalog,
    hiddenOrRemoved: input.catalogReady && !input.targetInCatalog
  }
}

export function sessionInDeviceCatalog(
  sessions: Array<{ id: string; device_id?: string }>,
  sessionId: string,
  deviceId: string
): boolean {
  return sessions.some(session =>
    session.id === sessionId && (!session.device_id || session.device_id === deviceId)
  )
}
