/** Phone-visible thread count for a device page. Never fall back to `active_agents`. */
export function deviceThreadStatCount(remoteSessions: number, localDrafts: number): number {
  const remote = Number.isFinite(remoteSessions) ? Math.max(0, remoteSessions) : 0
  const local = Number.isFinite(localDrafts) ? Math.max(0, localDrafts) : 0
  return remote + local
}
