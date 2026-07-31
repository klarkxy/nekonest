> [English](./protocol.md) | 简体中文

# 协议概览

NekoNest 手机 ↔ server ↔ daemon 通信的语言无关线协议。规范性 schema 文件为 [`protocol/protocol.json`](../protocol/protocol.json)。类型**手动维护**于：

- `protocol/protocol.json`
- `server/internal/protocol/types.go`
- `pwa/src/types/protocol.ts`
- Daemon 分发 / payload 构造

JSON 字段名、枚举、可选性、时间戳与语义须在各面保持一致。

## 版本与传输模式

| 字段 | 规则 |
|---|---|
| `protocol_version` | `major.minor`（当前 **1.0**）。主版本不匹配则拒绝；次版本向后兼容（未知可选字段忽略）。 |
| `transport_mode` | 全窝统一 `sealed` \| `open`。一窝一种模式；**禁止** sealed→open 自动降级。新窝默认 **sealed**。 |

首帧（daemon 的 `register_device`、手机的 `subscribe`）**必须**带上述字段。Server 在 `auth_response` / `subscribe_ack` 返回协商结果。稳定错误码：`version_mismatch`、`transport_mode_mismatch`、`invalid_envelope`。

## 信封

每条 WebSocket 应用消息为 `NekoMessage`：

| 字段 | 必需 | 说明 |
|---|---|---|
| `protocol_version` | 首帧必需 | `major.minor` |
| `transport_mode` | 首帧必需 | `sealed` \| `open` |
| `type` | 是 | `MessageType` 枚举字符串之一 |
| `device_id` | 是 | 设备标识（daemon 身份或路由上下文） |
| `timestamp` | 是 | Unix 时间戳，**秒** |
| `session_id` | 否 | 会话级消息时的 agent 线程 id |
| `client_msg_id` | 否 | prompt/start 幂等 id（中继可见） |
| `payload` | 否 | 开放模式或明文控制载荷；有 `sealed_payload` 时**必须缺省** |
| `sealed_payload` | 否 | 密文信封；开放模式下**必须缺省** |

`payload` 与 `sealed_payload` 互斥。schema 上信封 `additionalProperties: false`。

## 智能体类型标识

| 线 id | 产品名 |
|---|---|
| `claude_code` | Claude Code |
| `codex` | Codex |
| `kilo` | Kilo |
| `kimi_cli` | Kimi CLI |
| `grok_build` | Grok Build |

新增智能体需适配器 + 注册表、server 类型、PWA 目录/资源、schema、测试与文档一致。

## 核心共享对象（schema）

### Device

| 字段 | 说明 |
|---|---|
| `id`, `name` | 身份与显示名 |
| `os` | v1 正式：`windows` \| `linux` |
| `status` | `online` \| `offline` |
| `last_seen` | Unix 秒 |
| `active_agents` | 会话数量提示（不是猫娘种类数）；PWA 展示为线团数 |

### AgentSession

| 字段 | 说明 |
|---|---|
| `id` | 公开/线会话 id |
| `device_id` | 所属设备 |
| `agent_type` | 线 agent id |
| `status` | `running` \| `idle` \| `waiting_approval` |
| `summary`, `last_activity` | 列表 UX |
| `project_dir` / `project` | 目录分组 |
| `pending_approval` | 可选工具审批结构 |

### SessionMessage

| 字段 | 说明 |
|---|---|
| `id` | 稳定 id，用于合并/去重 |
| `role` | `assistant` \| `user` \| `tool` \| `system` |
| `content` | 文本正文 |
| `type` | `thinking` \| `text` \| `assistant` \| `tool_call` \| `tool_result` \| `error` \| `system` |
| `timestamp` | Unix 秒 |
| `metadata` | 可选对象 |

### AttachmentRef

| 字段 | 说明 |
|---|---|
| `url` | 必需引用 |
| `id`, `name`, `mime`, `size`, `key` | 可选元数据 |

## 消息类型目录

按角色分组。关键流的 payload 形状以实现侧 Go/TS 为准；集成时请对照类型定义。schema 枚举对**类型字符串名**具权威性。

### 设备生命周期

| type | 典型方向 | 作用 |
|---|---|---|
| `device_online` | daemon → server → phone | 设备在线 |
| `device_offline` | server → phone | 设备离线 |
| `device_list` | server → phone | 设备快照 |
| `register_device` | 控制面 | 注册相关 |
| `auth_response` | server → 对端 | 鉴权结果 |

### 会话

| type | 作用 |
|---|---|
| `session_list` | 全量/批量会话快照 |
| `session_update` | 增量会话元数据 |
| `session_message` | 流式或已存回合内容 |

### 提示词生命周期

| type | 作用 |
|---|---|
| `send_prompt` | 手机 → … → daemon：用户提示词（+ 附件） |
| `prompt_status_query` | 查询投递状态 |
| `prompt_not_seen` | 对端无此 id 记录 |
| `prompt_accepted` | 已纳入 daemon 管线 |
| `prompt_committed` | journal 已提交 |
| `prompt_failed` | 可见失败 |
| `prompt_sent` | 客户端发送/ack 信号（outbox 清理） |

**不要**把「WebSocket 写成功」等同于 `prompt_accepted` / 业务成功。

### 历史与订阅

| type | 作用 |
|---|---|
| `subscribe` | 手机请求设备/会话订阅 |
| `subscribe_ack` | 服务端确认订阅就绪 |
| `fetch_history` | 请求原生/服务端历史窗口 |
| `session_history` | 历史载荷响应 |

### 配对

| type | 作用 |
|---|---|
| `pair_request` | 请求配对材料 |
| `pair_confirm` | 确认配对 |

（另有 HTTP 配对 generate/consume API，见 [configuration.zh-CN.md](./configuration.zh-CN.md)。）

### 控制

| type | 作用 |
|---|---|
| `approve` | 批准待处理工具（CLI 支持时） |
| `deny` | 拒绝 |
| `interrupt` | 中断运行中的工作 |
| `heartbeat` | 保活 |
| `error` | 错误信封 |

## 线上明确非目标

- 支持的产品合同中**没有**手机侧 `create_session`（或等价物）。线程须先在 PC 原生 UI/CLI 创建。
- 未经明确产品决策与全栈更新，勿重新引入远程建线程。

## REST 配套 API

交互流量多走 WebSocket。REST（除注明外需手机密钥）：

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/health` | 无鉴权存活 |
| GET | `/api/devices` | 设备列表 |
| POST | `/api/devices/register` | Bootstrap 头 |
| GET | `/api/devices/sessions` | 会话 |
| GET | `/api/messages` | 消息 |
| POST/GET | `/api/attachments`… | 上传 / 下载 |
| POST | `/api/push/subscribe` | 推送 |
| GET | `/api/push/vapid-public-key` | VAPID 公钥 |
| POST | `/api/pair/generate` | 签发配对码 |
| POST | `/api/pair/consume` | 消费配对码 |
| GET | `/ws/phone` | 手机 WS |
| GET | `/ws/daemon` | Daemon WS |

鉴权头细节：[security.zh-CN.md](./security.zh-CN.md)、[configuration.zh-CN.md](./configuration.zh-CN.md)。

## 变更检查清单

线协议变更时：

1. 更新 `protocol/protocol.json`
2. 更新 `server/internal/protocol/types.go` 与 handler/持久化/测试
3. 更新 `pwa/src/types/protocol.ts` 与 stores/API/测试
4. 更新 daemon 消息分发与适配器边界
5. 更新本文档与 README 智能体表（若枚举或产品行为变化）
6. 跑 **server**、**daemon**、**pwa** 全套测试

## 相关文档

- [架构](./architecture.zh-CN.md)
- [开发](./development.zh-CN.md)
- [AGENTS.md](../AGENTS.md)
