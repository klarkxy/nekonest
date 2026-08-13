export type NekoNestRuntimeConfig = {
  api_base?: string
  ws_base?: string
  attachment_base?: string
  push_base?: string
  handoff_exchange_path?: string
  managed?: boolean
}

declare global {
  interface Window {
    __NEKONEST_RUNTIME_CONFIG__?: NekoNestRuntimeConfig
  }
}

function trimBase(value: string | undefined): string {
  return (value || '').trim().replace(/\/+$/, '')
}

function runtimeConfig(): NekoNestRuntimeConfig {
  if (typeof window === 'undefined') return {}
  return window.__NEKONEST_RUNTIME_CONFIG__ || {}
}

function managedBuild(): boolean {
  return String(import.meta.env.VITE_NEKONEST_MANAGED || '').toLowerCase() === 'true'
}

function validateRuntimeConfig(value: unknown): NekoNestRuntimeConfig {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) {
    throw new Error('Invalid NekoNest runtime config')
  }
  const config = value as Record<string, unknown>
  const allowed = new Set([
    'api_base',
    'ws_base',
    'attachment_base',
    'push_base',
    'handoff_exchange_path',
    'managed'
  ])
  if (Object.keys(config).some(key => !allowed.has(key))) {
    throw new Error('Invalid NekoNest runtime config field')
  }
  for (const key of ['api_base', 'ws_base', 'attachment_base', 'push_base', 'handoff_exchange_path'] as const) {
    if (config[key] !== undefined && typeof config[key] !== 'string') {
      throw new Error(`Invalid NekoNest runtime config ${key}`)
    }
  }
  if (config.managed !== undefined && typeof config.managed !== 'boolean') {
    throw new Error('Invalid NekoNest runtime config managed flag')
  }
  return config as NekoNestRuntimeConfig
}

/**
 * Loads deploy-time endpoint configuration before any identity or network
 * operation. The file is deliberately excluded from the service-worker
 * precache so a regional endpoint can change without rebuilding the PWA.
 */
export async function loadRuntimeConfig(): Promise<NekoNestRuntimeConfig> {
  if (typeof window === 'undefined') return {}
  let response: Response
  try {
    response = await fetch('/runtime-config.json', { cache: 'no-store', credentials: 'omit' })
  } catch (error) {
    if (managedBuild()) {
      const detail = error instanceof Error && error.message ? `: ${error.message}` : ''
      throw new Error(`Managed NekoNest runtime config is unavailable${detail}`)
    }
    window.__NEKONEST_RUNTIME_CONFIG__ = {}
    return {}
  }
  const contentType = response.headers.get('content-type') || ''
  if (!response.ok || !contentType.toLowerCase().includes('application/json')) {
    if (managedBuild()) throw new Error('Managed NekoNest runtime config is unavailable')
    window.__NEKONEST_RUNTIME_CONFIG__ = {}
    return {}
  }
  let config: NekoNestRuntimeConfig
  try {
    config = validateRuntimeConfig(await response.json())
    // Validate configured origins immediately instead of waiting for first use.
    if (config.api_base) requireHTTPOrigin(trimBase(config.api_base))
    if (config.ws_base) requireWSOrigin(trimBase(config.ws_base))
    if (config.attachment_base) requireHTTPOrigin(trimBase(config.attachment_base))
    if (config.push_base) requireHTTPOrigin(trimBase(config.push_base))
  } catch (error) {
    if (managedBuild()) throw error
    window.__NEKONEST_RUNTIME_CONFIG__ = {}
    return {}
  }
  if ((managedBuild() || config.managed) && !config.api_base) {
    throw new Error('Managed NekoNest runtime config requires api_base')
  }
  window.__NEKONEST_RUNTIME_CONFIG__ = config
  return config
}

function configuredAPIBase(): string {
  const config = runtimeConfig()
  return trimBase(config.api_base || import.meta.env.VITE_NEKONEST_API_BASE)
}

function configuredWSBase(): string {
  const config = runtimeConfig()
  if (config.ws_base) return trimBase(config.ws_base)
  // A deploy-time API origin is the stable endpoint. Do not keep a baked
  // WebSocket host after runtime-config.json has selected a new API origin.
  if (config.api_base) return ''
  return trimBase(import.meta.env.VITE_NEKONEST_WS_BASE)
}

function requireHTTPOrigin(raw: string): URL {
  const parsed = new URL(raw)
  if (!['http:', 'https:'].includes(parsed.protocol) || parsed.username || parsed.password) {
    throw new Error('Invalid NekoNest API base')
  }
  if ((parsed.pathname && parsed.pathname !== '/') || parsed.search || parsed.hash) {
    throw new Error('NekoNest API base must be an origin')
  }
  return parsed
}

function requireWSOrigin(raw: string): URL {
  const parsed = new URL(raw)
  if (!['ws:', 'wss:'].includes(parsed.protocol) || parsed.username || parsed.password) {
    throw new Error('Invalid NekoNest WebSocket base')
  }
  if ((parsed.pathname && parsed.pathname !== '/') || parsed.search || parsed.hash) {
    throw new Error('NekoNest WebSocket base must be an origin')
  }
  return parsed
}

/** The trusted API origin used for credential, transport-pin, and consent scoping. */
export function endpointOrigin(): string {
  const configured = configuredAPIBase()
  if (configured) return requireHTTPOrigin(configured).origin
  return location.origin
}

/** Resolves a Relay REST path while preserving relative URLs for self-hosted same-origin builds. */
export function apiURL(path: string): string {
  return resolveHTTPServiceURL(path, configuredAPIBase(), 'API')
}

function isUnsafeServicePath(path: string): boolean {
  return !path
    || path.includes('\\')
    || path.startsWith('//')
    || (/^[a-z][a-z0-9+.-]*:/i.test(path) && !/^https?:\/\//i.test(path))
}

function resolveHTTPServiceURL(path: string, configuredBase: string, label: string): string {
  if (isUnsafeServicePath(path)) {
    throw new Error(`NekoNest ${label} URL escaped the configured service origin`)
  }
  const expectedOrigin = configuredBase
    ? requireHTTPOrigin(configuredBase).origin
    : location.origin
  if (!/^https?:\/\//i.test(path)) {
    if (!path.startsWith('/')) {
      throw new Error(`NekoNest ${label} URL escaped the configured service origin`)
    }
    if (!configuredBase) return path
    path = new URL(path, `${expectedOrigin}/`).toString()
  }
  const parsed = new URL(path)
  if (parsed.origin !== expectedOrigin || parsed.username || parsed.password) {
    throw new Error(`NekoNest ${label} URL escaped the configured service origin`)
  }
  return parsed.toString()
}

/** Resolves capability URLs against an explicitly trusted attachment origin. */
export function attachmentURL(path: string): string {
  const configured = trimBase(runtimeConfig().attachment_base) || configuredAPIBase()
  return resolveHTTPServiceURL(path, configured, 'attachment')
}

/** Resolves Web Push control requests against an explicitly trusted push origin. */
export function pushURL(path: string): string {
  const configured = trimBase(runtimeConfig().push_base) || configuredAPIBase()
  return resolveHTTPServiceURL(path, configured, 'push')
}

/** Resolves a Relay WebSocket path. The stable managed endpoint is never learned from a response. */
export function websocketURL(path = '/ws/phone'): string {
  if (!path.startsWith('/') || path.startsWith('//') || path.includes('\\')) {
    throw new Error('Invalid NekoNest WebSocket path')
  }
  const configuredWS = configuredWSBase()
  if (configuredWS) {
    return new URL(path, `${requireWSOrigin(configuredWS).origin}/`).toString()
  }
  const configuredAPI = configuredAPIBase()
  const base = configuredAPI
    ? requireHTTPOrigin(configuredAPI)
    : new URL(location.origin)
  base.protocol = base.protocol === 'https:' ? 'wss:' : 'ws:'
  return new URL(path, `${base.origin}/`).toString()
}

export function handoffExchangePath(): string {
  const configured = runtimeConfig().handoff_exchange_path?.trim()
  if (!configured) return '/api/pwa/handoff/exchange'
  if (!configured.startsWith('/') || configured.startsWith('//') || configured.includes('\\')) {
    throw new Error('Invalid NekoNest handoff path')
  }
  return configured
}

export function isManagedRuntime(): boolean {
  return managedBuild() || runtimeConfig().managed === true
}

/** Test hook. Runtime config is intentionally read on every call, so no cache reset is required. */
export function setRuntimeConfigForTests(config?: NekoNestRuntimeConfig) {
  if (typeof window === 'undefined') return
  window.__NEKONEST_RUNTIME_CONFIG__ = config
}
