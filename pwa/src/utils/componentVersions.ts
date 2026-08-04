export interface ComponentVersions {
  frontend: string
  server: string
}

export interface ComponentVersionStatus {
  refreshRequired: boolean
  allKnown: boolean
  aligned: boolean
}

export interface DaemonVersionStatus {
  known: boolean
  updateRequired: boolean
  aligned: boolean
}

function known(version: string): boolean {
  return version.trim().length > 0
}

/**
 * The page-level release status compares only the served PWA and server.
 * Host daemon releases are evaluated per device because one nest may contain
 * multiple machines on different versions. Wire compatibility remains
 * governed separately by protocol_version.
 */
export function componentVersionStatus(
  versions: ComponentVersions
): ComponentVersionStatus {
  const frontend = versions.frontend.trim()
  const server = versions.server.trim()
  const refreshRequired = known(server) && frontend !== server
  const allKnown = known(frontend) && known(server)

  return {
    refreshRequired,
    allKnown,
    aligned: allKnown && frontend === server
  }
}

export function daemonVersionStatus(
  serverVersion: string,
  daemonVersion: string
): DaemonVersionStatus {
  const server = serverVersion.trim()
  const daemon = daemonVersion.trim()
  const versionsKnown = known(server) && known(daemon)

  return {
    known: versionsKnown,
    updateRequired: versionsKnown && daemon !== server,
    aligned: versionsKnown && daemon === server
  }
}
