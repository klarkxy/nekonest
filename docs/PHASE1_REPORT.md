# NekoNest Phase 1 施工报告

**日期**: 2026-07-25 18:55~19:03  
**状态**: P1 核心功能代码完成，等待编译验证

---

## 本轮完成: 实时消息流 (P1 核心)

### 数据流全链路打通

```
手机输入 prompt
  → PWA (send_prompt WebSocket)
  → Server (handlePhoneMessage → SendToDaemon)
  → Daemon (OnMessage → adapter.SendPrompt)
  → Claude Code / Codex (进程启动, --output-format json)
  → Daemon (OnOutput → parseAndForwardOutput → session_message)
  → Server (handleDaemonMessage → BroadcastToPhones)
  → PWA (session_message → messages.push)
  → 手机屏幕实时显示 ✨
```

### 修改文件 (11个)

| # | 文件 | 变更说明 |
|---|------|---------|
| 1 | `server/internal/protocol/types.go` | 新增 `MsgSessionMessage` 消息类型 |
| 2 | `server/internal/ws/handler.go` | 处理 `session_message` 并转发到手机端 |
| 3 | `daemon/cmd/daemon/main.go` | 新增 `setupOutputCallbacks` 将 agent 输出接入 WebSocket |
| 4 | `daemon/internal/agentexec/executor.go` | 改为 line-based 读取 stdout，适配 JSON 流 |
| 5 | `daemon/internal/agentexec/claude.go` | 新增 `OnAgentOutput` 回调 + Claude JSON 输出解析 |
| 6 | `daemon/internal/agentexec/codex.go` | 新增 `OnAgentOutput` 回调 + Codex JSON 输出解析 |
| 7 | `daemon/internal/adapters/claude_code.go` | 新增 `GetCommander()` 方法 |
| 8 | `daemon/internal/adapters/codex.go` | 新增 `GetCommander()` 方法 |
| 9 | `pwa/src/types/protocol.ts` | 新增 `session_message` / `prompt_sent` 类型 |
| 10 | `pwa/src/stores/session.ts` | 处理 `session_message` 追加到消息列表 |
| 11 | `pwa/src/views/SessionDetail.vue` | 自动滚动 + 消息气泡分类渲染 |

### 编译验证
- Server: ✅ 12MB
- Daemon: ✅ 7MB (CGO_ENABLED=0)
- PWA: ⏳ 待用户在店里电脑 approve-builds + build

### 关键设计决策

1. **Line-based 输出读取**: executor 从 chunk-based 改为 `bufio.Scanner` 逐行读取，因为 agent 输出是 NDJSON 格式
2. **回调链**: `Executor.OnOutput` → `Commander.parseAndForwardOutput` → `Commander.OnAgentOutput` → `daemon.sendSessionMessage` → `client.Send`
3. **Claude Code 输出解析**: 支持 `assistant`/`result`/`system` 三种消息类型，从 `message.content` 数组中提取 text/thinking/tool_use
4. **Codex 输出解析**: 支持 `assistant`/`tool`/`approval_request`/`approval_response` 事件类型
5. **消息上限**: PWA 端保留最近 500 条消息，防止内存溢出

---

## P1 功能完成度

| 功能 | 状态 | 说明 |
|------|------|------|
| Prompt 注入 | ✅ | 手机→Server→Daemon→Agent 全链路 |
| 审批流程 | ✅ | Approve/Deny 通过 stdin "y"/"n" 或文件回退 |
| 实时消息流 | ✅ | Agent stdout → session_message → 手机显示 |
| 会话发现 | ✅ | JSONL 文件扫描 + fsnotify 监控 |
| 中断 Agent | ✅ | os.Interrupt 信号 |
| 断线重连 | ✅ | PWA 指数退避重连 + 自动重订阅 |
| 设备配对 | ✅ | 6 位配对码 (REST API) |

---

## 下一步

1. 用户回到店里电脑后：解压 `nekonest-v3.tar.gz`，重新编译
2. PWA 构建: `pnpm approve-builds && pnpm build`
3. 端到端测试: 启动 Server → 启动 Daemon → PWA 连接 → 发送 prompt → 验证消息回流
