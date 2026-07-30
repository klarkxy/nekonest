> [English](./security.md) | 简体中文

# 安全模型

NekoNest 如何信任各组件、哪些密钥保护哪些接口，以及运维者应对 VPS 作何假设。本文是运维指南，不是正式威胁建模论文。

## 信任拓扑

```text
手机 PWA  ──HTTPS/WSS──►  VPS Server  ◄──出站 WSS──  Windows Daemon  ──►  本机 agent CLI/存储
```

| 组件 | 信任角色 |
|---|---|
| **手机** | 持有手机共享密钥；可见本窝已配对设备与会话流量 |
| **VPS** | 认证手机与 daemon；中转 WebSocket；**持久化**设备、消息与附件 |
| **家用 PC / Daemon** | 仅出站连接；读取原生 agent 存储；运行无头 CLI |
| **Agent CLI** | 会话/历史权威存储；以用户本机权限执行工具 |

**手机与家用 PC 之间没有端到端加密。** VPS 可读其存储的元数据、消息正文与附件字节。请把 VPS 主机、`data/`、备份与反代日志视为**敏感系统**。

## 影响安全的产品边界

- 手机只**续写** PC 上已有线程，不远程新建 agent 会话。
- Daemon **不需要**家用 PC 入站端口。
- 各 agent **本机原生存储**是发现与历史的权威来源。
- 工具审批依赖各 agent 的**非交互** CLI；受阻时可能需回 PC 终端。

## 密钥与凭据

| 密钥 | 持有方 | 保护对象 |
|---|---|---|
| `NEKONEST_PHONE_SECRET` | 运维 + 手机客户端 | 手机 REST（`Authorization: Bearer` 或 `X-Neko-Secret`）与手机 WS |
| `NEKONEST_BOOTSTRAP_TOKEN` | 运维 + 注册时的 daemon | `POST /api/devices/register`（`X-Neko-Bootstrap`） |
| Daemon `config.json` 中的 `token` | 仅家用 PC | 注册后的 Daemon WebSocket 身份 |
| 6 位配对码 | 短时、由 daemon 打印 | 将手机 UI 绑定到已注册设备 |
| VAPID 密钥 | 运维 | 可选 Web Push |

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
- [ ] systemd（或等价）用专用用户；`data/` 非 world-readable
- [ ] 密钥放在权限受限的 `EnvironmentFile`，勿写进 world-readable unit
- [ ] 防火墙仅 80/443 公网；应用端口（如 8080）仅本机
- [ ] `data/` 备份加密且访问受控
- [ ] Daemon `config.json` ACL 限本机用户
- [ ] 启用 Web Push 时离线生成 VAPID
- [ ] 公网主机无调试/开放注册模式

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
