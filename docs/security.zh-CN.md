> [English](./security.md) | 简体中文

# 安全模型

NekoNest 如何信任各组件、哪些密钥保护哪些接口，以及运维者应对 VPS 作何假设。本文是运维指南，不是正式威胁建模论文。

## 信任拓扑

```text
手机 PWA  ──HTTPS/WSS──►  VPS Server  ◄──出站 WSS──  主机 Daemon（Win/Linux）  ──►  本机 agent CLI/存储
```

| 组件 | 信任角色 |
|---|---|
| **手机** | 持有 admin 引导密钥和/或独立 phone token；E2E 私钥在 IndexedDB |
| **VPS** | 认证手机与 daemon；中转 WebSocket；按模式持久化设备与消息/附件 |
| **家用机 / Daemon** | 仅出站连接；持有 E2E 身份与内容密钥；读取原生 agent 存储；运行无头 CLI |
| **Agent CLI** | 会话/历史权威存储；以用户本机权限执行工具 |

### 传输模式（v0.2）

| 模式 | 默认 | VPS 可见 |
|---|---|---|
| **sealed** | 显式预览 | 密文正文；路由元数据（设备 ID、session ID、时间戳、大小、连接状态）。全密封时**不含** prompt/回复/工具明文。 |
| **open** | v0.2 默认 | 应用明文——视 VPS 为敏感 |

每个乐园只有一种固定模式；客户端必须匹配；**禁止** sealed→open 自动降级。待密封验收门槛完成后，v1.0.0 合同会把新乐园默认值切换为 sealed。

配对信任：QR 携带 daemon 公钥指纹；仅 6 位码为低保证 fallback（须对照 PC 屏幕指纹）。

## 影响安全的产品边界

- 手机主要**续接**原生线程。每个受支持 agent 都只有在原生 starter 已安装且探测通过后才能宣告 `start_thread`；目标仍限于 daemon **当前已发现**的项目目录。
- Daemon **不需要**家用机入站端口。
- 各 agent **本机原生存储**是发现与历史的权威来源。
- Codex app-server 是唯一完整审批路径；其他 agent 诚实宣告能力，原生新建不代表支持审批或 steer。

## 密钥与凭据

| 密钥 | 持有方 | 保护对象 |
|---|---|---|
| `NEKONEST_ADMIN_SECRET`（别名 `NEKONEST_PHONE_SECRET`） | 运维 | 管理员引导 / 签发 phone token；遗留全量手机访问 |
| Phone token | 每部手机 | 日常 REST/WS；按设备 grant 作用域；可撤销 |
| `NEKONEST_BOOTSTRAP_TOKEN` | 运维 + 注册时的 daemon | `POST /api/devices/register`（`X-Neko-Bootstrap`） |
| Daemon `config.json` 中的 `token` | 仅家用机 | 注册后的 Daemon WebSocket 身份 |
| Daemon `identity.json` / sealed keys | 仅家用机 | E2E 长期密钥与内容密钥 |
| 配对码 + QR 指纹 | 短时 | 将手机绑定到已注册设备 |
| VAPID 密钥 | 运维 | 可选 Web Push |

Cloud 的账号、权益、placement 与区域路由策略位于公开 Server 和 Relay Core 之外。
托管 Relay 先把经过认证的凭据解析为内部租户，再选择 Engine；客户端不能自行提交
原始租户 ID。Standalone 注册仍由本地 bootstrap token 保护，并且完全不访问 Cloud。

### 规则

1. **手机密钥 ≠ bootstrap 令牌**，使用两段独立长随机串。
2. **切勿提交**密钥、`config.json`、`data/`、附件原文或原生 agent 转录。
3. **不要在共享日志中打印**设备令牌、手机密钥、bootstrap 或完整提示词正文。
4. 轮换手机密钥/bootstrap 需协调重部署；轮换设备令牌需重新注册 daemon（新 `config.json`）。
5. 配对码很快过期（服务端约 5 分钟）。优先 `-pair gen`，勿长期散落旧码。

## 开发模式 vs 公网模式

| 模式 | 条件 | 绑定 | 注册 |
|---|---|---|---|
| **本地开发** | `NEKONEST_PHONE_SECRET` 为空 | 仅 loopback（`127.0.0.1`） | bootstrap 亦空时可能开放 |
| **公网** | 已设手机密钥 | 全接口（置于 TLS 反代后） | **必须** bootstrap；否则注册禁用 |

> [!WARNING]
> 切勿通过强行非 loopback 绑定或不当反代，把未鉴权/开放注册模式暴露到公网。

## 鉴权面

### 手机

- REST：`Authorization: Bearer <NEKONEST_PHONE_SECRET>` 或 `X-Neko-Secret: <secret>`
- WebSocket：同一密钥（客户端流程支持 `secret=` 查询参数）
- 来源：设置 `NEKONEST_ALLOWED_ORIGINS` 时仅接受列表中的 Origin

### Daemon 注册

- `POST /api/devices/register`，JSON `{"name":"…"}`，公网需 `X-Neko-Bootstrap`
- 响应得到 `device_id` + 设备令牌，写入 `%USERPROFILE%\.nekonest\config.json`
- Daemon 使用长期 Ed25519 密钥签署域分离的注册 transcript。托管 Cloud 复用已撤销 host ID 前必须验证该证明；只有公开指纹或旧 bearer 令牌不足以恢复。

### Daemon 运行时

- 出站连接 `/ws/daemon`，使用已存设备令牌鉴权
- 配置路径上的单实例锁降低同一身份双开风险

### 配对

1. Daemon 取得 6 位码（注册或 `-pair gen`）。
2. 手机在已用手机密钥登录的前提下输入该码。
3. Server 将该手机与该设备关联，用于列表与消息。

## 反代与客户端 IP

限流等逻辑使用 `clientIP` 辅助函数：

- 默认**忽略** `X-Forwarded-For`（使用直连地址）。
- 仅当反代**覆盖**转发头为真实客户端地址时设 `NEKONEST_TRUST_PROXY=1`（勿盲目追加不可信客户端链）。
- 优先单一可信跳（按实现取代理控制的值/最右跳语义）。
- 反代不在 loopback 时用 `NEKONEST_TRUSTED_PROXY_CIDRS` 声明。

示例见 [deploy-vps.zh-CN.md](./deploy-vps.zh-CN.md)。

## 附件

- 手机上传需手机密钥。
- 服务端强制 **4 MB** 上限与图片/文本/PDF/JSON MIME 白名单。
- 客户端单次最多 **5** 个文件。
- Daemon 下载到 Windows **每次运行的临时目录**，再按 agent 传入路径或原生参数。
- 附件字节落在 VPS 磁盘上——默认按可持久暴露处理。

## WebSocket 与滥用控制

- 已鉴权帧有大小限制（含历史类较大负载，约数 MiB 量级）。
- REST 正文另有上限（通用 handler 约 1 MiB；附件单独限额）。
- 每个 gorilla/websocket 连接一读一写。
- 勿在未评估容量前随意放宽 body/帧限制。

## 提示词投递完整性

与密码学无关、但与安全相关的投递属性：

- `client_msg_id` 加上 accepted / committed / failed 在重连场景提供**至多一次**语义。
- 传输成功 **≠** agent 已接受。
- Daemon 提示词 journal 在无法安全判断是否已接受时**失败关闭**——宁可可见失败，不可静默双执行。

详见 [architecture.zh-CN.md](./architecture.zh-CN.md)。

## 家用 PC 面

- Daemon 以登录用户身份运行，继承其对 agent 存储与项目目录的访问权。
- 无头 CLI 可能以该用户完整本机权限跑工具。
- Windows 使用 Job Object，便于 stop/interrupt 杀掉整棵进程树。
- 可选 Defender 排除会**扩大**本机攻击面——仅在理解代价后使用。

## 运维加固清单

- [ ] Caddy/Nginx 终止 TLS；按域名需要配置 HSTS
- [ ] 设置长且互不相同的 `NEKONEST_PHONE_SECRET` 与 `NEKONEST_BOOTSTRAP_TOKEN`
- [ ] `NEKONEST_ALLOWED_ORIGINS` 设为公网 HTTPS 来源
- [ ] 仅在覆盖 XFF 的反代后启用 `NEKONEST_TRUST_PROXY=1`
- [ ] systemd（或等价）使用专用用户；Linux `data/` 为 `0700`，DB/WAL/SHM 文件为 `0600`
- [ ] Docker 使用非 root 镜像用户、只读根文件系统、移除 capabilities，并挂载预先创建且由 uid/gid `10001` 持有的 `0700` `/data`
- [ ] 密钥放在权限受限的 `EnvironmentFile`，勿写进 world-readable unit
- [ ] 防火墙仅 80/443 公网；应用端口（如 8080）仅本机
- [ ] `data/` 备份加密且访问受控
- [ ] Daemon `config.json` ACL 限本机用户
- [ ] 启用 Web Push 时离线生成 VAPID
- [ ] 公网主机无调试/开放注册模式
- [ ] 共享运维日志只含稳定标识匿名值/状态，不含原始标识、凭据、提示词/响应正文、审批/输入详情、附件路径、推送正文或原始 CLI stderr

## NekoNest 不声称的能力

- 手机 ↔ PC 端到端加密
- 单 VPS 多租户零信任隔离（面向单运维自托管）
- 对 VPS 运维者隐藏提示词
- 在手机侧沙箱化 agent 工具执行（工具在 PC 上跑）

## 相关文档

- [配置](./configuration.zh-CN.md)
- [VPS 部署](./deploy-vps.zh-CN.md)
- [排障](./troubleshooting.zh-CN.md)
- [协议概览](./protocol.zh-CN.md)
