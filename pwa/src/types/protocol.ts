/** NekoNest protocol types (PWA) — keep in lockstep with protocol/protocol.json */

import { runtimeTransportMode } from '@/api/transport'

export const PROTOCOL_VERSION = '1.3' as const

export type TransportMode = 'sealed' | 'open'

export const SERVICE_ERROR_CODES = [
  'device_credential_invalid',
  'phone_credential_invalid',
  'access_suspended',
  'registration_disabled',
  'device_capacity_exceeded',
  'device_identity_conflict',
  'device_already_connected',
  'protocol_upgrade_required',
  'registration_rate_limited',
  'service_provisioning',
  'route_unavailable',
  'region_unavailable'
] as const

export type ServiceErrorCode = typeof SERVICE_ERROR_CODES[number]

export interface ServiceErrorPayload {
  error_code: ServiceErrorCode | string
  message: string
  retryable: boolean
  retry_after_seconds?: number
  action_url?: string
}

export type MessageType =
  | 'device_online'
  | 'device_offline'
  | 'device_list'
  | 'session_list'
  | 'refresh_sessions'
  | 'session_update'
  | 'session_message'
  | 'send_prompt'
  | 'prompt_status_query'
  | 'prompt_not_seen'
  | 'prompt_queued'
  | 'prompt_accepted'
  | 'prompt_committed'
  | 'prompt_failed'
  | 'prompt_sent' // deprecated: clear outbox on prompt_committed
  | 'approve'
  | 'deny'
  | 'interrupt'
  | 'steer'
  | 'respond_user_input'
  | 'user_input_result'
  | 'queue_update'
  | 'cancel_prompt'
  | 'prompt_cancelled'
  | 'resume_prompt_queue'
  | 'skip_prompt_queue_item'
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
  outcome?: string
  retry_allowed?: boolean
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
  send?: boolean
  approve?: boolean
  deny?: boolean
  interrupt?: boolean
  steer?: boolean
  queue?: boolean
  spawn?: boolean
  user_input?: boolean
  attachment_mode?: AttachmentMode
  control_path?: string
  control_version?: string
  unavailable_reasons?: Partial<Record<'send' | 'approve' | 'deny' | 'interrupt' | 'steer' | 'queue' | 'spawn' | 'user_input' | 'attachment', string>>
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
  attachment_mode?: AttachmentMode
}

export interface SessionListPayload {
  sessions?: AgentSession[]
  start_capabilities?: AgentStartCapability[]
}

export function capabilityEnabled(
  caps: SessionCapabilities | null | undefined,
  key: keyof Pick<SessionCapabilities, 'send' | 'approve' | 'deny' | 'interrupt' | 'steer' | 'queue' | 'spawn' | 'user_input'>
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
  pending_user_input?: PendingUserInput
  active_turn?: ActiveTurnBinding | null
}

export interface ActiveTurnBinding {
  generation: number
  client_msg_id: string
  native_request_id?: string
}

export interface PendingApproval {
  id: string
  tool_name: string
  description: string
  parameters?: Record<string, unknown>
}

export interface UserInputOption {
  label: string
  description: string
}

export interface UserInputQuestion {
  id: string
  header: string
  question: string
  options?: UserInputOption[]
  is_other?: boolean
  is_secret?: boolean
}

export interface PendingUserInput {
  request_id: string
  item_id: string
  questions: UserInputQuestion[]
  auto_resolution_ms?: number
  /** Unix milliseconds. */
  expires_at?: number
}

export interface QueueItem {
  client_msg_id: string
  position: number
  status: 'queued' | 'running' | 'completed' | 'blocked_failed' | 'blocked_interrupted' | 'blocked_indeterminate' | 'cancelled'
}

export interface PromptQueueState {
  paused: boolean
  items: QueueItem[]
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
  | 'cancelled'
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

/**
 * The mode is server-owned.  This synchronous helper is intentionally unable
 * to guess: callers must resolve /health before opening a websocket.
 */
export function nestTransportMode(): TransportMode {
  const mode = runtimeTransportMode()
  if (mode === 'sealed' || mode === 'open') return mode
  throw new Error('Nest transport mode has not been verified from /health')
}
