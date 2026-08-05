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
| `transport_mode` | 全窝统一 `sealed` \| `open`。一窝一种模式；**禁止** sealed→open 自动降级。v0.2 默认 **open**；v1 验收切换后新窝默认 sealed。 |

首帧（daemon 的 `register_device`、手机的 `subscribe`）**必须**带上述字段。Server 在 `auth_response` / `subscribe_ack` 返回协商结果。稳定错误码：`version_mismatch`、`transport_mode_mismatch`、`invalid_envelope`。

### 应用发行版本

应用发行版本与 `protocol_version` 相互独立：只要线协议仍兼容，发行版本不一致只作为诊断信息，不会单独拒绝连接。

| 字段 | 方向 / 语义 |
|---|---|
| `daemon_version` | Daemon 在 `register_device.payload` 上报；Server 在 `auth_response`、`subscribe_ack`、`device_online` 与在线 `Device` 快照返回当前值。缺省表示离线或旧版 Daemon 未上报。 |
| `pwa_version` | PWA 在 `subscribe.payload` 上报构建版本；Server 在 `subscribe_ack` 回显接受的值。 |
| `server_version` | Server 应用发行版本，出现在 `auth_response`、`subscribe_ack`、`/health` 与 `/api/devices`；它**不是**线协议版本。 |
| `refresh_required` | `subscribe_ack` 布尔值；已上报 PWA 版本与 Server 不同时为 true。PWA 提供由用户触发的 Service Worker 更新/重载，不会因该信号自动循环刷新。 |
| `update_required` | `auth_response` 布尔值；已上报 Daemon 版本与 Server 不同时为 true。 |

当前构建使用 SemVer 发行版本。为兼容旧客户端，`pwa_version` 与 `daemon_version` 均为可选；缺失时显示“未知”，不得假定为当前版本。

WebSocket 连接建立后，PWA 以实时 `subscribe_ack.server_version` 为权威值。携带版本的动态 HTTP 响应（`/health` 与 `/api/devices`）使用 `Cache-Control: no-store`，Service Worker 也不会拦截这些请求。页面顶部只比较网页与 Server；Daemon 版本和更新提示归属各自的设备卡片。

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
| `daemon_version` | 当前在线 Daemon 上报的发行版本；离线/未上报时省略 |

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

### SessionCapabilities

| 字段 | 说明 |
|---|---|
| `control_mode` | `app_server` \| `exec_resume` \| `compatibility` |
| `approve`、`deny`、`interrupt`、`steer`、`queue`、`spawn` | 布尔值；缺省为 false。只有已安装并探测通过的原生 starter、且目录获准时 `spawn` 才可为 true；它不代表任何其他控制能力。 |
| `attachment_mode` | `native_image_and_file` \| `native_image` \| `path_best_effort` \| `unsupported` |

### `session_list` 开线程能力目录

`session_list.payload.start_capabilities` 是可选的设备级目录。缺省时禁用设备级新建；在 minor 版本迁移期间，旧 daemon 仅可通过会话中明确的 `capabilities.spawn=true` 保留 Codex 新建入口。每项含 `agent_type`、`available`、`spawn`，以及可选的展示 `reason`、`control_path` / `control_version`。仅当 `available=true` 且 `spawn=true` 时，PWA 才可提供本地草稿；目录必须来自 daemon 当前原生发现项目目录的并集，绝不可输入任意路径。

### 原生开线程载荷

`start_thread.payload` 使用 `agent_type`、`operation_id`、`project_dir`、`prompt`；`cwd` 与 `initial_prompt` 仍是可选的遗留别名。该 prompt 是原生线程首条提示词，不是先由手机创建的会话。sealed 模式会用设备目录密钥加密整个正文；relay 只能看到路由元数据与稳定 operation id。发送前 PWA 必须把本地草稿与该 operation id 持久绑定；刷新后不得另造 operation 或重试未决新建。`thread_*` 载荷为兼容保留 `operation_id`、`session_id`、`thread_id`、`error`、`message`；sealed 模式下这些结果载荷也用设备目录密钥加密，外层仅保留状态、operation id 与原生 session id 作为路由元数据。可见的路由元数据不等于已认证的业务结果：结果密文缺失或认证失败时，PWA 必须本地降为 `thread_indeterminate`，绝不能采信外层 `thread_owned` 或 `thread_failed`。`thread_owned` 必须同时具备原生 store 所有权与首条提示词正向确认，因此其 `prompt_accepted` 必须为 true；任一证据缺失都须使用 `thread_indeterminate` 并保留本地草稿。

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

### 控制与生命周期（v1）

| type | 作用 |
|---|---|
| `approve` / `deny` | 工具审批（有能力时为 Codex app-server） |
| `interrupt` | 停止运行中的工作 |
| `steer` | 回合中修正（Codex） |
| `start_thread` / `thread_*` | agent 范围的手机本地草稿：在获准的已发现目录用首条提示词原生开线程 |
| `pair_*` / `key_package` / `phone_revoked` | 配对与 E2E 密钥分发 |
| `attention_event` | 通用、适合密封模式推送的事件类别 |

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

- 支持的产品合同中**没有**通用手机侧 `create_session`（或等价物）或窝侧幽灵线程。
- 唯一允许的手机新建路径是 agent 范围的 `start_thread`：先创建手机本地草稿；仅当所选 agent 的 starter 已安装/探测通过且宣告 `spawn=true` 时，才将首条提示词原生建线程。目录必须来自 daemon **当前原生发现项目目录并集**。
- 生命周期为 `thread_starting → thread_owned | thread_failed | thread_indeterminate`；仅在首条提示词得到正向确认、且所选 agent 原生 store 明确认领后才发送 `thread_owned`，否则报 `thread_indeterminate`。
- 不得发明永久窝侧会话行。

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
