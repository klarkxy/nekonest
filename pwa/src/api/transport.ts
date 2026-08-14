import type { TransportMode } from '@/types/protocol'
import { apiURL, endpointOrigin, isManagedRuntime } from '@/config/runtimeEndpoint'

const VALID_MODES = new Set<TransportMode>(['sealed', 'open'])

export type TransportFailureKind =
  | 'consent_required'
  | 'mismatch'
  | 'downgrade_blocked'
  | 'managed_requires_sealed'
  | 'health_unavailable'
  | 'invalid_mode'
  | 'unknown'

let resolvedMode: TransportMode | null = null
let resolvePromise: Promise<TransportMode> | null = null
let transportError = ''
let transportKind: TransportFailureKind | '' = ''
let consentRequired = false

const PIN_KEY_PREFIX = 'nekonest_transport_pin:'
const OPEN_CONSENT_KEY_PREFIX = 'nekonest_open_transport_consent:'

function originKey(prefix: string): string {
  return `${prefix}${endpointOrigin()}`
}

function readLocal(key: string): string {
  try { return localStorage.getItem(key) || '' } catch { return '' }
}

function writeLocal(key: string, value: string) {
  try { localStorage.setItem(key, value) } catch { /* fail closed on the next verification */ }
}

export class OpenTransportConsentRequiredError extends Error {
  constructor() {
    super('This nest uses open relay. Confirm that prompts and responses may be visible to the server before connecting.')
    this.name = 'OpenTransportConsentRequiredError'
  }
}

function configuredOverride(): TransportMode | null {
  const value = (import.meta.env.VITE_NEKONEST_TRANSPORT_MODE as string | undefined)?.trim()
  if (!value) return null
  if (VALID_MODES.has(value as TransportMode)) return value as TransportMode
  throw new Error(`Invalid VITE_NEKONEST_TRANSPORT_MODE: ${value}`)
}

/**
 * Reads the nest's persisted mode before a live channel is opened.  A build
 * override is only an assertion for development; it never selects a mode.
 */
export async function ensureTransportMode(force = false): Promise<TransportMode> {
  if (resolvedMode && !force) return resolvedMode
  if (resolvePromise && !force) return resolvePromise

  resolvePromise = (async () => {
    try {
      const override = configuredOverride()
      const response = await fetch(apiURL('/health'), { cache: 'no-store' })
      if (!response.ok) {
        const err = new Error(`Could not read nest transport mode (HTTP ${response.status})`)
        ;(err as Error & { kind: TransportFailureKind }).kind = 'health_unavailable'
        throw err
      }
      const body = await response.json() as { transport_mode?: unknown }
      const mode = body.transport_mode
      if (!VALID_MODES.has(mode as TransportMode)) {
        const err = new Error('Nest server returned an invalid transport mode')
        ;(err as Error & { kind: TransportFailureKind }).kind = 'invalid_mode'
        throw err
      }
      if (override && override !== mode) {
        const err = new Error(`Transport mode mismatch: web app expects ${override}, nest server is ${mode}`)
        ;(err as Error & { kind: TransportFailureKind }).kind = 'mismatch'
        throw err
      }
      if (isManagedRuntime() && mode !== 'sealed') {
        const err = new Error('Managed NekoNest requires sealed transport')
        ;(err as Error & { kind: TransportFailureKind }).kind = 'managed_requires_sealed'
        throw err
      }
      const pinKey = originKey(PIN_KEY_PREFIX)
      const pinned = readLocal(pinKey)
      if (mode === 'open') {
        if (pinned === 'sealed') {
          const err = new Error('Transport downgrade blocked: this origin was previously pinned to sealed mode')
          ;(err as Error & { kind: TransportFailureKind }).kind = 'downgrade_blocked'
          throw err
        }
        if (readLocal(originKey(OPEN_CONSENT_KEY_PREFIX)) !== 'confirmed') {
          consentRequired = true
          throw new OpenTransportConsentRequiredError()
        }
      }
      writeLocal(pinKey, mode as TransportMode)
      resolvedMode = mode as TransportMode
      transportError = ''
      transportKind = ''
      consentRequired = false
      return resolvedMode
    } catch (error) {
      resolvedMode = null
      transportError = error instanceof Error ? error.message : 'Could not determine nest transport mode'
      const tagged = error as { kind?: TransportFailureKind; name?: string }
      if (tagged?.name === 'OpenTransportConsentRequiredError') transportKind = 'consent_required'
      else transportKind = tagged.kind || 'unknown'
      throw error
    } finally {
      resolvePromise = null
    }
  })()
  return resolvePromise
}

/** Returns the verified runtime mode only; callers must fail closed while unresolved. */
export function runtimeTransportMode(): TransportMode | null {
  return resolvedMode
}

export function transportModeError(): string {
  return transportError
}

export function transportModeKind(): TransportFailureKind | '' {
  return transportKind
}

export function openTransportConsentRequired(): boolean {
  return consentRequired
}

/** Records an explicit, origin-scoped operator decision; mode is re-read next. */
export function acknowledgeOpenTransport() {
  writeLocal(originKey(OPEN_CONSENT_KEY_PREFIX), 'confirmed')
  resolvedMode = null
  resolvePromise = null
  transportError = ''
  transportKind = ''
  consentRequired = false
}

/** Refresh is deliberate so reconnects can reuse the already verified value. */
export async function refreshTransportMode(): Promise<TransportMode> {
  return ensureTransportMode(true)
}

/** Test-only reset hook; production code must use ensureTransportMode. */
export function resetTransportModeForTests(mode: TransportMode | null = null) {
  resolvedMode = mode
  resolvePromise = null
  transportError = ''
  transportKind = ''
  consentRequired = false
}
