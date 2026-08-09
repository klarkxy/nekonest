> [English](./agent-capability-matrix.md) | 简体中文

# Agent harness 能力矩阵

这是 NekoNest 四个现行 harness 的实时规范能力表。手机端必须以 daemon
当前发送的 `session.capabilities` 或 `session_list.start_capabilities` 为准；
字段缺失即 false / unsupported。

Kilo 已退役。协议 1.x 仍解析旧 wire id，确保混合版本节点失败关闭而不是
断开连接，但现行 daemon 与 PWA 不再发现、广告、新建或显示 Kilo 会话，
也绝不修改 Kilo 原生数据。

## 图例

| 单元格 | 含义 |
|---|---|
| **Yes** | 路径健康时已实现并广告 |
| **Probe** | 仅在已安装原生路径探测通过后开启 |
| **Fallback** | 只在所述降级路径可用 |
| **No** | 不广告；手机禁用控件并显示原因 |
| **未实机验证** | 代码/fixture 已具备，但当前安装版本尚未产生所需真实事件 |

`queue` 始终指 NekoNest 持久 FIFO，不冒充 Agent 原生队列。Codex 仍是
唯一保证完整控制路径的 Agent。任意文件浏览、通用 `create_session` 和
模拟 `steer` 继续保持关闭。

## 现行标识与原生存储

| | Claude Code | Codex | Kimi CLI | Grok Build |
|---|---|---|---|---|
| Wire id | `claude_code` | `codex` | `kimi_cli` | `grok_build` |
| 角色 | 兼容续聊 | app-server 健康时全控制 | 兼容续聊 | 兼容续聊 |
| 原生存储 | `~/.claude/projects/<encoded-path>/*.jsonl` | `~/.codex/sessions/…/rollout-*.jsonl` | `~/.kimi-code`（兼容旧 `~/.kimi`） | `~/.grok/sessions` |
| 主要控制路径 | CLI resume；可选 SDK bridge | `codex app-server` | CLI resume；ACP 仅用于新建 | CLI resume / 新建 |
| 健康 `control_mode` | `compatibility` | `app_server` | `compatibility` | `compatibility` |
| 降级 `control_mode` | `compatibility` | `exec_resume` | `compatibility` | `compatibility` |

每个适配器都必须先从原生存储正向确认所有权，再路由会话。空历史不能证明
所有权；stderr 仅作诊断；能识别的 subagent、sidechain 与纯合成记录不得
进入手机主线程列表。

## 会话控制

| 能力 | Claude Code | Codex app-server | Codex exec 降级 | Kimi CLI | Grok Build |
|---|---|---|---|---|---|
| 发现 / 所有权 / 历史 | Yes | Yes | Yes | Yes | Yes |
| `send` + 流式 | Yes | Yes | Fallback | Yes | Yes |
| `interrupt` | Yes | Yes | Fallback | Yes；无原生 cancel 时停止受管进程树 | Yes |
| `approve` / `deny` | Probe，仅 bridge | Yes | No | 当前 CLI resume 路径为 No | 当前 CLI resume 路径为 No |
| 结构化 `user_input` | Probe，仅 bridge | Yes | No | No | 当前 CLI resume 路径为 No |
| `steer` | No | Yes | No | No | No |
| NekoNest 持久 FIFO | 日志可写时 Yes | 日志可写时 Yes | No | 日志可写时 Yes | 日志可写时 Yes |
| 会话级 `spawn` | No；仅设备目录 | Probe | No | No；仅设备目录 | No；仅设备目录 |
| `attachment_mode` | `path_best_effort`；bridge 可探测原生图片 | `native_image_and_file` | `native_image` | `path_best_effort`；ACP 新建当前不广告附件输入 | `path_best_effort`；图片保持 No |

等待状态只能来自 schema 合法且属于当前 generation 的原生正向事件。禁止从
transcript 猜测出可操作审批或提问 UI。权限与提问在可用时绑定当前原生
request；中断还必须回传 daemon 广告的 generation 与 `client_msg_id`，旧轮次
的延迟命令会被拒绝。

## 原生线程新建

`start_capabilities` 是设备级目录。每项显式包含 `available`、`spawn`、
`attachment_mode`，并在可知时填充 `control_path` 与 `control_version`。

| Harness | 探测 / 确认 | 结果 |
|---|---|---|
| Claude Code | starter/bridge 握手；首回合 assistant/result 信号 + 原生存储所有权 | Probe |
| Codex | 健康 app-server；turn started + 原生存储所有权 | Probe |
| Kimi CLI | ACP initialize 与 prompt 成功终态 + 原生存储所有权；既有会话仍走 CLI resume | Probe |
| Grok Build | CLI 新建的首轮正向输出 + 原生存储所有权 | Probe |

目标目录必须属于 daemon 当前从原生会话发现的项目目录并集。生命周期为
`thread_starting → thread_owned | thread_failed | thread_indeterminate`。
`thread_owned` 同时要求首提示词正向确认与原生存储所有权。只要可能跨过原生
边界，未知结果就进入 `thread_indeterminate`，绝不换路径重放。

## 持久队列

所有可靠且已安装的发送路径都可在日志可写时广告 v2 FIFO。状态为
`queued → running → completed`，或 `blocked_failed`、
`blocked_interrupted`、`blocked_indeterminate`。

- prompt 在跨越 Agent 边界前写入日志。
- sealed 模式复用原始 envelope 的完全相同字节。
- 失败、中断或不确定会暂停后续项；当前 prompt 永不重放。
- `resume_prompt_queue` 只继续后续项；不确定 blocker 必须显式确认
  `skip_prompt_queue_item`。
- 重启时未确认的 running 项转为 `blocked_indeterminate`。
- 完成、取消、跳过后清空 payload，仅保留有界去重 tombstone。

## 兼容与不可用原因

协议 1.2 显式发送所有能力布尔值，并为 `send`、`approve`、`deny`、
`interrupt`、`steer`、`queue`、`spawn`、`user_input`、`attachment`
提供稳定 `unavailable_reasons`。新 PWA 只在确认能力表由协议 1.1 或更早
daemon 产生时兼容推定 `send/interrupt=true`；来源未知时失败关闭。

缺少某个 CLI 不会影响其他适配器：现行 Agent 的原生历史仍可浏览，但发送
与其他控制会禁用并给出具体原因。

## 运维检查

用 `nekonest-daemon -doctor` 查看已安装 CLI、控制路径/版本、认证/探测状态、
附件档位与实际能力。实时 wire flag 优先于本文。故障与验收路径见
[troubleshooting.zh-CN.md](./troubleshooting.zh-CN.md) 与
[e2e-smoke.zh-CN.md](./e2e-smoke.zh-CN.md)。
