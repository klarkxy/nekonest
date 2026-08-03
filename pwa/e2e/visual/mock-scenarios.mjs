export const FIXED_NOW_MS = Date.parse('2026-08-02T09:00:00+08:00')
export const FIXED_NOW = Math.floor(FIXED_NOW_MS / 1000)
export const MAIN_DEVICE_ID = 'device-windows'
export const MAIN_SESSION_ID = 'session-codex'
export const NATIVE_THREAD_ID = 'native-thread-visual'
export const APP_VERSION = '0.2.1'

export const SCENARIO_NAMES = new Set([
  'setup-fresh',
  'setup-whitespace',
  'pair-initial',
  'pair-fingerprint',
  'pair-failure',
  'devices-loading',
  'devices-empty',
  'devices-mixed',
  'devices-auth-error',
  'devices-server-error',
  'device-full',
  'device-offline',
  'device-empty',
  'device-load-error',
  'device-filter',
  'device-archived',
  'session-rich',
  'prompt-queued',
  'prompt-sending',
  'prompt-accepted',
  'prompt-committed',
  'prompt-failed',
  'prompt-not-seen',
  'session-streaming',
  'session-approval',
  'session-disconnected',
  'session-unavailable',
  'session-attachment',
  'thread-starting',
  'thread-failed',
  'thread-indeterminate',
  'thread-owned'
])

const codexCapabilities = {
  control_mode: 'app_server',
  approve: true,
  deny: true,
  interrupt: true,
  steer: true,
  queue: false,
  spawn: true,
  attachment_mode: 'native_image_and_file'
}

const compatibilityCapabilities = {
  control_mode: 'compatibility',
  approve: false,
  deny: false,
  interrupt: true,
  steer: false,
  queue: false,
  spawn: false,
  attachment_mode: 'path_best_effort'
}

export function devicesFor(name) {
  if (name === 'devices-empty') return []
  const offline = name === 'device-offline'
  return [
    {
      id: MAIN_DEVICE_ID,
      name: '书房电脑',
      os: 'windows',
      status: offline ? 'offline' : 'online',
      last_seen: FIXED_NOW - (offline ? 3600 : 8),
      active_agents: 5,
      ...(offline ? {} : { daemon_version: APP_VERSION })
    },
    {
      id: 'device-linux',
      name: 'Linux 构建机',
      os: 'linux',
      status: 'offline',
      last_seen: FIXED_NOW - 7200,
      active_agents: 2
    }
  ]
}

function primarySession(name) {
  const approval = name === 'session-approval'
  const streaming = name === 'session-streaming'
  const unavailable = name === 'session-unavailable'
  return {
    id: MAIN_SESSION_ID,
    device_id: MAIN_DEVICE_ID,
    agent_type: 'codex',
    status: approval ? 'waiting_approval' : streaming ? 'running' : 'idle',
    summary: '完善 NekoNest 本地截图回归',
    last_activity: FIXED_NOW - 45,
    project_dir: 'D:\\0 code\\nekonest',
    project: 'nekonest',
    capabilities: unavailable
      ? {
          control_mode: 'exec_resume',
          approve: false,
          deny: false,
          interrupt: false,
          steer: false,
          queue: false,
          spawn: false,
          attachment_mode: 'unsupported'
        }
      : codexCapabilities,
    ...(approval
      ? {
          pending_approval: {
            id: 'approval-visual-1',
            tool_name: 'shell_command',
            description: '运行本地 Playwright 截图回归',
            parameters: {
              command: 'pnpm test:visual',
              workdir: 'D:\\0 code\\nekonest\\pwa'
            }
          }
        }
      : {})
  }
}

export function sessionsFor(name) {
  if (name === 'device-empty') return []
  if (name.startsWith('thread-')) {
    return [primarySession(name)]
  }
  if (!['device-full', 'device-filter', 'device-archived'].includes(name)) {
    return [primarySession(name)]
  }
  return [
    primarySession(name),
    {
      id: 'session-claude',
      device_id: MAIN_DEVICE_ID,
      agent_type: 'claude_code',
      status: 'idle',
      summary: '检查部署文档中英文一致性',
      last_activity: FIXED_NOW - 600,
      project_dir: 'D:\\0 code\\nekonest',
      project: 'nekonest',
      capabilities: compatibilityCapabilities
    },
    {
      id: 'session-kilo',
      device_id: MAIN_DEVICE_ID,
      agent_type: 'kilo',
      status: 'error',
      summary: '修复 Windows 构建脚本',
      last_activity: FIXED_NOW - 1200,
      project_dir: 'D:\\0 code\\nekonest',
      project: 'nekonest',
      capabilities: compatibilityCapabilities
    },
    {
      id: 'session-kimi',
      device_id: MAIN_DEVICE_ID,
      agent_type: 'kimi_cli',
      status: 'waiting_user',
      summary: '整理移动端交互说明',
      last_activity: FIXED_NOW - 1800,
      project_dir: 'D:\\0 code\\mobile-demo',
      project: 'mobile-demo',
      capabilities: compatibilityCapabilities
    },
    {
      id: 'session-grok',
      device_id: MAIN_DEVICE_ID,
      agent_type: 'grok_build',
      status: 'running',
      summary: '执行跨平台冒烟检查',
      last_activity: FIXED_NOW - 2400,
      capabilities: compatibilityCapabilities
    }
  ]
}

const richMessages = [
  {
    id: 'history-user-1',
    role: 'user',
    type: 'text',
    content: '请把本地截图测试补完整，并确认手机端没有横向溢出。',
    timestamp: FIXED_NOW - 240
  },
  {
    id: 'history-thinking-1',
    role: 'assistant',
    type: 'thinking',
    content: '正在检查路由、状态和响应式断点',
    timestamp: FIXED_NOW - 230
  },
  {
    id: 'history-tool-1',
    role: 'tool',
    type: 'tool_call',
    content: 'shell_command · pnpm test:visual',
    timestamp: FIXED_NOW - 220
  },
  {
    id: 'history-assistant-1',
    role: 'assistant',
    type: 'text',
    content: '## 检查完成\n\n- 设备列表与线程树已覆盖\n- 发送状态保持幂等\n- Markdown 输出经过清理\n\n`390 × 844` 主基线可以稳定复现。',
    timestamp: FIXED_NOW - 180,
    metadata: {
      attachments: [
        {
          id: 'visual-preview',
          url: '/api/e2e/preview.svg',
          name: 'visual-report.svg',
          mime: 'image/svg+xml',
          size: 1520
        },
        {
          id: 'visual-log',
          url: '/api/e2e/report.txt',
          name: 'playwright-report.txt',
          mime: 'text/plain',
          size: 640
        }
      ]
    }
  },
  {
    id: 'history-user-2',
    role: 'user',
    type: 'text',
    content: '再确认发送中、排队、提交成功和失败重试都使用真实协议状态。',
    timestamp: FIXED_NOW - 160
  },
  {
    id: 'history-assistant-2',
    role: 'assistant',
    type: 'text',
    content: '已逐项检查投递状态，并保留 client_msg_id 作为稳定消息标识。',
    timestamp: FIXED_NOW - 140
  },
  {
    id: 'history-tool-2',
    role: 'tool',
    type: 'tool_call',
    content: 'shell_command · pnpm type-check',
    timestamp: FIXED_NOW - 120
  },
  {
    id: 'history-assistant-3',
    role: 'assistant',
    type: 'text',
    content: '类型检查通过。接下来验证顶部历史与固定底部输入栏不会互相遮挡。',
    timestamp: FIXED_NOW - 100
  },
  {
    id: 'history-user-3',
    role: 'user',
    type: 'text',
    content: '最后检查窄屏、桌面、深色和英文布局。',
    timestamp: FIXED_NOW - 80
  },
  {
    id: 'history-assistant-4',
    role: 'assistant',
    type: 'text',
    content: '全部代表性视口已准备完成，可以生成稳定基线。',
    timestamp: FIXED_NOW - 60
  }
]

export function messagesFor(name, sessionId = MAIN_SESSION_ID) {
  if (sessionId === NATIVE_THREAD_ID) return []
  if (name === 'session-rich' || name === 'session-attachment') return richMessages
  if (name === 'session-streaming') {
    return [
      richMessages[0],
      {
        id: 'streaming-assistant',
        role: 'assistant',
        type: 'text',
        content: '正在生成截图矩阵，已完成设备列表和线程树…',
        timestamp: FIXED_NOW - 2
      }
    ]
  }
  return [
    {
      id: 'history-user-short',
      role: 'user',
      type: 'text',
      content: '继续完成本地截图测试。',
      timestamp: FIXED_NOW - 90
    },
    {
      id: 'history-assistant-short',
      role: 'assistant',
      type: 'text',
      content: '可以，测试通道已经准备好。',
      timestamp: FIXED_NOW - 70
    }
  ]
}

export function scenarioBehavior(name) {
  return {
    devicesLoading: name === 'devices-loading',
    devicesStatus: name === 'devices-auth-error' ? 401 : name === 'devices-server-error' ? 503 : 200,
    sessionsStatus: name === 'device-load-error' ? 503 : 200,
    pairStatus: name === 'pair-failure' ? 409 : 200,
    websocket: ['prompt-queued', 'session-disconnected'].includes(name) ? 'disconnect' : 'connected',
    promptOutcome: name.startsWith('prompt-') ? name.slice('prompt-'.length) : 'committed',
    threadOutcome: name.startsWith('thread-') ? name.slice('thread-'.length) : 'owned'
  }
}
