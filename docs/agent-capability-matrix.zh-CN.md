> [English](./agent-capability-matrix.md) | 简体中文

# 智能体 Harness 能力矩阵

本文是 NekoNest **现行**支持的每个 harness（agent 适配器）能力对照表：手机侧能做什么、线协议如何广告这些能力，以及与冻结的 [v1.0.0 产品合同](./v1-product.zh-CN.md) 有何差异。

**冲突时的优先级：**

1. 现行线协议广告（`session.capabilities`、`session_list.start_capabilities`）
2. Daemon 实现（`daemon/internal/adapters`、`daemon/internal/agentexec`，以及 `daemon/cmd/daemon/main.go` 中的能力盖章）
3. 本文（运维 / 贡献者摘要）
4. [v1-product.zh-CN.md](./v1-product.zh-CN.md) §7.3（目标发版门槛，未必已全部落地）
5. [README.zh-CN.md](../README.zh-CN.md) 中的短表（仅概览）

PWA 控件**必须**按已广告标志开关。缺省字段 = **false / 不支持**。不得暗示比 daemon 为该会话或设备发布的更强控制或附件档位。

---

## 1. 角色与控制模式

| 角色 | 含义 |
|---|---|
| **全控制（Full-control）** | 健康时手机可驱动完整回合面：发送/流式、中断、批准/拒绝、steer、完整原生附件，以及（探测通过时）原生建线。**仅 Codex。** |
| **兼容续写（Compatibility-resume）** | 发现 + 所有权 + 历史 + 发送/流式 + 中断。附件与 `start_thread` 仅以广告为准。**不**承诺审批 / steer / 队列。 |

| `control_mode` | 何时广告 | 手机侧预期 |
|---|---|---|
| `app_server` | 仅 Codex，且 `codex app-server` initialize 成功 | 全控制标志可为 true |
| `exec_resume` | Codex 默认 / app-server 不健康时的降级 | 发送、历史、中断；偏图片附件；无 approve/steer/spawn |
| `compatibility` | Claude Code、Kilo、Kimi CLI、Grok Build | 续写/发送路径；无全控制承诺 |

---

## 2. 图例

| 单元格 | 含义 |
|---|---|
| **Yes** | 已实现，并在路径可用时广告 |
| **Probe** | 仅当原生 starter / 健康探测成功时为 Yes |
| **Fallback** | 降级路径上可用 |
| **No** | 不广告；手机须禁用控件并给出具体原因 |
| **Detect only** | 可能出现在状态/历史启发式中；**不是**手机控制通路 |
| **Best-effort** | 已物化为本地文件；agent 沙箱/CLI 仍可能拒绝读取 |

五个 harness 在 CLI/仓库可用时的共同能力：

- 从**原生**仓库发现主线程
- 路由前做正性所有权检查
- 导入历史并用稳定 id 合并
- 发送提示词 + 流式归一化助手输出
- 中断 daemon 拥有的运行中进程树（有活动运行时）
- 在适配器实现过滤处排除子代理 / sidechain / 仅系统记录

除非某行另有说明，共同**不具备**：

- 现行从不广告 `queue`
- 禁止任意文件系统浏览与泛化 `create_session`
- `start_thread` 只能指向 daemon **当前**原生发现项目目录并集
- `thread_owned` 需要首条提示词确认 **且** 原生仓库所有权

---

## 3. 现行矩阵（v0.2 已交付行为）

线协议 id：`claude_code`、`codex`、`kilo`、`kimi_cli`、`grok_build`。

### 3.1 身份与仓库

| | Claude Code | Codex | Kilo | Kimi CLI | Grok Build |
|---|---|---|---|---|---|
| 线协议 id | `claude_code` | `codex` | `kilo` | `kimi_cli` | `grok_build` |
| 产品名 | Claude Code | Codex | Kilo | Kimi CLI | Grok Build |
| 角色 | 兼容续写 | 健康时全控制；否则 exec-resume 降级 | 兼容续写 | 兼容续写 | 兼容续写 |
| 原生仓库 | `~/.claude/projects/<encoded-path>/*.jsonl` | `~/.codex/sessions/…/rollout-*.jsonl` | Kilo/OpenCode SQLite（OS 数据目录下的 `kilo.db`） | `~/.kimi-code`（旧布局 `~/.kimi`） | `~/.grok/sessions` |
| 续写/发送 CLI | `claude --resume <id> -p … --output-format stream-json` | 健康：app-server turn API。降级：`codex exec … resume <id> -- <prompt>` | `kilo run --session <id> …`（JSON） | `kimi --session <id> -p … --output-format stream-json`（旧版可能需 `--print`） | `grok --resume <id> -p … --output-format streaming-json --permission-mode auto` |
| 默认广告 `control_mode` | `compatibility` | `exec_resume`，app-server 健康后 → `app_server` | `compatibility` | `compatibility` | `compatibility` |

### 3.2 会话控件（每会话 `capabilities`）

下表是 daemon **盖给手机**的值。Codex 全控制标志仅在 `AppServerHealthy()` 为真时抬升（`daemon/cmd/daemon/main.go`）。

| 能力 | Claude Code | Codex（app-server 健康） | Codex（exec-resume / 不健康） | Kilo | Kimi CLI | Grok Build |
|---|---|---|---|---|---|---|
| 发现 / 列表 | Yes | Yes | Yes | Yes | Yes | Yes |
| 所有权门槛 | Yes | Yes | Yes | Yes | Yes | Yes |
| 历史 | Yes | Yes | Yes | Yes | Yes | Yes |
| 发送 + 流式 | Yes | Yes | Yes | Yes | Yes | Yes |
| `interrupt` | Yes | Yes | Yes | Yes | Yes | Yes |
| `approve` / `deny` | **No**（见注） | **Yes** | **No** | **No** | **No** | **No** |
| `steer` | **No** | **Yes** | **No** | **No** | **No** | **No** |
| `queue` | **No** | **No** | **No** | **No** | **No** | **No** |
| 会话级 `spawn` | **No**（仅设备目录） | 健康时 **Yes** | **No** | **No**（仅设备目录） | **No**（仅设备目录） | **No**（仅设备目录） |
| 广告的 `attachment_mode` | `path_best_effort` | `native_image_and_file` | `native_image` | `path_best_effort` | `path_best_effort` | `path_best_effort` |
| 状态 `waiting_approval` | 仅检测（历史启发式；不广告手机审批） | 来自 app-server 正性信号 | 不伪造审批 UI | No | No | No |
| 状态 `waiting_user` | No | 来自 app-server 正性信号 | No | No | No | No |

**说明**

- Claude / Kilo 的 `Approve`/`Deny` 仅在 stdin 仍打开时尽力写 “y/n”。print/resume 会关闭 stdin，故手机侧广告为 **false**；若调用会返回 `approval_unavailable`。
- Kimi / Grok 的 approve/deny 恒为 `approval_unavailable`。
- Claude 可能因转写形态把列表状态标成 `waiting_approval`，**不**等于手机 Approve/Deny 可用。此类 CLI 请回 PC 终端处理。
- Codex exec-resume 可通过 `--image` 传图片、`--add-dir` 授权目录；非图片文件在该路径上不是一等原生文件回合输入（故为 `native_image`，不是 `native_image_and_file`）。

### 3.3 设备级建线目录（`start_capabilities`）

发布于 `session_list.payload.start_capabilities`。每项字段：

| 字段 | 含义 |
|---|---|
| `agent_type` | 线协议 id |
| `available` | 找到原生建线路径且探测成功 |
| `spawn` | 探测成功时当前与 `available` 相同 |
| `reason` | 不可用时的具体文案 |
| `control_path` / `control_version` | 可选；现行可能省略 |

| Harness | 建线机制（探测） | 探测 OK 时 `spawn` | 备注 |
|---|---|---|---|
| Claude Code | CLI help 需含 `--session-id` + stream-json；建线用 `--session-id` + 首条 `-p` | Probe | 仍须在 `~/.claude/projects` 确认所有权 |
| Codex | 需要**健康 app-server**握手（不是裸 CLI help） | Probe | `thread/start` 再首回合；仅有 exec-resume **不**广告 spawn |
| Kilo | CLI help `acp` + ACP 建线探测（`kilo acp`） | Probe | 建线走 ACP；后续回合仍走 resume/`kilo run` |
| Kimi CLI | ACP 建线探测 | Probe | 优先现代仓库；仍发现旧 `.kimi` |
| Grok Build | CLI help `--session-id` + `streaming-json`；建线用 `--session-id` | Probe | 非交互 `--permission-mode auto` |

所有 harness 生命周期：手机本地草稿 → `start_thread` → `thread_starting` → `thread_owned` | `thread_failed` | `thread_indeterminate`。仅在 `thread_owned` 后导航。

### 3.4 附件接线（实现 vs 广告）

共性：手机上传 → server blob → daemon 每次运行临时目录（open 路径最多 **5** 个文件、每个 **4 MB**；见 [configuration.zh-CN.md](./configuration.zh-CN.md)）。

| Harness | 广告模式 | 文件如何到达 agent | 实际限制 |
|---|---|---|---|
| Claude Code | `path_best_effort` | 对附件父目录 `--add-dir`；路径出现在 NekoNest 提示词后缀。**不用** Claude 远程 `--file` id | 需 agent 能 Read 授权目录；沙箱仍可能拦截 |
| Codex app-server | `native_image_and_file` | turn input 携带原生图片与文件部件 | 全控制路径 |
| Codex exec-resume | `native_image` | 目录 `--add-dir` + 图片 MIME/扩展名 `--image`；其他文件主要靠提示词路径 | 弱于 app-server |
| Kilo | `path_best_effort` | `kilo run` 上原生重复 `--file <path>` | 实现强于广告枚举名；在标志抬升前 UI 仍不得宣称 `native_image_and_file` |
| Kimi CLI | `path_best_effort` | 仅提示词路径后缀；argv 构造忽略附件切片 | 取决于 Kimi 文件权限/沙箱 |
| Grok Build | `path_best_effort` | 仅提示词路径后缀；argv 构造忽略附件切片 | 同类限制；无头 auto 权限模式 |

### 3.5 过滤与卫生（全部 harness）

| 规则 | 要求 |
|---|---|
| 子代理 / sidechain | 可检测时从手机主线程列表排除 |
| 空历史 | 不能证明所有权 |
| 缺少 CLI | 非致命；其他 harness 继续工作 |
| stderr | 仅诊断；永不当助手正文 |
| 能力诚实 | 缺省 = false/不支持；不得伪造 Approve/Steer/Start 成功 |

---

## 4. 分 harness 卡片

### 4.1 Codex（`codex`）— 全控制 harness

| 领域 | 现行行为 |
|---|---|
| 健康路径 | `codex app-server` JSON-RPC：initialize、thread/turn、审批、中断、steer、附件 |
| 降级路径 | `codex exec resume` 发送/流式/中断 + `native_image` 附件 |
| 能力盖章 | 健康时：`control_mode=app_server`，`approve/deny/interrupt/steer/spawn=true`，`attachment_mode=native_image_and_file` |
| 建线 | 仅 app-server 健康时设备目录 + 会话级 `spawn` |
| 状态 | `waiting_approval` / `waiting_user` 仅来自 app-server 正性信号（发现上叠加 overlay） |
| 基线 | 开发冒烟钉 **codex-cli 0.144.1** 面；方法名可能漂移——用 `nekonest-daemon -doctor` |
| v1 目标 | 角色相同；密封默认与诚实降级仍是发版要求 |

### 4.2 Claude Code（`claude_code`）

| 领域 | 现行行为 |
|---|---|
| 控制 | 仅兼容续写 |
| 附件 | 授权临时目录（`--add-dir`）；路径进提示词 |
| 建线 | 探测 `--session-id`；UUID + 首提示词；在 projects 仓库确认 |
| 审批 | 不广告；print 模式非交互 |
| 特别 | 可能出现基于转写的等待状态；勿当作手机审批桥 |

### 4.3 Kilo（`kilo`）

| 领域 | 现行行为 |
|---|---|
| 控制 | 经 `kilo run --session` 兼容续写 |
| 附件 | 原生 `--file` 接线；现行广告 `path_best_effort` |
| 建线 | ACP（`kilo acp`）探测 + 原生创建；后续回合仍走 resume |
| 审批 | 不广告 |

### 4.4 Kimi CLI（`kimi_cli`）

| 领域 | 现行行为 |
|---|---|
| 控制 | 经 `kimi --session` 兼容续写 |
| 仓库 | 优先 `~/.kimi-code`；仍读旧 `~/.kimi` |
| 附件 | 仅提示词路径 |
| 建线 | CLI 支持时 ACP 探测 |
| 审批 / steer / 队列 | 无 |

### 4.5 Grok Build（`grok_build`）

| 领域 | 现行行为 |
|---|---|
| 控制 | 经 `grok --resume` 兼容续写 |
| 安全默认 | `--permission-mode auto`、streaming JSON、非交互 |
| 附件 | 仅提示词路径 |
| 建线 | `--session-id` 探测 + 确定性新 id；在 `~/.grok/sessions` 确认 |
| 审批 / steer / 队列 | 无 |

---

## 5. 现行 vs v1.0.0 目标

| 主题 | 现行（以本矩阵为准） | v1 合同（[v1-product.zh-CN.md](./v1-product.zh-CN.md)） |
|---|---|---|
| Codex 角色 | app-server 健康时全控制 | 相同；降级须保持诚实 |
| 其余四个 | 兼容续写 + 探测建线 | 对 approve/steer/queue 同样不承诺 |
| 传输默认 | open（密封为可选预览） | 新 nest 默认密封 |
| `queue` | 不广告 | Codex 在能保证顺序时为 SHOULD |
| 主机 OS | Windows + Linux | 相同；macOS 更晚 |
| 扩展 agent | 无硬性要求 | OpenCode / Gemini / Cursor 等更晚，非 v1 门槛 |
| Kilo 附件枚举 | 已用 `--file` 仍广告 `path_best_effort` | 必须诚实；抬升 `native_image`（或更清晰档位）是实现跟进，不是手机猜测 |

朝 v1.0.0 标签推进时，**范围**以 v1 合同为准。运维或排查运行中 nest 时，以**本矩阵 + 现行标志**为准。

---

## 6. 线协议与 UI 映射

| 面 | 字段 | 消费规则 |
|---|---|---|
| `AgentSession.capabilities` | `control_mode`、布尔项、`attachment_mode` | 按打开的线程开关作曲器控件 |
| `session_list.start_capabilities` | 每 agent 的 `available`/`spawn`/`reason` | 按设备开关「新建线程」草稿 |
| 消息 | `approve`、`deny`、`interrupt`、`steer`、`start_thread`、`send_prompt` | Server 中继；daemon 执行真实支持 |
| PWA | `pwa/src/types/protocol.ts`、session store、SessionDetail 控件 | 标志为 false 时禁用并解释 |

Schema：[protocol.zh-CN.md](./protocol.zh-CN.md)、`protocol/protocol.json`。

---

## 7. 验证清单

证明某 harness 行仍准确的最小证据：

1. 该 agent 的 `daemon/internal/adapters` 与 `daemon/internal/agentexec` 单测/夹具。
2. `DefaultCapabilities` / 发现盖章测试（Codex 在健康前不得宣称 app-server 标志）。
3. 建线能力目录测试：CLI 缺失时有不可用原因；仅探测后 `spawn=true`。
4. 运维：`nekonest-daemon -doctor` 的适配器与 Codex app-server 行。
5. 手工：[e2e-smoke.zh-CN.md](./e2e-smoke.zh-CN.md) A/B 节，针对主机已安装的 agent。

---

## 8. 维护规则

变更某个 harness 时：

1. 先改 adapter + agentexec + 测试。
2. 若线形状变化，保持 `protocol/protocol.json`、server 类型与 `pwa/src/types/protocol.ts` 一致。
3. **本文与中文镜像一起**更新。
4. 若一行摘要漂移，同步 [README.md](../README.md) / [README.zh-CN.md](../README.zh-CN.md) 短表。
5. 若 v1 发版门槛变化，先改 [v1-product.md](./v1-product.md)（+ zh），再改本矩阵 §5。
6. 不要文档化手机在现行 `capabilities` / `start_capabilities` 中看不到的能力。

## 相关文档

- [架构](./architecture.zh-CN.md)
- [协议](./protocol.zh-CN.md)
- [v1 产品合同](./v1-product.zh-CN.md)
- [端到端冒烟](./e2e-smoke.zh-CN.md)
- [故障排查](./troubleshooting.zh-CN.md)
- [AGENTS.md](../AGENTS.md)（英文）
