import { createServer } from 'node:http'
import { resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { WebSocketServer } from 'ws'
import {
  FIXED_NOW,
  MAIN_DEVICE_ID,
  MAIN_SESSION_ID,
  NATIVE_THREAD_ID,
  SCENARIO_NAMES,
  devicesFor,
  messagesFor,
  scenarioBehavior,
  sessionsFor
} from './mock-scenarios.mjs'

const HOST = '127.0.0.1'
const PORT = 18080
let activeScenario = 'devices-mixed'

function sendJSON(response, status, value) {
  const body = JSON.stringify(value)
  response.writeHead(status, {
    'Content-Type': 'application/json; charset=utf-8',
    'Content-Length': Buffer.byteLength(body),
    'Cache-Control': 'no-store'
  })
  response.end(body)
}

function readJSON(request) {
  return new Promise((resolve, reject) => {
    const chunks = []
    request.on('data', chunk => chunks.push(chunk))
    request.on('end', () => {
      try {
        resolve(chunks.length ? JSON.parse(Buffer.concat(chunks).toString('utf8')) : {})
      } catch (error) {
        reject(error)
      }
    })
    request.on('error', reject)
  })
}

function scenarioSnapshot() {
  const name = activeScenario
  return {
    name,
    behavior: scenarioBehavior(name),
    devices: devicesFor(name),
    sessions: sessionsFor(name)
  }
}

const server = createServer(async (request, response) => {
  const url = new URL(request.url || '/', `http://${HOST}:${PORT}`)
  const state = scenarioSnapshot()

  if (url.pathname === '/__e2e/health') {
    sendJSON(response, 200, { ok: true, scenario: activeScenario })
    return
  }

  if (url.pathname === '/__e2e/scenario' && request.method === 'POST') {
    try {
      const body = await readJSON(request)
      const name = String(body?.name || '')
      if (!SCENARIO_NAMES.has(name)) {
        sendJSON(response, 400, { error: `unknown scenario: ${name}` })
        return
      }
      activeScenario = name
      for (const client of webSockets.clients) client.close(1000, 'scenario changed')
      sendJSON(response, 200, { ok: true, scenario: activeScenario })
    } catch {
      sendJSON(response, 400, { error: 'invalid JSON' })
    }
    return
  }

  if (url.pathname === '/api/devices' && request.method === 'GET') {
    if (state.behavior.devicesLoading) {
      request.on('close', () => response.destroy())
      setTimeout(() => {
        if (!response.destroyed) sendJSON(response, 200, { devices: state.devices })
      }, 20_000)
      return
    }
    if (state.behavior.devicesStatus !== 200) {
      sendJSON(response, state.behavior.devicesStatus, { error: 'visual fixture error' })
      return
    }
    sendJSON(response, 200, { devices: state.devices })
    return
  }

  if (url.pathname === '/api/devices/sessions' && request.method === 'GET') {
    if (state.behavior.sessionsStatus !== 200) {
      sendJSON(response, state.behavior.sessionsStatus, { error: 'visual fixture error' })
      return
    }
    sendJSON(response, 200, { sessions: state.sessions })
    return
  }

  if (url.pathname === '/api/messages' && request.method === 'GET') {
    const sessionId = url.searchParams.get('session_id') || MAIN_SESSION_ID
    sendJSON(response, 200, { messages: messagesFor(state.name, sessionId) })
    return
  }

  if (url.pathname === '/api/pair/consume' && request.method === 'POST') {
    request.resume()
    if (state.behavior.pairStatus !== 200) {
      sendJSON(response, state.behavior.pairStatus, { error: 'fingerprint mismatch' })
      return
    }
    sendJSON(response, 200, {
      device_id: MAIN_DEVICE_ID,
      name: '书房电脑',
      phone_id: 'visual-phone',
      phone_token: 'visual-phone-token'
    })
    return
  }

  if (url.pathname === '/api/attachments' && request.method === 'POST') {
    request.resume()
    sendJSON(response, 200, {
      id: 'visual-upload',
      url: '/api/e2e/uploaded-file',
      name: 'visual-check.txt',
      mime: 'text/plain',
      size: 28
    })
    return
  }

  if (url.pathname === '/api/push/vapid-public-key') {
    sendJSON(response, 200, { enabled: false })
    return
  }

  if (url.pathname === '/api/e2e/preview.svg') {
    const svg = [
      '<svg xmlns="http://www.w3.org/2000/svg" width="480" height="270" viewBox="0 0 480 270">',
      '<rect width="480" height="270" rx="24" fill="#f2e8ff"/>',
      '<path d="M74 190h332" stroke="#8b5cf6" stroke-width="12" stroke-linecap="round"/>',
      '<circle cx="132" cy="116" r="42" fill="#f472b6"/>',
      '<rect x="204" y="72" width="204" height="26" rx="13" fill="#6d5b7b"/>',
      '<rect x="204" y="116" width="150" height="20" rx="10" fill="#a58bb3"/>',
      '<text x="240" y="224" text-anchor="middle" font-family="sans-serif" font-size="22" fill="#4d3e58">NekoNest visual report</text>',
      '</svg>'
    ].join('')
    response.writeHead(200, {
      'Content-Type': 'image/svg+xml; charset=utf-8',
      'Content-Length': Buffer.byteLength(svg),
      'Cache-Control': 'no-store'
    })
    response.end(svg)
    return
  }

  if (url.pathname === '/api/e2e/report.txt' || url.pathname === '/api/e2e/uploaded-file') {
    const body = 'NekoNest deterministic visual fixture\n'
    response.writeHead(200, {
      'Content-Type': 'text/plain; charset=utf-8',
      'Content-Length': Buffer.byteLength(body),
      'Cache-Control': 'no-store'
    })
    response.end(body)
    return
  }

  sendJSON(response, 404, { error: 'not found' })
})

const webSockets = new WebSocketServer({ noServer: true })

server.on('upgrade', (request, socket, head) => {
  const url = new URL(request.url || '/', `http://${HOST}:${PORT}`)
  if (url.pathname !== '/ws/phone') {
    socket.destroy()
    return
  }
  webSockets.handleUpgrade(request, socket, head, ws => webSockets.emit('connection', ws, request))
})

function sendFrame(socket, frame) {
  if (socket.readyState !== 1) return
  socket.send(JSON.stringify({
    protocol_version: '1.0',
    transport_mode: 'open',
    timestamp: FIXED_NOW,
    ...frame
  }))
}

function sendSessionMessage(socket, sessionId, message) {
  sendFrame(socket, {
    type: 'session_message',
    device_id: MAIN_DEVICE_ID,
    session_id: sessionId,
    payload: { message }
  })
}

webSockets.on('connection', socket => {
  socket.on('message', raw => {
    let message
    try {
      message = JSON.parse(raw.toString())
    } catch {
      return
    }

    const state = scenarioSnapshot()
    const deviceId = message.device_id || MAIN_DEVICE_ID

    if (message.type === 'subscribe') {
      if (state.behavior.websocket === 'disconnect') {
        socket.close(1012, 'visual fixture disconnected')
        return
      }
      sendFrame(socket, {
        type: 'subscribe_ack',
        device_id: deviceId,
        payload: {
          subscription_id: message.payload?.subscription_id
        }
      })
      sendFrame(socket, {
        type: 'session_list',
        device_id: deviceId,
        payload: { sessions: state.sessions }
      })
      if (state.name === 'session-streaming') {
        setTimeout(() => {
          sendSessionMessage(socket, MAIN_SESSION_ID, {
            id: 'stream-live-2',
            role: 'assistant',
            type: 'text',
            content: '正在补充深色模式和窄屏截图…',
            timestamp: FIXED_NOW
          })
        }, 120)
      }
      return
    }

    if (message.type === 'fetch_history') {
      sendFrame(socket, {
        type: 'session_history',
        device_id: deviceId,
        session_id: message.session_id,
        payload: { messages: messagesFor(state.name, message.session_id) }
      })
      return
    }

    if (message.type === 'send_prompt') {
      const clientMsgId = message.client_msg_id || message.payload?.client_msg_id
      const prompt = message.payload?.prompt || ''
      const base = {
        device_id: deviceId,
        session_id: message.session_id,
        client_msg_id: clientMsgId,
        payload: { client_msg_id: clientMsgId }
      }
      if (state.behavior.promptOutcome === 'sending') return
      if (state.behavior.promptOutcome === 'accepted') {
        setTimeout(() => sendFrame(socket, { type: 'prompt_accepted', ...base }), 80)
        return
      }
      if (state.behavior.promptOutcome === 'failed') {
        setTimeout(() => sendFrame(socket, {
          type: 'prompt_failed',
          ...base,
          payload: {
            client_msg_id: clientMsgId,
            reason: 'Agent 暂时不可用',
            retry_allowed: true
          }
        }), 80)
        return
      }
      if (state.behavior.promptOutcome === 'not-seen') {
        setTimeout(() => sendFrame(socket, { type: 'prompt_not_seen', ...base }), 80)
        return
      }
      setTimeout(() => {
        sendFrame(socket, {
          type: 'prompt_committed',
          ...base,
          payload: {
            client_msg_id: clientMsgId,
            message_id: clientMsgId,
            prompt
          }
        })
        sendSessionMessage(socket, message.session_id, {
          id: `assistant-${clientMsgId}`,
          role: 'assistant',
          type: 'text',
          content: '已收到，视觉回归状态正常。',
          timestamp: FIXED_NOW
        })
      }, 100)
      return
    }

    if (message.type === 'start_thread') {
      const operationId = message.payload?.operation_id
      const firstPrompt = String(message.payload?.prompt || '').trim()
      sendFrame(socket, {
        type: 'thread_starting',
        device_id: deviceId,
        payload: { operation_id: operationId }
      })
      if (state.behavior.threadOutcome === 'starting') return
      if (state.behavior.threadOutcome === 'failed') {
        setTimeout(() => sendFrame(socket, {
          type: 'thread_failed',
          device_id: deviceId,
          payload: {
            operation_id: operationId,
            error: 'Codex app-server 当前不可用'
          }
        }), 160)
        return
      }

      const nativeSession = {
        id: NATIVE_THREAD_ID,
        device_id: deviceId,
        agent_type: 'codex',
        status: 'running',
        summary: firstPrompt || '新建 Codex 线团',
        last_activity: FIXED_NOW,
        project_dir: 'D:\\0 code\\nekonest',
        project: 'nekonest',
        capabilities: {
          control_mode: 'app_server',
          approve: true,
          deny: true,
          interrupt: true,
          steer: true,
          spawn: true,
          attachment_mode: 'native_image_and_file'
        }
      }
      setTimeout(() => {
        sendFrame(socket, {
          type: 'session_update',
          device_id: deviceId,
          session_id: NATIVE_THREAD_ID,
          payload: { session: nativeSession }
        })
        sendFrame(socket, {
          type: state.behavior.threadOutcome === 'indeterminate'
            ? 'thread_indeterminate'
            : 'thread_owned',
          device_id: deviceId,
          payload: {
            operation_id: operationId,
            session_id: NATIVE_THREAD_ID,
            ...(state.behavior.threadOutcome === 'indeterminate'
              ? { error: 'Daemon 未能确认最终回执' }
              : {})
          }
        })
        if (state.behavior.threadOutcome === 'owned') {
          setTimeout(() => {
            sendSessionMessage(socket, NATIVE_THREAD_ID, {
              id: 'native-assistant-visual',
              role: 'assistant',
              type: 'text',
              content: 'pong ×19 🏓',
              timestamp: FIXED_NOW + 1
            })
            sendFrame(socket, {
              type: 'session_update',
              device_id: deviceId,
              session_id: NATIVE_THREAD_ID,
              payload: { session: { ...nativeSession, status: 'idle' } }
            })
          }, 260)
        }
      }, 160)
    }
  })
})

export function startVisualMock() {
  return new Promise((resolve, reject) => {
    const onError = error => {
      server.off('listening', onListening)
      reject(error)
    }
    const onListening = () => {
      server.off('error', onError)
      resolve(async () => {
        for (const client of webSockets.clients) client.terminate()
        await new Promise(closeResolve => server.close(closeResolve))
      })
    }
    server.once('error', onError)
    server.once('listening', onListening)
    server.listen(PORT, HOST)
  })
}

if (process.argv[1] && fileURLToPath(import.meta.url) === resolve(process.argv[1])) {
  const stop = await startVisualMock()
  const shutdown = async () => {
    await stop()
    process.exit(0)
  }
  process.on('SIGINT', shutdown)
  process.on('SIGTERM', shutdown)
}
