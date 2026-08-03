export interface ComponentVersions {
  frontend: string
  server: string
  daemon: string
}

export interface ComponentVersionStatus {
  refreshRequired: boolean
  daemonUpdateRequired: boolean
  allKnown: boolean
  aligned: boolean
}

function known(version: string): boolean {
  return version.trim().length > 0
}

/**
 * Application releases should match across the served PWA, server binary and
 * selected host daemon. Wire compatibility remains governed separately by
 * protocol_version.
 */
export function componentVersionStatus(
  versions: ComponentVersions
): ComponentVersionStatus {
  const frontend = versions.frontend.trim()
  const server = versions.server.trim()
  const daemon = versions.daemon.trim()
  const refreshRequired = known(server) && frontend !== server
  const daemonUpdateRequired = known(server) && known(daemon) && daemon !== server
  const allKnown = known(frontend) && known(server) && known(daemon)

  return {
    refreshRequired,
    daemonUpdateRequired,
    allKnown,
    aligned: allKnown && frontend === server && daemon === server
  }
}
