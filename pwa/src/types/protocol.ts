/** NekoNest protocol types (PWA) — keep in lockstep with protocol/protocol.json */

export const PROTOCOL_VERSION = '1.0' as const

export type TransportMode = 'sealed' | 'open'

export type MessageType =
  | 'device_online'
  | 'device_offline'
  | 'device_list'
  | 'session_list'
  | 'session_update'
  | 'session_message'
  | 'send_prompt'
  | 'prompt_status_query'
  | 'prompt_not_seen'
  | 'prompt_accepted'
  | 'prompt_committed'
  | 'prompt_failed'
  | 'prompt_sent' // deprecated: clear outbox on prompt_committed
  | 'approve'
  | 'deny'
  | 'interrupt'
  | 'steer'
  | 'start_thread'
  | 'thread_starting'
  | 'thread_owned'
  | 'thread_failed'
  | 'thread_indeterminate'
  | 'heartbeat'
  | 'error'
  | 'register_device'
  | 'auth_response'
  | 'pair_request'
  | 'pair_confirm'
  | 'pair_ready'
  | 'pair_failed'
  | 'key_package'
  | 'phone_revoked'
  | 'attention_event'
  | 'subscribe'
  | 'fetch_history'
  | 'session_history'
  | 'subscribe_ack'

export type KeyScope = 'device_catalog' | 'session'

export interface SealedPayload {
  alg: string
  version?: number
  key_scope: KeyScope
  epoch: number
  sender_id: string
  recipient_id: string
  sequence: number
  nonce: string
  ciphertext: string
}

export interface NekoMessage {
  protocol_version?: string
  transport_mode?: TransportMode
  type: MessageType
  device_id: string
  session_id?: string
  client_msg_id?: string
  timestamp: number
  payload?: Record<string, unknown>
  sealed_payload?: SealedPayload
}

export interface Device {
  id: string
  name: string
  os: 'windows' | 'linux' | string
  status: 'online' | 'offline'
  last_seen: number
  active_agents: number
  /** Application release reported by the current live daemon connection. */
  daemon_version?: string
}

export type KnownAgentType =
  | 'claude_code'
  | 'codex'
  | 'kilo'
  | 'kimi_cli'
  | 'grok_build'

/** Known agents keep editor completion while future daemon adapters remain wire-compatible. */
export type AgentType = KnownAgentType | (string & {})
export type AgentStatus = 'running' | 'idle' | 'waiting_user' | 'waiting_approval' | 'error'

export type ControlMode = 'app_server' | 'exec_resume' | 'compatibility'
export type AttachmentMode =
  | 'native_image_and_file'
  | 'native_image'
  | 'path_best_effort'
  | 'unsupported'

/** Absent fields mean unsupported / false. */
export interface SessionCapabilities {
  control_mode?: ControlMode
  approve?: boolean
  deny?: boolean
  interrupt?: boolean
  steer?: boolean
  queue?: boolean
  spawn?: boolean
  attachment_mode?: AttachmentMode
}

/**
 * Device-level native thread-start capability sent with `session_list`.
 * Older daemons omit the catalog entirely; callers must then use the legacy
 * per-session Codex spawn capability rather than assuming support.
 */
export interface AgentStartCapability {
  agent_type: AgentType
  available: boolean
  spawn: boolean
  reason?: string
  control_path?: string
  control_version?: string
}

export interface SessionListPayload {
  sessions?: AgentSession[]
  start_capabilities?: AgentStartCapability[]
}

export function capabilityEnabled(
  caps: SessionCapabilities | null | undefined,
  key: keyof Pick<SessionCapabilities, 'approve' | 'deny' | 'interrupt' | 'steer' | 'queue' | 'spawn'>
): boolean {
  return Boolean(caps?.[key])
}

export function attachmentModeOf(
  caps: SessionCapabilities | null | undefined
): AttachmentMode {
  return caps?.attachment_mode || 'unsupported'
}

export interface AgentSession {
  id: string
  device_id: string
  agent_type: AgentType
  status: AgentStatus
  summary: string
  last_activity: number
  /** Full project/workspace path on the host */
  project_dir?: string
  /** Short project folder name */
  project?: string
  capabilities?: SessionCapabilities
  pending_approval?: PendingApproval
}

export interface PendingApproval {
  id: string
  tool_name: string
  description: string
  parameters?: Record<string, unknown>
}

export interface AttachmentRef {
  id?: string
  url: string
  name?: string
  mime?: string
  size?: number
  key?: string
}

export type DeliveryStatus =
  | 'queued'
  | 'sending'
  | 'accepted'
  | 'committed'
  | 'not_seen'
  | 'failed'
  | 'indeterminate'

export interface SessionMessage {
  id: string
  role: 'assistant' | 'user' | 'tool' | 'system'
  content: string
  type?: 'thinking' | 'text' | 'tool_call' | 'tool_result' | 'error' | 'assistant' | 'system'
  timestamp: number
  metadata?: {
    attachments?: AttachmentRef[]
    /** Local delivery state for a prompt waiting on daemon acceptance. */
    delivery_status?: DeliveryStatus
    delivery_error?: string
    delivery_retry_allowed?: boolean
    [key: string]: unknown
  }
}

/** Nest transport preference until sealed crypto is fully client-side. */
export function nestTransportMode(): TransportMode {
  const raw = (import.meta.env.VITE_NEKONEST_TRANSPORT_MODE as string | undefined)?.trim()
  if (raw === 'sealed' || raw === 'open') return raw
  // Default open while E2E crypto lands; sealed becomes default with crypto.
  return 'open'
}
