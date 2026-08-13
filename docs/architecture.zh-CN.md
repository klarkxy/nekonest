> [English](./architecture.md) | 简体中文

# 架构

NekoNest 三个运行时如何协作，使手机能够续写编码智能体线程。贡献者不变量见 [AGENTS.md](../AGENTS.md)。

## 总览

```text
┌─────────────┐       HTTPS / WSS       ┌──────────────────┐
│  手机 PWA   │ ◄─────────────────────► │  VPS Server      │
│ Vue3+Pinia  │                         │  Go + SQLite     │
└─────────────┘                         └────────┬─────────┘
                                                 │ WSS
                                                 │ 由 PC 出站
                                        ┌────────▼─────────┐
                                        │ Windows Daemon   │
                                        │ 发现 / 历史      │
                                        │ journal / 执行   │
                                        └────────┬─────────┘
                                                 │ 原生存储 + CLI
                    ┌────────────┬───────────┬───┴────────┬────────────┐
                    │Claude Code │   Codex   │ Kimi CLI   │ Grok Build │
                    └────────────┴───────────┴────────────┴────────────┘
```

| 层 | 模块 | 职责 |
|---|---|---|
| PWA | `pwa/` | 鉴权 UI、配对、会话树、草稿、outbox、Markdown、可选推送 |
| Server | `server/` | 手机/daemon 鉴权、配对、WS 中转、SQLite、附件、Web Push |
| Daemon | `daemon/` | 出站 WS、适配器发现/历史、提示词 journal、无头 CLI 控制 |
| 协议 | `protocol/protocol.json` | 语言无关信封/schema（Go/TS 手动镜像） |

家用 PC **没有入站**连接；由 daemon 拨号 VPS。

## 会话呈现模型

会话是**派生视图**，不会改写原生库：

```text
目录（项目路径） → 智能体类型 → 线程（原生 session id）
```

- 无法识别工作目录的线程归入「**未分类**」。
- 某目录下无某类智能体线程时不显示该分组。
- 现行线协议 agent id：`claude_code`、`codex`、`kimi_cli`、`grok_build`。
- 协议 1.x 只为失败关闭的混合版本兼容解析已退役的 `kilo`；现行目录会过滤它。

分 harness **现行**能力矩阵（控制标志、附件、建线探测、现行 vs v1）：
[agent-capability-matrix.zh-CN.md](./agent-capability-matrix.zh-CN.md)。

## 发现与所有权

1. Daemon 在启动/强制事件，以及每轮完成 30 秒后对各已注册适配器做 **Discover**；慢扫描不会积累 ticker 追赶。
2. 适配器仅在对原生存储有**正向所有权**时认领会话（空转录 ≠ 所有权）。
3. 子代理、sidechain、primer、纯系统/合成记录从手机可见主线程列表中**排除**。
4. 缺少 CLI 是**非致命**的不可用适配器；其他智能体继续工作。
5. 手机目录仅含最近 7 天有活动的线程，以及有正向信号的运行/等待线程。旧原生记录不删除；只有旧线程的项目会离开当前目录并集，主机端再次活动后恢复。
6. 发现结果经 server 以 session list/update 等形式推到手机。文件型适配器按路径/大小/mtime 复用小型元数据，只重解析变化文件；发现缓存不保存 transcript 正文。

原生存储仍是权威；VPS 缓存用于中转与乐园侧消息持久化，不能替代 agent 数据库。

## 提示词投递路径

```text
PWA 输入框
  → client_msg_id + 可选附件
  → 手机 WS send_prompt
  → server 校验、可能持久化、转发 daemon
  → daemon 提示词 journal（dispatching → accepted → committed | 失败路径）
  → agentexec 无头 CLI（resume/session 参数 + 附件接线）
  → 流式 session_message / 状态事件回到手机
```

### 投递状态（概念）

| 状态 / 信号 | 含义 |
|---|---|
| 传输 OK | 帧到达对端；**不是** agent 已接受的证明 |
| `prompt_accepted` | Daemon 已把提示词纳入执行管线 |
| `prompt_committed` | journal 意义上达到已提交点 |
| `prompt_failed` / 错误 | 可见失败，应展示给用户 |
| `prompt_sent`（PWA） | 仅在恰当 ack 时清理 outbox（见 PWA 规则） |

### 至多一次与重连

- 稳定 **`client_msg_id`** 把手机 outbox 与 server/daemon 处理绑定。
- 重连**复用**同一 id，不为同一用户动作静默换新 id。
- journal 无法判断是否已接受时 **fail-close**（宁可报错，不可双跑）。
- PWA outbox 上限 40；满则阻塞新发送直至 ack 消化。

## 历史与流式合并

打开线程时：

1. 客户端可能 `fetch_history` / 收到 `session_history`。
2. Daemon 从**原生**适配器拉历史（默认窗口约 40 条，内容常截断约 4k rune）。
3. Server 持久化的乐园侧消息与实时 `session_message` 在 PWA 用**稳定消息 id** 合并。
4. 合并只丢弃应丢的 pending outbox / 乐观本地消息，避免重放重复。
5. CLI **stderr 仅诊断**，不进助手气泡。

空会话可在用户再次进入时重新同步历史。

## 订阅

手机客户端须在把会话流量视为就绪前，完成对活动设备/会话上下文的 **subscribe** 并得到 **subscribe_ack**：

```text
subscribe → subscribe_ack → 会话流量 / 提示词
```

路由变更与组件卸载必须移除 WS 处理器，避免重复处理（PWA 不变量）。

## 附件路径

```text
手机 multipart 上传 → server data/attachments
  → 提示词引用附件 id/url
  → daemon 下载到每次运行的临时目录
  → 按 agent 接线（原生 --file / 图片参数 / 提示词路径）
```

限额与 MIME：[configuration.zh-CN.md](./configuration.zh-CN.md)。

## 审批与控制

线类型含 `approve`、`deny`、`interrupt`。真实行为取决于 agent **非交互** CLI 能否承载审批：

- 若 CLI 无法承载，手机不得伪装成功——用户回 PC 终端。
- 中断/停止使用进程控制（Windows 上 Job Object + kill-on-job-close）拆除子进程树。

## 并发与连接生命周期（高层）

| 关注点 | 做法 |
|---|---|
| WebSocket 写 | 串行写；每连接一读一写 |
| 注册表锁 | 不在慢适配器/网络/文件系统调用期间持锁 |
| Daemon 重连 | 代次检查，旧连接不能覆盖新状态 |
| 配置重载 | 不可变配置快照；凭据至进程重启前固定 |
| 实例锁 | 每个配置路径一个 daemon 进程 |

## 代码仓库地图

```text
protocol/     JSON schema（手动）
relaycore/    可复用的单 Nest 数据面 Engine 与公开端口
server/       VPS 模块（github.com/nekonest/server）
daemon/       Windows daemon 模块（github.com/nekonest/daemon）
  internal/adapters/   各 agent 原生存储 + 归一化
  internal/agentexec/  CLI 调用 / 进程
pwa/          Vue 3 + TS + Pinia
docs/         运维与贡献者文档
```

Go module 路径为 `github.com/klarkxy/nekonest/relaycore`、`github.com/nekonest/server` 与 `github.com/nekonest/daemon`。后两个应用 module 路径保持稳定；勿仅因 GitHub 仓库为 `klarkxy/nekonest` 而“修正”。

## 协议 1.2+ 控制面

- Daemon 是能力表生产者。Server 在实时转发与 open 模式重建快照中保留生产者协议版本，不把目录重盖章成 Server 自身版本。
- 每个回合绑定唯一控制 generation、公开 session id、`client_msg_id` 与原生 request id；旧 generation 的 accepted/success/failure/interrupted/indeterminate 事件全部拒绝；中断还必须在同一 session 派发锁内回传当前广告的 generation 与 `client_msg_id`。
- 每条可靠且已安装的发送路径都可在越过原生边界前进入 NekoNest queue v2。重启后未知的 running 工作变为 `blocked_indeterminate`，绝不重放。
- 原生开线程保持 `thread_starting`，直到首提示词成功与原生 store 所有权均为正；PWA 不再本地超时制造终态。

## 相关文档

- [智能体能力矩阵](./agent-capability-matrix.zh-CN.md)
- [协议概览](./protocol.zh-CN.md)
- [安全模型](./security.zh-CN.md)
- [配置](./configuration.zh-CN.md)
- [开发](./development.zh-CN.md)
- [AGENTS.md](../AGENTS.md)
