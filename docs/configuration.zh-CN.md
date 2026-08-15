> [English](./configuration.md) | 简体中文

# 配置

本页只列出受支持的运维设置。参数以已安装二进制的 `-help` 输出为准；容器部署
以 `compose.yaml` 和 `docker.env.example` 为准。

## Server

常用参数：

| 参数 | 默认值 | 用途 |
|---|---|---|
| `-port` | `8080` | HTTP/WebSocket 监听端口 |
| `-data` | `./data` | SQLite 与附件目录 |
| `-pwa` | `./pwa-dist` | 已构建的 PWA 目录 |
| `-version` | — | 输出应用版本 |

### 公网服务鉴权

| 变量 | 用途 |
|---|---|
| `NEKONEST_ADMIN_SECRET` | 初始管理员凭据，使用长随机值。 |
| `NEKONEST_ADMIN_SECRET_FILE` | 从私有普通文件读取管理员凭据，替代内联变量；两种形式不能同时设置。 |
| `NEKONEST_BOOTSTRAP_TOKEN` | 授权新主机注册，必须与管理员密钥不同。 |
| `NEKONEST_ALLOWED_ORIGINS` | 逗号分隔的浏览器来源，通常就是公网 HTTPS 地址。 |

`NEKONEST_ADMIN_SECRET` 与 `NEKONEST_ADMIN_SECRET_FILE` 二选一，另加
注册令牌与允许来源。未配置管理员密钥时，Server 只监听 loopback，供本地开发
使用。配置了管理员密钥但没有注册令牌时，新主机注册会被禁用。
`NEKONEST_PHONE_SECRET` 只是已弃用的兼容别名，新部署不要使用。

### 可选

| 变量 | 用途 |
|---|---|
| `NEKONEST_TRANSPORT_MODE` | 只在创建新数据目录时选择 `sealed` 或 `open`；以后必须与已保存模式一致。新数据默认 `sealed`。 |
| `NEKONEST_TRUST_PROXY` | 仅当受信反代会覆盖客户端转发头时设为 `1`。 |
| `NEKONEST_TRUSTED_PROXY_CIDRS` | 反代连接不来自 loopback 时，列出可信代理网段。 |
| `NEKONEST_VAPID_PUBLIC_KEY` | Web Push 公钥。 |
| `NEKONEST_VAPID_PRIVATE_KEY` | Web Push 私钥。 |
| `NEKONEST_VAPID_SUBJECT` | Web Push 联系方式，例如 `mailto:operator@example.com`。 |
| `NEKONEST_LOG_FORMAT` | `text`（默认）或 `json`。 |
| `NEKONEST_LOG_LEVEL` | `debug`、`info`（默认）、`warn` 或 `error`。 |

Web Push 的三个 VAPID 值要么全部配置，要么保持关闭。`debug` 只用于有时间
边界的排障。

## 主机 Daemon

常用命令：

| 命令 | 用途 |
|---|---|
| `nekonest-daemon -register -name "Home PC"` | 注册主机并打印配对材料 |
| `nekonest-daemon -pair gen` | 生成新的手机配对码 |
| `nekonest-daemon -doctor` | 检查配置、Server 连通性和已安装智能体 |
| `nekonest-daemon -config <path>` | 使用非默认配置文件 |
| `nekonest-daemon -version` | 输出应用版本 |

注册时读取：

| 变量 | 用途 |
|---|---|
| `NEKONEST_SERVER` | 公网猫娘乐园地址，例如 `https://nekonest.example.com`。 |
| `NEKONEST_BOOTSTRAP_TOKEN` | 与 Server 相同的注册令牌。 |
| `NEKONEST_TRANSPORT_MODE` | 可选断言；设置后必须与 Server 一致。 |

Server 与 Daemon 都支持 `NEKONEST_LOG_FORMAT` 和 `NEKONEST_LOG_LEVEL`。
Daemon 正常运行时使用配置文件内已经保存的凭据，注册用变量无需长期留在启动器
中。

## 数据与备份

| 位置 | 内容 | 备份规则 |
|---|---|---|
| Server 的 `-data` 目录 | 数据库与上传附件 | 停止或静默 Server 后，整体备份。 |
| `~/.nekonest/config.json` | Server 地址与设备凭据 | 保密；Daemon 升级前备份。 |
| `~/.nekonest/identity.json` | 主机身份密钥 | 保密；重新注册或恢复时继续保留。 |
| Daemon 配置旁的文件 | 提示词日志、队列和实例锁 | 与配置一起保留，不要手工编辑。 |

Daemon 默认目录在 Windows 是 `%USERPROFILE%\.nekonest`，Linux 是
`~/.nekonest`。使用自定义 `-config` 时，相关身份与状态保存在该文件旁边。
同一份配置只能由一个 Daemon 进程使用。

不要手改设备 ID、令牌、传输模式、日志或队列文件。需要更换凭据时重新注册。

## 用户可见限制

- 每次提示词最多 5 个附件，每个最多 4 MB。
- 支持 JPEG、PNG、WebP、GIF、TXT、Markdown、PDF 和 JSON。
- 每个智能体最终如何接收附件，仍由它当前声明的能力决定。
- 手机只启用 Daemon 当前明确声明的控制。

## 相关文档

- [VPS 部署](./deploy-vps.zh-CN.md)
- [Windows 主机](./deploy-windows.zh-CN.md)
- [Linux 主机](./deploy-linux.zh-CN.md)
- [安全](./security.zh-CN.md)
- [排障](./troubleshooting.zh-CN.md)
