> [English](./configuration.md) | 简体中文

# 配置参考

NekoNest v0.2.x 的环境变量、命令行 flags、配置文件与运行限额权威列表。产品边界见根目录 [README.zh-CN.md](../README.zh-CN.md)；工程不变量见 [AGENTS.md](../AGENTS.md)。

## Server

二进制：`nekonest-server`（`server/cmd/server`）。

### Flags

| Flag | 默认 | 说明 |
|---|---|---|
| `-port` | `8080` | 监听端口 |
| `-data` | `./data` | 数据目录（SQLite + 附件） |
| `-pwa` | `./pwa-dist` | 已构建 PWA 静态目录 |
| `-version` | — | 输出 Server 应用发行版本并退出 |

### 监听地址

| 管理员密钥（`NEKONEST_ADMIN_SECRET` 或兼容别名） | 绑定地址 |
|---|---|
| **未设置 / 空** | 仅 `127.0.0.1:<port>`（本地开发） |
| **已设置** | `0.0.0.0:<port>`（`:<port>`），供公网反代 |

不要把未鉴权 Server 暴露到局域网或公网。

### 环境变量

| 变量 | 公网是否必需 | 说明 |
|---|---|---|
| `NEKONEST_ADMIN_SECRET` | **是** | 首选管理员引导密钥；可直接鉴权并签发独立手机身份/令牌。 |
| `NEKONEST_PHONE_SECRET` | 兼容 | `NEKONEST_ADMIN_SECRET` 的单版本弃用别名。 |
| `NEKONEST_BOOTSTRAP_TOKEN` | **是** | 保护 `POST /api/devices/register`（头 `X-Neko-Bootstrap`）。必须与管理员密钥不同。 |
| `NEKONEST_TRANSPORT_MODE` | 否 | 全窝固定 `open` \| `sealed`；v0.2 默认 `open`。密封模式为显式预览，所有端必须一致。 |
| `NEKONEST_ALLOWED_ORIGINS` | 建议 | 逗号分隔的浏览器来源白名单。 |
| `NEKONEST_TRUST_PROXY` | 若在反代后 | 仅当反代**覆盖** `X-Forwarded-For` / `X-Real-IP` 时设为 `1` 或 `true`。用于限流客户端 IP。 |
| `NEKONEST_TRUSTED_PROXY_CIDRS` | 反代非 loopback | 可信反代 CIDR/IP 列表。 |
| `NEKONEST_VAPID_PUBLIC_KEY` | 可选 | Web Push VAPID 公钥（base64url）。 |
| `NEKONEST_VAPID_PRIVATE_KEY` | 可选 | Web Push VAPID 私钥。 |
| `NEKONEST_VAPID_SUBJECT` | 可选 | Web Push 联系地址，如 `mailto:you@example.com`。 |

#### Bootstrap 行为

| 管理员密钥 | Bootstrap | 注册 |
|---|---|---|
| 已设 | 已设 | 需要 `X-Neko-Bootstrap` |
| 已设 | 空 | 注册**禁用** |
| 空（开发） | 空 | 注册开放（仅开发，且 loopback） |
| 空（开发） | 已设 | 仍按服务端逻辑校验 bootstrap |

### 服务端数据布局

```text
<data>/
  nekonest.db
  attachments/
```

将该目录视为敏感数据，备份时同等保护。

### HTTP / WebSocket 面

| 路径 | 作用 | 鉴权 |
|---|---|---|
| `GET /health` | 存活检查，并返回 `server_version` 与 `protocol_version` | 无 |
| `GET /ws/phone` | 手机 WebSocket | 手机密钥 |
| `GET /ws/daemon` | Daemon WebSocket | 注册后的设备令牌 |
| `GET /api/devices` | 设备列表 | 手机密钥 |
| `POST /api/devices/register` | Daemon 注册 | Bootstrap（公网） |
| `GET /api/devices/sessions` | 设备会话 | 手机密钥 |
| `GET /api/messages` | 消息历史 API | 手机密钥 |
| `POST /api/attachments` | 上传附件 | 手机密钥 |
| `GET /api/attachments/{id}` | 下载附件 | 手机密钥等（按实现） |
| `POST /api/push/subscribe` | Web Push 订阅 | 手机密钥 |
| `GET /api/push/vapid-public-key` | VAPID 公钥 | 手机密钥 |
| `POST /api/pair/generate` | 签发配对码 | 设备侧流程 |
| `POST /api/pair/consume` | 手机消费 6 位码 | 手机密钥 |
| `/` 与 SPA 资源 | `-pwa` 存在时提供 PWA | — |

消息类型详见 [protocol.zh-CN.md](./protocol.zh-CN.md)。部署见 [deploy-vps.zh-CN.md](./deploy-vps.zh-CN.md)。

---

## Daemon（Windows/Linux）

二进制：`nekonest-daemon.exe`（`daemon/cmd/daemon`）。

### Flags

| Flag | 说明 |
|---|---|
| `-register` | 向 Server 注册本机（需要 `NEKONEST_SERVER`） |
| `-name <string>` | 注册时的设备显示名 |
| `-pair gen` | 为已注册设备生成新的 6 位手机配对码 |
| `-config <path>` | 配置文件路径（默认 `%USERPROFILE%\.nekonest\config.json`） |
| `-doctor` | 运行诊断，包括 Daemon / Server 应用版本是否一致 |
| `-version` | 输出 Daemon 应用发行版本并退出 |

### 环境变量（注册时）

| 变量 | 何时 | 说明 |
|---|---|---|
| `NEKONEST_SERVER` | `-register` | VPS 地址，如 `https://nekonest.example.com`（http(s) 会规范为 ws(s)） |
| `NEKONEST_BOOTSTRAP_TOKEN` | 公网 `-register` | 与 Server 相同；以 `X-Neko-Bootstrap` 发送 |
| `NEKONEST_TRANSPORT_MODE` | 所有运行 | `open`（v0.2 默认）或 `sealed`；必须与 Server 和 PWA 构建一致。 |

常驻运行从配置文件读凭据，不依赖上述环境变量。

### 配置文件

默认：`%USERPROFILE%\.nekonest\config.json`

| 字段 | JSON 键 | 说明 |
|---|---|---|
| 服务器 URL | `server_url` | `wss://…` / `ws://…`（也接受 http(s)） |
| 设备 ID | `device_id` | 注册时分配 |
| 令牌 | `token` | 设备鉴权令牌；**机密** |
| 工作目录 | `work_dir` | 可选会话目录提示 |

### 实例锁与日志

| 路径 | 用途 |
|---|---|
| `<config>.daemon.lock` | 单实例锁；同配置第二进程会被拒绝 |
| 提示词 journal（配置旁、按设备） | 接受/提交状态，用于至多一次投递 |

### 配置热更

Daemon 监视配置路径，变更时替换内存中的 `*Config` 快照。

- 非凭据类字段可通过快照替换生效。
- **`device_id` / `token` 在进程生命周期内固定**。更改身份类字段需要**重启**进程。

### URL 规范化

| 输入 | 拨号形式 |
|---|---|
| `https://host` | `wss://host` |
| `http://host` | `ws://host` |
| `wss://` / `ws://` | 不变 |
| 裸 `host:port` | `ws://host:port` |

REST 使用由 ws(s) 推导的 http(s)。部署见 [deploy-windows.zh-CN.md](./deploy-windows.zh-CN.md)。

---

## PWA

| 构建变量 | 默认 | 说明 |
|---|---|---|
| `VITE_NEKONEST_TRANSPORT_MODE` | `open` | 必须与 Server、Daemon 一致；仅为显式配置的密封预览窝设为 `sealed`。 |

---

## 附件与客户端限额

| 限额 | 值 |
|---|---|
| 单次最多文件数 | **5** |
| 单文件最大 | **4 MB** |
| 允许 MIME（服务端） | `image/jpeg`、`image/jpg`、`image/png`、`image/webp`、`image/gif`、`text/plain`、`text/markdown`、`application/pdf`、`application/json` |
| PWA 图片最长边 | 1920 px（适用时客户端缩小） |
| PWA 待发 outbox | 最多 40 条 |
| 历史拉取（daemon） | 最多约 **40** 条；单条内容常截断约 **4000** rune |

各智能体附件接线见 [README.zh-CN.md](../README.zh-CN.md) 与 [deploy-windows.zh-CN.md](./deploy-windows.zh-CN.md)。

---

## 配对

- 配对码为 **6 位数字**。
- 服务端签发 TTL 约 **5 分钟**。
- 手机在持有手机密钥的前提下消费配对 API；Daemon 在 `-register` 或 `-pair gen` 后打印配对码。

## 相关文档

- [安全模型](./security.zh-CN.md)
- [架构](./architecture.zh-CN.md)
- [VPS 部署](./deploy-vps.zh-CN.md)
- [Windows 部署](./deploy-windows.zh-CN.md)
- [排障](./troubleshooting.zh-CN.md)
