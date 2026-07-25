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
  | 'approve'
  | 'deny'
  | 'interrupt'
  | 'heartbeat'
  | 'error'
  | 'register_device'
  | 'auth_response'
  | 'pair_request'
  | 'pair_confirm'
  | 'create_session'
  | 'session_created'
  | 'subscribe'

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

export type AgentType = 'claude_code' | 'codex'
export type AgentStatus = 'running' | 'idle' | 'waiting_approval'

export interface AgentSession {
  id: string
  device_id: string
  agent_type: AgentType
  status: AgentStatus
  summary: string
  last_activity: number
  pending_approval?: PendingApproval
}

export interface PendingApproval {
  id: string
  tool_name: string
  description: string
  parameters?: Record<string, unknown>
}

export interface SessionMessage {
  id: string
  role: 'assistant' | 'user' | 'tool' | 'system'
  content: string
  type?: 'thinking' | 'text' | 'tool_call' | 'tool_result' | 'error' | 'assistant' | 'system'
  timestamp: number
  metadata?: Record<string, unknown>
}
