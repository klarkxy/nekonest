/** NekoNest 协议类型定义 (PWA 端) */

export type MessageType =
  | 'device_online'
  | 'device_offline'
  | 'device_list'
  | 'session_list'
  | 'session_update'
  | 'session_message'
  | 'send_prompt'
  | 'prompt_sent'
  | 'prompt_failed'
  | 'approve'
  | 'deny'
  | 'interrupt'
  | 'heartbeat'
  | 'error'
  | 'register_device'
  | 'auth_response'
  | 'pair_request'
  | 'pair_confirm'
  | 'subscribe'
  | 'fetch_history'
  | 'session_history'
  | 'subscribe_ack'

export interface NekoMessage {
  type: MessageType
  device_id: string
  session_id?: string
  timestamp: number
  payload?: Record<string, unknown>
}

export interface Device {
  id: string
  name: string
  os: 'windows'
  status: 'online' | 'offline'
  last_seen: number
  active_agents: number
}

export type KnownAgentType =
  | 'claude_code'
  | 'codex'
  | 'kilo'
  | 'kimi_cli'
  | 'grok_build'

/** Known agents keep editor completion while future daemon adapters remain wire-compatible. */
export type AgentType = KnownAgentType | (string & {})
export type AgentStatus = 'running' | 'idle' | 'waiting_approval'

export interface AgentSession {
  id: string
  device_id: string
  agent_type: AgentType
  status: AgentStatus
  summary: string
  last_activity: number
  /** Full project/workspace path on the PC */
  project_dir?: string
  /** Short project folder name */
  project?: string
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

export interface SessionMessage {
  id: string
  role: 'assistant' | 'user' | 'tool' | 'system'
  content: string
  type?: 'thinking' | 'text' | 'tool_call' | 'tool_result' | 'error' | 'assistant' | 'system'
  timestamp: number
  metadata?: {
    attachments?: AttachmentRef[]
    /** Local delivery state for a prompt waiting on daemon acceptance. */
    delivery_status?: 'queued' | 'sending' | 'failed'
    delivery_error?: string
    delivery_retry_allowed?: boolean
    [key: string]: unknown
  }
}
