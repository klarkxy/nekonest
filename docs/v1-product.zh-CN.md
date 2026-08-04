> [English](./v1-product.md) | 简体中文

# NekoNest v1.0.0 — 产品全功能合同

**状态：** 首个完整版本的**冻结**目标产品合同  
**读者：** 维护者、贡献者、编码 Agent  
**与 v0.1 的关系：** v0.1.0 是为快速上线裁切的*发布切片*。除本文外，`docs/*.md`、`README.md` 中的边界说明，以及 `docs/archive/`，描述的是该切片。**仅供参考。** 本文定义 v1.0.0 发布时产品必须达到的完整形态。

v1 推进期间若实现与合同冲突，应**有意识地修订合同**，而不是默默缩回 v0.1 限制。任何对冻结决策的修改，必须先同步更新**两种语言**镜像，再继续实现。

---

## 1. 一句话产品

NekoNest 是**自托管的猫娘窝**：在不向家用主机开入站端口、不取代各 agent 原生会话库的前提下，用**手机优先客户端**继续、驾驭并完成家中机器上**真实的本地编程 agent 线程**。**Codex 是 v1 唯一全控制 agent**；Claude Code、Kilo、Kimi CLI、Grok Build 以**兼容续聊**适配器交付。

## 2. 问题与要完成的工作

| 工作 | 成功标准 |
|---|---|
| **离开工位仍能续聊** | 打开手机 → 看到主机上已有线程 → 读历史 → 发下一句 → 看流式输出 |
| **不断轮、不双发** | 重连、杀进程、弱网：prompt 至多一次；用户看到诚实的投递状态 |
| **不必走回主机才能解卡** | Codex 等你（权限、提问、出错后空闲）时手机有通知且能操作 |
| **多 agent 日常** | 一个窝呈现 Codex（全控制）以及 Claude Code、Kilo、Kimi、Grok（兼容续聊）；缺一个 CLI 不拖死全家 |
| **信任这只窝** | 运维自有 VPS；**默认密封 E2E** 使中继读不到应用正文；开放中继仅管理员可开 |
| **家宽现实** | 主机只出站；CGNAT / 无公网 IP / 不能端口映射也能用 |

明确不做：

- 不是远程桌面或完整 IDE  
- 不是把活搬到云上跑的托管 coding agent  
- 不是面向陌生多人的多租户 SaaS 控制面  
- 主 UX 不是 tmux/xterm 替代品（聊天优先；终端仅可作次要逃生口）  
- 不是 v1 对每个支持的 agent 给出同等全控制承诺  

## 3. 产品原则（不可妥协的 DNA）

1. **原生 store 为权威**（发现与 transcript，以及 Codex 开线程后的所有权）。窝侧 SQLite 是中继与耐久，不是永久分叉的第二套 agent 库。  
2. **Daemon 主动出站**连窝。核心产品不要求家用主机入站端口。  
3. **呈现为派生视图：** `目录 → agent → 线程`。无目录进 **未分类**。不为建树改写源会话行。  
4. **投递 ≠ 传输：** WebSocket 写成功 ≠ agent 已接受。保留 accepted / committed / failed / not_seen / indeterminate 及手机侧可见派生状态。  
5. **日志歧义时 fail-closed**（宁可显式失败，不可双跑）。  
6. **不可信内容渲染：** agent/远端 Markdown 必须消毒。  
7. **不假装能力：** agent 做不了审批/steer/附件/开线程时，UI 必须说明，禁止空操作亮绿灯。能力标志缺省为 false/unsupported。  
8. **单个 agent 缺失对其余非致命。**  
9. **默认密封：** 新窝使用 E2E 密封传输；开放中继须管理员显式配置；同一窝密封/开放客户端不得混用；禁止自动从密封降级为开放。  
10. **仅 Codex 全控制：** approve/deny、steer、完整附件、手机 `start_thread` 是 Codex app-server 承诺。其余 agent 仅诚实宣告兼容续聊能力。  

## 4. 竞品定位（为何 v1 长这样）

| 类型 | 代表 | NekoNest 立场 |
|---|---|---|
| 单/双 agent 手机遥控 | Happy、Remodex | 在 Codex 上对齐 **推送、审批、steer、安装体验、信任**；经兼容适配器保持多 agent + 自托管优先 |
| 多 agent 中继 + IM | Legax、botmux | 对齐 **多机 + 原生审批回调**（Codex）；主客户端仍是聊天 PWA；IM 为 v1 后扩展 |
| 手机终端 / tmux | Pane Remote、run-kit、QuickTUI、chat2ide | **不**把主 UX 改成裸 PTY；保持结构化聊天 + 控制 |
| 会话时间线归档 | Longhouse | 借鉴 **搜索 / 多机时间线**；整体更轻 |

**v1 楔子：** 纯出站家用主机 + Codex 全控制手机闭环 + 多 agent 兼容续聊 + 诚实投递 + 默认密封中继。

## 5. 角色与拓扑

```text
┌──────────────┐     HTTPS/WSS      ┌─────────────────┐
│ 手机客户端   │ ◄────────────────► │ 窝服务器        │
│ （PWA 优先） │                    │ 鉴权 · 中继     │
└──────────────┘                    │ 耐久 · 推送     │
                                    │ 附件            │
                                    └────────┬────────┘
                                             │ WSS（各主机出站）
                    ┌────────────────────────┼────────────────────────┐
                    ▼                        ▼                        ▼
             ┌────────────┐           ┌────────────┐           ┌────────────┐
             │ Daemon A   │           │ Daemon B   │           │ Daemon …   │
             │ Windows    │           │ Linux      │           │            │
             └─────┬──────┘           └─────┬──────┘           └─────┬──────┘
                   │                        │                        │
            原生 CLI + store           原生 CLI + store         原生 CLI + store
```

| 角色 | v1 职责 |
|---|---|
| **手机客户端** | 配对、浏览、聊天、控制、通知、草稿/发件箱；独立身份/令牌/密钥 |
| **窝服务器** | 鉴权、配对、多设备注册、手机身份/授权、WS 分发、密封耐久、附件（密封模式下密文）、推送 |
| **主机 daemon** | 发现、历史、日志、执行、Codex app-server 桥、进程控制、健康检查、E2E 密钥 |
| **Agent 适配器** | 所有权、列表/历史归一、resume/send、流解析、能力标志 |

**正式主机 OS（MUST）：** Windows 与 Linux（产物：`windows-amd64`、`linux-amd64`、`linux-arm64`；Linux smoke 基线 Ubuntu 22.04+ 与 Debian 12+；XDG 路径与 `systemd --user`）。**macOS 为 LATER。**

## 6. 发布定义：何谓 v1.0.0

v1.0.0 = **单人运维自托管窝**可日常在路上使用的功能完备版本：含 **Codex 全控制**、**四个兼容续聊适配器**、**Windows 与 Linux**、**默认密封 E2E**。

### 6.1 必须交付（发版阻塞）

§7 中标注 **MUST** 的全部项。

### 6.2 应当交付（默认合入；省略须在 CHANGELOG 写明例外）

§7 中标注 **SHOULD** 的项。

### 6.3 明确不进 v1.0.0

见 §11。不得因此拖住发版。

### 6.4 质量门槛

- `server/`、`daemon/`、`pwa/` 聚焦测试 + 全量套件通过  
- 任何线协议变更完成跨层核对清单  
- 文档化部署路径：VPS + Windows + Linux（amd64/arm64 产物；Ubuntu/Debian smoke）  
- e2e-smoke 对齐本合同（不再按 v0.1 妥协写死）  
- 支持 OS 上无已知「prompt 双发」或「interrupt 后残留 agent 进程树」  
- 安全文档与真实信任模型一致（默认密封、开放仅管理员、元数据可见性、密钥丢失边界）  
- Codex 审批、steer、中断、开线程、图片与普通文件旅程在钉选的 app-server 基线上通过  

## 7. 功能目录

优先级：

- **MUST** — v1.0.0 标签硬性要求  
- **SHOULD** — 默认应进 v1.0.0；省略须维护者书面例外  
- **MAY** — 成本低可做；非阻塞  
- **LATER** — 预留设计，不要求进 v1.0.0  

### 7.0 冻结决策（规范性摘要）

| 领域 | v1 决策 |
|---|---|
| 传输 | E2E 密封为发版 **MUST**，且为新窝**默认**。密封模式下 VPS 仅存密文。 |
| 开放中继 | 保留但**默认关闭**。仅服务器管理员可启用。一窝一固定模式；密封/开放客户端不可混用；**禁止自动降级**。 |
| 密码学 | X25519 密钥协商、Ed25519 身份签名、HKDF-SHA-256 派生、AES-256-GCM 载荷加密。仅用成熟实现与跨语言向量。 |
| 配对 | QR 信任锚含中继 URL、设备 ID、daemon 公钥/指纹、签名 transcript、一次性码、过期、协议 major。六位码仅为回退，且须人工比对指纹。 |
| 多手机 | 每部手机有**独立**身份、令牌与配对包装密钥。Daemon 为每部授权手机包装设备/会话密钥。 |
| 撤销 | 立即撤销令牌、WS、推送、授权与未来密钥包；轮换到新密钥 epoch。已下载/解密的历史无法远程抹除。 |
| 密钥恢复 | 手机私钥**永不**备份到 VPS。丢失/清空 PWA 状态 → 以**新**身份重新配对。从原生 agent store 重建可得历史；无旧密钥则 VPS 侧旧明文/密文不可恢复。 |
| VPS 元数据 | VPS **可见**设备 ID、原生会话 ID、时间戳、client message ID、事件类别、大小与连接元数据。Agent 类型、项目路径、标题、prompt/回复/工具正文、审批细节、附件字节/元数据在密封模式下保持**加密**。 |
| 手机鉴权 | `NEKONEST_ADMIN_SECRET` 为管理员引导凭证（未设置时 `NEKONEST_PHONE_SECRET` 为单版本弃用别名）。一般 REST/WS 使用按设备授权范围的独立可撤销手机令牌。 |
| 迁移 | 一次显式破坏性 v0.1→v1 迁移；无长期 v0/v1 混跑协议。保留 daemon 设备 ID/令牌哈希与原生 store。验证备份后清理旧 VPS 明文消息、prompt、配对码与附件。手机重新登录/配对。 |
| 协议兼容 | 版本为 `major.minor`。major 不匹配则拒绝。minor 向后兼容：未知可选字段忽略；缺省能力为 false/unsupported。E2E、身份或投递语义破坏须升 major。永不把密封降为开放。 |
| 正式主机 OS | **Windows + Linux**。macOS 更后。 |
| 主 agent | **Codex** 为唯一全控制 v1 agent。规范路径：`codex app-server` JSON-RPC；钉选/探测最低兼容 CLI，自本地已验证的 0.144.1 协议面起。 |
| Codex 控制 | 发送、approve/deny、中断、steer 为 **MUST**。后续排队为 **SHOULD**。遗留 `codex exec resume` 为能力降级兼容路径。 |
| 开线程 | **仅 Codex。** 手机仅可在 daemon 报告的、已从原生会话发现的目录中开线程。经 app-server 使原生 store 拥有结果。禁止任意路径输入或扫盘。生命周期：`starting → owned \| failed \| indeterminate`（无永久幽灵行）。 |
| 其他 agent | Claude Code、Kilo、Kimi CLI、Grok Build：**兼容续聊** — 发现、所有权、历史、发送/流、中断、按宣告能力的附件。**不**承诺审批/开线程/steer/排队。 |
| 附件 | Codex app-server **MUST** 端到端支持图片与普通文件。其他适配器宣告 `native_image`、`path_best_effort` 或 `unsupported`；UI 不得暗示更高等级。 |
| 通知 | Codex 等待审批、等待用户输入、运行失败为 **MUST**。成功与设备离线为 **SHOULD**。密封推送仅含通用事件文案 + 设备/会话引用；详情在打开 PWA 后解密。 |
| 扩展 agent | **无** v1 发版门槛要求在五个 wire id 之外再加 agent。 |

### 7.1 窝服务器

| ID | 功能 | 优先级 | 验收要点 |
|---|---|---|---|
| S1 | 经独立可撤销手机令牌鉴权；管理员引导密钥用于铸造/配对管理路径 | MUST | 无鉴权不得公网绑定；保留 loopback 未鉴权开发模式；`NEKONEST_ADMIN_SECRET`（+ 单版本 `NEKONEST_PHONE_SECRET` 别名） |
| S2 | Daemon 引导注册 + 长期设备令牌 | MUST | bootstrap ≠ 手机令牌；公网模式禁止开放注册 |
| S3 | 配对（QR 为主；短时验证码 + 指纹比对回退） | MUST | 一次性、TTL；手机绑定设备无需再分享 bootstrap；签名 transcript |
| S4 | 多设备注册表 | MUST | 在线/离线、last_seen、显示名、OS（正式 `windows` / `linux`） |
| S5 | 同一设备多手机订阅，身份/令牌/授权独立 | MUST | 两部手机跟随同一设备；撤销其一不影响另一 |
| S6 | WebSocket 中继 + subscribe_ack 门闩 + 传输模式 / 协议 major.minor 协商 | MUST | ack 前不当作会话就绪；daemon 重连 generation 安全；拒绝 major 或模式不匹配 |
| S7 | 密封（或开放模式明文）消息耐久 + 服务端 prompt 命令日志 | MUST | 持久化失败可见；必要写入成功前不得假业务 ACK；密封模式仅存密文 |
| S8 | 附件上传/下载与大小/MIME 限制 | MUST | 需鉴权；密封模式仅存密文 blob + 非敏感元数据 |
| S9 | Web Push（VAPID）可行动事件 | MUST | MUST 事件：waiting_approval、waiting_user、运行失败。SHOULD：成功、设备离线。密封推送：通用文案 + 设备/会话引用 |
| S10 | Origin 白名单 + 可信代理 IP 模型 | MUST | 默认安全并有文档 |
| S11 | 限流 / 正文与帧大小上限 | MUST | |
| S12 | 健康检查 + 结构化运维日志（无密钥） | MUST | |
| S13 | **E2E 密封通道** 手机↔daemon（中继仅见密文与已文档化的路由元数据） | MUST | **新窝默认。** 开放中继仅管理员、显式、无自动回退 |
| S14 | 由服务托管静态 PWA | SHOULD | |
| S15 | 手机撤销 / 设备重命名 / 遗忘；手机身份列表 | MUST（手机撤销）；SHOULD（设备改名/遗忘） | 丢手机或退役主机可恢复 |
| S16 | 附件保留策略（TTL / 容量） | SHOULD | 运维可配 |
| S17 | 鉴权后的指标/调试端点 | MAY | |
| S18 | 显式 v0.1→v1 离线迁移（`schema_meta`、备份、清理明文） | MUST | 幂等；保留 daemon 设备 ID/令牌哈希；手机重新配对 |
| S19 | 授权手机的密钥包（包装的设备目录/会话密钥） | MUST | 配合 S13；撤销时 epoch 轮换 |

### 7.2 主机 daemon

| ID | 功能 | 优先级 | 验收要点 |
|---|---|---|---|
| D1 | 出站 WSS 重连（退避 + generation） | MUST | |
| D2 | 每配置身份单实例锁 | MUST | 所有支持 OS |
| D3 | **Windows + Linux** 一等公民主机 | MUST | 产品路径一致；各 OS 进程杀树正确。macOS 为 LATER |
| D4 | 适配器注册表；缺 CLI 非致命 | MUST | |
| D5 | 周期发现 + 所有权路由 | MUST | 空 transcript ≠ 所有权 |
| D6 | 历史导入稳定 id；排除 subagent/sidechain/primer | MUST | |
| D7 | 经各 agent 支持路径的 headless resume/send | MUST | Codex 优先 app-server；其余 CLI resume |
| D8 | Prompt 日志 fail-closed；`client_msg_id` 至多一次 | MUST | 密封信封精确重试；不得把重加密当作同一命令 |
| D9 | 流归一；stderr 仅诊断 | MUST | |
| D10 | 中断 / 停止进程树 | MUST | 支持 OS 无孤儿树（Windows Job Object；Linux 进程组） |
| D11 | 附件落到 per-run 临时目录 + agent 接线 | MUST | 各 agent 能力矩阵诚实；Codex 完整图片+文件 |
| D12 | **Codex app-server 原生审批桥** | MUST | 手机 approve/deny 到达真回调；非 Codex 宣告不可用；禁止假装 |
| D13 | 会话状态机：`idle` / `running` / `waiting_user` / `waiting_approval` / `error` | MUST | `waiting_*` 仅来自 app-server 正信号；不支持的适配器保持 running/idle/error |
| D14 | 每 agent/会话能力宣告 | MUST | 标志：control_mode、approve、deny、interrupt、steer、queue、spawn、attachment_mode；缺省 = false/unsupported |
| D15 | register + pair 生成 CLI；**doctor** 诊断 | MUST | 查协议/模式、鉴权、密钥、服务器、适配器、Codex app-server 方法/版本、可写状态、进程控制 |
| D16 | 自启动包（Windows 服务/任务；Linux `systemd --user`） | SHOULD | 文档一键路径；macOS launchd 为 LATER |
| D17 | 配置校验 + 非身份字段安全热更 | SHOULD | |
| D18 | 可选本机 loopback 调试 HTTP | MAY | |
| D19 | E2E 密钥（配合 S13） | MUST | 与 S13 同发；权限收紧；Linux 用 XDG |
| D20 | 仅 Codex：经 app-server 在当前已发现项目目录 `start_thread` | MUST | 日志：starting → owned \| failed \| indeterminate；owned 前须原生所有权 |
| D21 | 驾驭进行中的 Codex 回合（steer） | MUST | 能力门控；非 Codex 为 false |
| D22 | 发布加密设备目录（会话列表、已发现根、能力） | MUST | 配合 S13 |

### 7.3 Agent 适配器（v1 矩阵）

Wire id 保持稳定；新增 agent 必须全栈一致（schema、server、daemon、PWA、测试、文档）。

#### 7.3.1 全控制 agent（MUST）

| Wire id | 产品 | 角色 | 保证 |
|---|---|---|---|
| `codex` | Codex | **唯一全控制 v1 agent** | 发现、所有权、历史、发送/流、中断、**approve/deny**、**steer**、**spawn/`start_thread`**、**附件 `native_image_and_file`**，经 `codex app-server`。排队在协议能保证顺序时为 SHOULD。遗留 `codex exec resume` 仅为降级兼容（如实宣告能力）。主列表排除 subagent。自 0.144.1 面钉选/探测最低 CLI。 |

#### 7.3.2 兼容续聊 agent（MUST）

| Wire id | 产品 | 保证 | 明确不承诺 |
|---|---|---|---|
| `claude_code` | Claude Code | 发现、所有权、历史、发送/流、中断；附件按宣告等级 | 不承诺审批 / start_thread / steer / 排队 |
| `kilo` | Kilo | 同上兼容续聊集合 | 同上不承诺 |
| `kimi_cli` | Kimi CLI | 同上；现代 store；遗留路径有文档 | 同上不承诺 |
| `grok_build` | Grok Build | 同上；安全非交互默认 | 同上不承诺 |

非 Codex 附件等级：`native_image` \| `path_best_effort` \| `unsupported`。UI 不得暗示高于宣告的等级。

#### 7.3.3 扩展 agent

**LATER / 无 v1 门槛。** OpenCode、Gemini CLI、Cursor Agent 及其他 CLI 不是 v1.0.0 标签要求。无「至少两个扩展 agent」发版要求。

#### 7.3.4 适配器符合性

**全控制（Codex）MUST** 文档化并测试所有权、列表过滤、历史稳定、app-server 发送/流、中断、附件（图片+文件）、审批、steer、开线程生命周期、正信号状态检测、崩溃/重启与 fixture 语料。

**兼容续聊 agent MUST** 文档化并测试所有权、列表过滤、历史稳定、resume/send 参数、流映射、中断、附件策略与失败模式、诚实能力标志（除非真正实现否则审批/开线程/steer/排队为 false）与 fixture 语料。不得靠推断报告 `waiting_approval` / `waiting_user`。

### 7.4 手机客户端（PWA 优先）

| ID | 功能 | 优先级 | 验收要点 |
|---|---|---|---|
| P1 | 可安装 PWA | MUST | Offline shell；路由切换不重复挂 WS |
| P2 | 设置：窝 URL + 管理员/引导 → 独立手机身份；不可达时清晰错误 | MUST | |
| P3 | 配对（QR 为主；验证码 + 指纹回退） | MUST | |
| P4 | 设备列表（在线状态、线程数） | MUST | 密封模式下解密设备目录 |
| P5 | 工作区：目录 → agent → 线程树 | MUST | 折叠、排序、线程级或整项目归档、手动序 |
| P6 | 本地线程搜索（摘要、路径、agent） | MUST | 本地解密后 |
| P7 | 会话聊天：历史合并 + 直播 + 稳定 id | MUST | 重连不双份回合 |
| P8 | 输入：发送、草稿、附件、忙碌锁定 | MUST | 附件 UI 按能力分档 |
| P9 | 发件箱：`client_msg_id`、重试、上限、重连重放 | MUST | 重试同一密封信封；仅 committed 时清除 |
| P10 | **投递 UX**：accepted / committed / failed / not_seen / indeterminate | MUST | 用户可见，不只传输成功或已弃用的 `prompt_sent` |
| P11 | 中断控制 | MUST | 无能力时禁用 |
| P12 | **批准 / 拒绝** UI（`waiting_approval`） | MUST | 仅 Codex 且能力为真；否则禁用并解释 |
| P13 | 状态徽章：运行中 / 等你 / 等审批 / 错误 | MUST | |
| P14 | 推送选择加入 + 深链到目标会话 | MUST | 配合 S9；通用密封载荷；PWA 内解密详情 |
| P15 | i18n 简中 + 英文 | MUST | |
| P16 | 主题 浅/深/跟随系统 | MUST | |
| P17 | 消毒 Markdown + 代码块 | MUST | |
| P18 | 触控约 44px；safe-area；尊重减少动效 | MUST | |
| P19 | 引导文案与真实安装命令一致 | MUST | 分 OS（Windows + Linux） |
| P20 | 会话偏好持久化 | SHOULD | 线程/项目归档、折叠、排序 |
| P21 | 相册/相机附件 | SHOULD | 在附件限制内；Codex 完整路径 |
| P22 | 语音转文字（系统/浏览器 API） | SHOULD | 不强制云 STT |
| P23 | 运行中排队下一条（有能力时） | SHOULD | 否则禁用并说明；仅 Codex 且已宣告 |
| P24 | Steer / 跟进而不整段中断 | Codex app-server 能力为真时 MUST；UI 门控 | 非 Codex 禁用并说明 |
| P25 | 设备管理 + 手机身份撤销 | SHOULD / 自令牌撤销为 MUST | 配合 S15 |
| P26 | 离线横幅 + 上次同步时间 | SHOULD | |
| P27 | 无障碍：焦点、标签、对比度 | SHOULD | |
| P28 | 可选「原始日志」次要视图 | MAY | 不作主 UX |
| P29 | 仅 Codex：在已发现目录上开线程 UX | MUST | 仅 `thread_owned` 后导航；indeterminate 显示恢复说明 |
| P30 | IndexedDB 身份/密钥存储；密钥丢失以新身份重配 | MUST | 服务器不恢复旧手机私钥 |

### 7.5 线程生命周期（v1 产品决策）

| ID | 功能 | 优先级 | 规则 |
|---|---|---|---|
| L1 | 续接已有原生线程 | MUST | 五个 agent 的核心路径；保留原生 id |
| L2 | **从手机开线程** | MUST | **仅 Codex**，经 app-server，使**原生 store 出现线程** |
| L3 | 目录选择限于 daemon 发现得到的**当前已发现**项目目录 | MUST | 禁止任意扫盘；禁止运维手输路径；拒绝消失目录、`..`、符号链接逃逸 |
| L4 | Codex app-server 缺失、`spawn=false`、或目录不在当前已发现集合时拒绝 | MUST | 错误清晰；可能时在 spawn 前 → `thread_failed` |
| L5 | 开线程生命周期状态 | MUST | `thread_starting` → `thread_owned` \| `thread_failed` \| `thread_indeterminate`。按设备 + 操作 id fail-closed 日志。indeterminate 后不自动重试。**无永久窝侧幽灵行。** |
| L6 | 不做跨 agent 假迁移 transcript | MUST | 跨 agent handoff 属 LATER（除非原生工具支持） |
| L7 | 手机视图归档/隐藏 | SHOULD | 不删原生 store |
| L8 | 从手机删除/杀死原生线程 | LATER | v1 默认过毁 |
| L9 | 通用 `create_session` / 窝发明会话 | 禁止 | 不得重新引入 |

**不变量：** L2 成功（`thread_owned`）后，下一次发现必须能从原生 Codex store 找到该线程；手机不得永久持有主机无法 own 的线程。不确定的开线程仅通过后续发现对账。

### 7.6 聊天之外的控制面

| ID | 功能 | 优先级 | 验收 |
|---|---|---|---|
| C1 | 中断运行中工作 | MUST | 所有宣告 interrupt 的 agent |
| C2 | 批准 / 拒绝工具 | MUST | **Codex app-server 真桥**；其余诚实不可用 |
| C3 | agent 抛出的用户提问 | Codex 正信号为 MUST；通道 SHOULD | 与审批同一状态通道 |
| C4 | Prompt 排队 | SHOULD | 仅 Codex 且能力为真 |
| C5 | 驾驭进行中回合（steer） | MUST | Codex app-server；能力门控 |
| C6 | 只读 git 快照（分支、脏、短 status） | SHOULD | 无用户显式动作不自动 commit |
| C7 | 手机 git 写操作（commit/push） | LATER | 高风险；非 v1 阻塞 |
| C8 | Worktree / 多 agent 编排 UI | LATER | Pane 赛道 |
| C9 | IM 桥（Telegram / 飞书 / Discord） | LATER | 协议不禁止；不进 v1 客户端 |

### 7.7 通知

| ID | 事件 | 优先级 | 说明 |
|---|---|---|---|
| N1 | `waiting_approval`（Codex） | MUST | 通用密封推送 + 深链 |
| N2 | `waiting_user`（Codex 等你 / 用户输入） | MUST | 同上 |
| N3 | 后台跑完成功 | SHOULD | 非阻塞 |
| N4 | 运行失败 / 崩溃 | MUST | |
| N5 | 订阅中设备离线 | SHOULD | 非阻塞 |
| N6 | 免打扰 / 按设备静音 | SHOULD | |

推送载荷须深链到 `device + session`，且不嵌入密钥、正文、审批细节或路径。详情仅在打开 PWA 后解密。

旅程拆分（取代大杂烩单一故事）：

1. **投递 / 中断** — 发送、accepted→committed、重连完整性、中途 interrupt  
2. **推送 / 深链** — 通用 attention 事件 → 打开路由 → 解密详情  
3. **Codex 审批** — 真实阻塞请求 → 批准/拒绝 → 继续  

### 7.8 安全与隐私

| ID | 功能 | 优先级 | 说明 |
|---|---|---|---|
| X1 | 管理员引导密钥、daemon bootstrap、独立手机令牌分离 | MUST | |
| X2 | 不记密钥日志；不进 git | MUST | |
| X3 | 公网模式必须鉴权 + bootstrap | MUST | |
| X4 | 附件白名单 + 大小上限（端上明文策略；服务端密文上限） | MUST | |
| X5 | **默认 E2E 密封模式**（S13） | MUST | 已文档化元数据仍可见（见 §7.0） |
| X6 | 可信私有 VPS 的开放中继模式 | MUST | 仅管理员启用；窝固定模式；README 警告；不混用/不回退 |
| X7 | 配对 QR 不在明文日志打印长期令牌 | MUST | |
| X8 | 手机撤销 + 密钥 epoch 轮换；设备令牌轮换 / 重新配对 | MUST（手机撤销）；SHOULD（设备轮换） | |
| X9 | 可选客户端锁（PIN/生物识别） | MAY | 仅本地 UX |
| X10 | 多用户 ACL | LATER | v1 单人运维 |
| X11 | 连元数据也对窝不可见 | LATER | |
| X12 | 密钥丢失策略：以新身份重配；原生 store 重建；VPS 不备份手机私钥 | MUST | 文档说明不可恢复的 VPS-only 历史 |
| X13 | 密封信封认证 AAD；唯一 nonce + 单调序号 | MUST | 拒绝重复/窗口外序号 |

### 7.9 安装、运维、文档

| ID | 功能 | 优先级 |
|---|---|---|
| O1 | 文档化 VPS 路径（二进制 + 反代 + TLS） | MUST |
| O2 | 各正式 OS 的 daemon 安装（Windows + Linux） | MUST |
| O3 | daemon `doctor` + 窝健康检查 | MUST |
| O4 | e2e-smoke 对齐本文合同 | MUST |
| O5 | 双语 README + 运维文档在发版时按 v1 重写 | MUST |
| O6 | 继续强制协议变更核对清单 | MUST |
| O7 | 发布产物（server；daemon windows-amd64、linux-amd64、linux-arm64；PWA 说明） | MUST |
| O8 | v0.1 → v1.0 迁移指南（备份、schema、重配、E2E 密钥、管理员密钥更名） | MUST |
| O9 | 应用内版本 / 支持诊断包（脱敏） | SHOULD |
| O10 | Linux `systemd --user` 单元路径 | SHOULD |
| O11 | 文档钉选 Codex 基线与能力降级行为 | MUST |

### 7.10 线协议

| ID | 功能 | 优先级 |
|---|---|---|
| W1 | 保持信封模型；多语言表面手工同步 | MUST |
| W2 | `protocol_version` major.minor 协商；major 不匹配则拒绝 | MUST |
| W3 | `transport_mode` sealed \| open；不匹配则拒绝；无密封→开放回退 | MUST |
| W4 | 手机处理完整 prompt 生命周期类型；弃用 `prompt_sent` | MUST |
| W5 | 会话状态：`idle` \| `running` \| `waiting_user` \| `waiting_approval` \| `error` | MUST |
| W6 | 能力标志（缺省 = false/unsupported） | MUST |
| W7 | Codex `start_thread` / `thread_starting` / `thread_owned` / `thread_failed` / `thread_indeterminate` | MUST |
| W8 | 带稳定 approval id 的审批载荷；`steer` | MUST |
| W9 | E2E 控制与 `sealed_payload`；配对/密钥消息（`pair_*`、`key_package`、`phone_revoked`） | MUST |
| W10 | 驱动推送的通用 `attention_event` | MUST |
| W11 | IM/webhook 扩展点 | LATER |

任何 W* 变更走既有跨层清单（`protocol.json`、Go、TS、daemon、测试、文档）。

## 8. 端到端用户旅程（v1 验收故事）

### J1 — 首次安装（Windows 或 Linux）

1. 运维部署窝（TLS、管理员密钥、bootstrap）；传输模式密封（默认）。  
2. 家用主机装 daemon（Windows 或 Linux），注册身份密钥，doctor 全绿。  
3. 打开 PWA，建立手机身份，经 QR（或验证码 + 指纹）配对。  
4. 看到设备在线与解密后的会话列表，或在已发现根存在时出现 Codex「在项目中开始」。

### J2a — 投递与中断

1. 打开已有 Codex（或兼容）线程；历史与原生 gist 一致。  
2. 发送；投递显示 accepted→committed（适用时含 not_seen/failed/indeterminate）。  
3. 流式渲染；能力为真时中途 interrupt 有效；无孤儿进程树。

### J2b — 推送与深链

1. 订阅中 App 进后台。  
2. 收到通用 attention 推送（无密钥/正文）。  
3. 打开 device+session 深链；PWA 拉取并解密详情。

### J2c — Codex 审批

1. 触发真实 Codex 审批；状态 `waiting_approval`。  
2. 收到通用推送；深链；解密审批详情。  
3. 批准或拒绝；观察继续或诚实失败。

### J3 — 手机开 Codex 线程

1. 从**当前已发现**根目录选路径 + 已具备 app-server 的 Codex。  
2. Daemon 发出 starting → owned（原生 store 确认）或 failed / indeterminate。  
3. owned 后手机导航到正确目录/agent 下线程；PC 侧 CLI 看到同一原生线程。  
4. 伪造或消失路径被拒绝；重试不双 spawn。

### J4 — 多机

1. 两台 daemon 配到同一窝。  
2. 手机切换设备；订阅隔离；prompt 不串台。

### J5 — 重连完整性

1. 发送中断网；发件箱保留 `client_msg_id` 与精确密封信封。  
2. 恢复网络；agent 不双跑；UI 收敛为单条用户回合。

### J6 — 密封模式

1. 默认密封窝；抽查窝 DB/日志/附件存储无正文明文/文件名/审批细节。  
2. 本地解密后聊天、审批与开线程仍可用。

### J7 — 多手机与撤销

1. 配对手机 A/B；二者在当前 epoch 下解密共享历史。  
2. 撤销 A；A 失去 WS/令牌/推送/授权；epoch 轮换；B 以新密钥包继续。  
3. A 上已解密本地历史无法远程抹除（有文档）。

### J8 — 密钥丢失

1. 清空 PWA 状态；以新身份重配。  
2. 从原生 agent store 重建可得历史；明确省略不可恢复的 VPS-only 历史。

### J9 — 开放模式（管理员）

1. 管理员在服务器启用开放模式；daemon 与手机显式选择加入。  
2. 仅密封客户端无法连接；无自动回退。

### J10 — 迁移

1. 植入 v0.1 DB/文件；验证备份；离线迁移。  
2. Daemon 令牌仍可鉴权；旧明文内容已清除；手机重新登录/配对。

## 9. UX 信息架构

```text
设置
  └─ 窝 URL + 管理员/引导 → 手机身份（+ 密封密钥）

首页
  └─ 设备[]
        └─ 工作区
              ├─ 搜索
              ├─ 目录
              │     └─ Agent
              │           └─ 线程列表（状态徽章）
              ├─ 开 Codex 线程（仅已发现目录）
              └─ 设备设置 / doctor 摘要 / 撤销

线程
  ├─ 顶栏：agent、路径、状态、中断
  ├─ 时间线：消息、工具、审批
  ├─ 审批 / 用户输入卡片（待处理时；Codex）
  ├─ Steer 控制（能力为真时）
  └─ 输入：文本、附件（分档）、排队指示、发送
```

## 10. 非功能要求

| 领域 | v1 目标 |
|---|---|
| Prompt 结果可见性 | daemon 决策后数秒内用户可见终态失败 |
| 历史首屏 | 数十条量级可用窗口，不为全量原生导入永久卡住 |
| 发现节奏 | 正常负载下新主机线程约 30s 内出现在手机（可调） |
| 手机耗电 | 无热循环轮询；WS + 推送 |
| 体积/依赖 | server/daemon 保持精简 Go 部署；PWA 标准 pnpm 构建 |
| 并发 | 每 WS 单写者；慢 IO 不持锁（既有工程不变量） |
| Windows 进程卫生 | Job Object 或等价；interrupt 清子进程 |
| Linux 进程卫生 | 进程组 SIGINT 后有界 SIGKILL；无孤儿 |
| iOS PWA 限制 | 文档说明推送/PWA 约束；优雅降级 |
| 协议兼容 | 按 §7.0 的 major.minor |

## 11. v1.0.0 明确不做

不因下列项阻塞 v1：

- macOS 正式支持（LATER；v1 标签仅 Windows + Linux）  
- 五个 wire id 之外的扩展 agent（无 ≥2 扩展门槛）  
- 对非 Codex agent 的同等全控制（审批/steer/开线程/完整附件）  
- App Store 原生 iOS/Android（PWA 为主；原生 MAY 更后）  
- 飞书 / Telegram / Slack / Discord 作主客户端  
- 多租户 RBAC、组织、SSO  
- 多租户云 SaaS 商业托管  
- 完整 git 写 UX、PR 合并驾驶舱  
- Worktree 舰队编排（Pane 级）  
- 以裸终端模拟器为主产品  
- 在 VPS 上跑 agent 替代家用主机  
- 自动跨 agent 迁移 transcript  
- 保证支持全世界所有长尾 CLI  
- 对窝完全隐藏元数据  
- 多人 CRDT 协作编辑同一线程  
- 服务器备份手机私钥  
- 远程抹除已解密的手机历史  
- 同一窝长期混跑 v0/v1 协议  

## 12. 对照：v0.1 → v1.0（增量摘要）

| 领域 | v0.1（上线切片） | v1.0.0 合同 |
|---|---|---|
| 主机 OS | 产品仅 Windows | **Windows + Linux**；macOS 更后 |
| 开线程 | 手机禁止 | **仅 Codex** `start_thread` 进入**当前已发现**目录（app-server）；无通用 `create_session` |
| 审批 | 线协议为主；UI 只展示；CLI 常不可用 | **Codex 真桥** + UI；其余诚实不可用 |
| Steer | 未产品化 | Codex app-server 可用时为 **MUST** |
| 投递 UX | 多半 `prompt_sent` / failed | 完整生命周期含 not_seen / indeterminate |
| 状态 | running / idle / waiting_approval | + waiting_user / error；仅正信号 |
| 推送 | 配 VAPID 才可选 | MUST attention 事件必有路径；通用密封载荷 |
| 信任 | VPS 明文 | **默认密封 E2E** + 仅管理员开放中继 |
| Agent | 固定 5 个、同等期待 | **Codex 全控制** + **4 兼容续聊**；无扩展门槛 |
| 附件 | 尽力而为 | Codex 图片+文件 MUST；其余分档宣告 |
| 鉴权 | 共享手机密钥 | 管理员密钥 + **独立手机令牌/授权/撤销** |
| 安装 | 偏手工构建 | doctor + 自启动 + Windows/Linux 文档 |
| 配对 | 6 位码 | QR 为主 + 验证码/指纹回退 |
| 能力 | 隐式 | 宣告标志；缺省 false |
| 多机 | 注册表可能 | 一等旅程 |
| 协议 | v0 信封 | major.minor + transport_mode |
| 文档 | 把妥协写成「边界」 | **本文为 v1 产品真相** |

## 13. 建议实现波次（不是多个产品）

波次仅是 v1 工程内的交付顺序；打标签仍要求 MUST 全部完成。与 Codex 优先 E2E 实现计划对齐。

| 波次 | 焦点 | 退出条件 |
|---|---|---|
| **W0 — 合同** | 本文冻结；AGENTS.md 对 Codex start_thread 的例外 | EN/ZH 对等；无未决发版阻塞决策 |
| **W1 — 协议脚手架** | 信封、状态、能力、开线程/steer/配对/密钥消息、major.minor + 模式 | 旧客户端清晰失败；v1 对等体先协商 |
| **W2 — 服务端迁移 + 手机身份** | schema_meta、migrate-v1、管理员密钥、授权、撤销 | 两手机独立；撤销即时 |
| **W3 — 密码学 + 配对** | 向量、QR、密钥包、epoch | 中继无法替换密钥；第二部手机可配对 |
| **W4 — 密封中继耐久** | 不透明载荷、密封附件、通用 attention 推送 | 密封服务端无应用正文明文 |
| **W5 — 能力 + 控制 UX** | 标志、投递状态、门控控件 | 无能力则无控件 |
| **W6 — Codex app-server 全控制** | approve/deny/interrupt/steer/附件 | 钉选基线上 J2a–J2c |
| **W7 — Codex 开线程** | 已发现目录、日志生命周期 | J3；原生所有权 |
| **W8 — Windows + Linux 正式化** | 路径、进程组、产物、doctor、systemd | 两 OS 基线上 J1 |
| **W9 — 发版文档 + 门槛** | 迁移、安全模型、CHANGELOG、smoke | v1.0.0 标签 |

## 14. 已关闭决策（冻结）

此前开放项全部关闭。决议：

| # | 主题 | 决议 |
|---|---|---|
| 1 | **E2E 默认** | 新窝默认密封。开放中继保留、默认关闭、**仅管理员**启用。一窝一模式；不混用；不自动降级。 |
| 2 | **开线程 UX** | **仅** daemon **当前发现集合**中的目录（来自原生会话）。禁止任意路径输入；v1 不要求单独运维白名单文件。仅 Codex app-server。 |
| 3 | **鉴权演进** | 管理员引导密钥（`NEKONEST_ADMIN_SECRET`，单版本 `NEKONEST_PHONE_SECRET` 别名）。一般访问：**独立可撤销手机令牌** + 设备授权。多手机密钥按手机包装。 |
| 4 | **Codex 审批传输** | 规范路径：**`codex app-server` JSON-RPC**。自本地已验证 **0.144.1** 协议面钉选/探测最低兼容 CLI。遗留 `exec resume` 仅降级。Claude Code 等 v1 不承诺审批。 |
| 5 | **扩展 agent** | v1.0.0 **无**额外要求。无「至少两个」门槛。 |
| 6 | **消息类型名** | 按计划目录在实现中钉选：`start_thread`、`thread_starting`、`thread_owned`、`thread_failed`、`thread_indeterminate`、`steer`、`pair_request`、`pair_confirm`、`pair_ready`、`pair_failed`、`key_package`、`phone_revoked`、`attention_event`；状态含 `waiting_user`。精确 schema 在 Phase 1 `protocol.json` 落地。 |
| 7 | **正式 OS** | Windows + Linux MUST；macOS LATER。 |
| 8 | **Agent 角色** | Codex 全控制；其余四者为兼容续聊。 |
| 9 | **开线程生命周期** | `starting → owned \| failed \| indeterminate`；无永久幽灵行。 |
| 10 | **通知** | MUST：waiting_approval、waiting_user、运行失败。SHOULD：成功、设备离线。 |
| 11 | **协议版本** | 按 §7.0 的 `major.minor`。 |
| 12 | **迁移 / 密钥丢失 / 元数据** | 按 §7.0 表。 |

修改任一行须先更新本文件与 `v1-product.md`，再继续实现。

## 15. 文档治理

| | |
|---|---|
| **v1.0.0 权威产品合同** | 本文件 + `v1-product.md` |
| **现行工程不变量** | [AGENTS.md](../AGENTS.md)（含仅 Codex 的 `start_thread` 例外；禁止通用 `create_session`） |
| **v0.1 运维文档** | 发版前仍为 `docs/*.md` — **构建 v1 期间仅参考** |
| **线上根 README** | 在发版重写前描述**现行 v0.1** 行为 — 不作 v1 合同 |
| **冻结历史** | `docs/archive/` — 永不当合同 |
| **用户可见历史** | [CHANGELOG.md](../CHANGELOG.md) |
| **实现计划** | `.kilo/plans/*-v1-codex-e2e-release-plan.md`（工程顺序；产品含义以本合同为准） |

### 变更流程

1. 产品范围变更先改本合同。  
2. 同步简体中文镜像并保持对等。  
3. 不变量变化时更新 `AGENTS.md`。  
4. 实现并配测试。  
5. v1.0.0 发版时：按本合同重写 README 与运维文档；把过时的「v0.1 边界」表述移出线上 README。

---

*当单人运维能部署默认密封的窝、在 Windows 与 Linux 上连接家用机、为五个 agent 续接原生线程、对 Codex 完成全控制闭环（通知 → 审批/steer/中断/在已发现目录开线程 → 完成）、对其余四个诚实兼容续聊、在已文档化的元数据边界内信任投递与密封传输——且产品没有滑成远程 IDE、云端 agent、或对每个 CLI 同等全控制承诺——NekoNest v1.0.0 即告完成。*
