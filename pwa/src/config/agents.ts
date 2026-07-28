import type { AgentType, KnownAgentType } from '@/types/protocol'

export type AgentMeta = {
  id: AgentType
  label: string
  avatar: string
  color: string
  softColor: string
  symbol: string
}

export const KNOWN_AGENT_TYPES = [
  'claude_code',
  'codex',
  'kilo',
  'kimi_cli',
  'grok_build'
] as const satisfies readonly KnownAgentType[]

export const AGENT_CATALOG: Readonly<Record<KnownAgentType, AgentMeta>> = {
  claude_code: {
    id: 'claude_code',
    label: 'Claude Code',
    avatar: '/agents/claude-code.webp',
    color: '#C87558',
    softColor: '#F8E7DF',
    symbol: '🟠'
  },
  codex: {
    id: 'codex',
    label: 'Codex',
    avatar: '/agents/codex.webp',
    color: '#4FA879',
    softColor: '#E2F3EA',
    symbol: '🟢'
  },
  kilo: {
    id: 'kilo',
    label: 'Kilo',
    avatar: '/agents/kilo.webp',
    color: '#D96675',
    softColor: '#FAE5E9',
    symbol: '🔴'
  },
  kimi_cli: {
    id: 'kimi_cli',
    label: 'Kimi CLI',
    avatar: '/agents/kimi-cli.webp',
    color: '#5878D8',
    softColor: '#E7ECFC',
    symbol: '🔵'
  },
  grok_build: {
    id: 'grok_build',
    label: 'Grok Build',
    avatar: '/agents/grok-build.webp',
    color: '#303744',
    softColor: '#E7E9ED',
    symbol: '⚫'
  }
}

export const UNKNOWN_AGENT_META: AgentMeta = {
  id: 'unknown',
  label: '未知智能体',
  avatar: '/agents/unknown.webp',
  color: '#8F7FC2',
  softColor: '#EEEAF8',
  symbol: '🐱'
}

export function getAgentMeta(type?: AgentType | null): AgentMeta {
  const normalized = String(type || '').trim()
  const known = AGENT_CATALOG[normalized as KnownAgentType]
  if (known) return known
  if (!normalized || normalized === 'unknown') return UNKNOWN_AGENT_META
  return {
    ...UNKNOWN_AGENT_META,
    id: normalized,
    label: normalized
  }
}

export function agentOrder(type?: AgentType | null): number {
  const index = KNOWN_AGENT_TYPES.indexOf(type as KnownAgentType)
  return index >= 0 ? index : KNOWN_AGENT_TYPES.length
}
