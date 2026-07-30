> [English](./troubleshooting.md) | 简体中文

# 排障

面向自托管 NekoNest 的症状排查。配置细节：[configuration.zh-CN.md](./configuration.zh-CN.md)。安全背景：[security.zh-CN.md](./security.zh-CN.md)。

## 打不开 PWA / 立即鉴权失败

| 检查 | 期望 |
|---|---|
| URL | 公网 HTTPS 来源（或开发用 loopback） |
| 手机密钥 | 与 `NEKONEST_PHONE_SECRET` 完全一致 |
| 反代 | 具备 WebSocket 升级头（Nginx） |
| Origins | `NEKONEST_ALLOWED_ORIGINS` 含浏览器来源 |
| `/health` | 服务端返回 `{"status":"nyan~"}` |

401 / 无法操作通常是密钥错误或 API 缺少鉴权头。

## Daemon 无法注册

| 检查 | 期望 |
|---|---|
| `NEKONEST_SERVER` | 可达的基址（`https://…` 或本地 `http://…`） |
| `NEKONEST_BOOTSTRAP_TOKEN` | 与公网 VPS 值相同 |
| Server bootstrap | 已设手机密钥时必须设置；否则注册可能被禁用 |
| TLS | 系统信任库接受证书 |
| 时钟 | 不要严重偏移 |

## 设备一直 offline

| 检查 | 期望 |
|---|---|
| Daemon 进程 | 在跑；日志显示已鉴权 device id |
| 第二实例 | 被 `.daemon.lock` 拒绝——同一配置只能一个进程 |
| `config.json` | 有效的 `server_url`、`device_id`、`token` |
| 网络 | PC 可出站 WSS 到 VPS |
| Server | 在线；未崩溃重启循环 |

Daemon 启动后，手机列表应在短重连窗口内变为 online。

## 配对码被拒

| 检查 | 期望 |
|---|---|
| 新码 | 约 5 分钟过期——运行 `-pair gen` |
| 位数 | 6 位；PWA 会规范化输入——避免多余空格/字母 |
| 手机鉴权 | 已用正确手机密钥登录 |
| 同一窝 | 码由注册到**本** Server 的 daemon 签发 |

## 无会话 / 目录树为空

| 检查 | 期望 |
|---|---|
| PC 上已有线程 | 先在 PC **原生** agent 中创建/使用会话 |
| CLI 已安装 | agent 在 PATH；适配器未静默不可用 |
| 原生存储路径 | 用户目录下默认位置（见 README 智能体表） |
| 发现间隔 | daemon 启动后等待数秒 |
| 所有权过滤 | 子代理/sidechain 设计上隐藏 |
| 目录分组 | 无目录者在「**未分类**」 |

手机永不远程新建线程。

## 提示词卡住、busy 或“仍在运行”

| 检查 | 期望 |
|---|---|
| 会话状态 | `running` 时 UI 会阻止重叠发送 |
| Outbox | localStorage 中有 pending `client_msg_id`；重连用**同一** id 重发 |
| Outbox 满 | 上限 40——等 ack |
| Daemon journal | 不确定状态 fail-close 会报错，而非静默成功 |
| CLI 挂起 | UI 支持则 interrupt；否则在 PC 停进程 |

若第一次发送可能已被接受，勿为“重试”手动换新 message id。

## 重连后消息重复或缺失

| 检查 | 期望 |
|---|---|
| 稳定 id | 历史合并用消息 id；服务端/原生追上后应去掉乐观本地消息 |
| SW 更新 | 重大 PWA 升级后可能需完全关闭再开一次 |
| fetch_history | 重新打开线程以同步空/残缺视图 |

## 附件失败

| 检查 | 期望 |
|---|---|
| 数量 / 大小 | ≤ 5 个，每个 ≤ 4 MB |
| MIME | 图片（jpeg/png/webp/gif）、txt、markdown、pdf、json |
| 上传 | 手机密钥有效；`data/attachments` 有磁盘空间 |
| Daemon 下载 | 设备 online；能从 VPS GET 附件 URL |
| Agent 接线 | Claude/Codex/Kilo 用原生文件/图片机制；**Kimi CLI / Grok Build** 仅在提示词中得到本机路径——CLI 沙箱可能禁止读临时目录 |

## 手机上审批完不成

对非交互 CLI 无法承载审批 UX 的 agent 属预期。在 **PC 终端**完成审批，会话 idle 后再用手机继续。

## Web Push 从不到达

| 检查 | 期望 |
|---|---|
| 三个 VAPID 环境变量 | 公钥、私钥、subject 均已设 |
| 浏览器权限 | 已授权；订阅已 POST 到 `/api/push/subscribe` |
| HTTPS | 真机 Push API 需要 |

无 VAPID 时服务端跳过真实推送发送。

## 升级后空白 UI 或旧客户端

1. 完全关闭 PWA / 浏览器标签。  
2. 再开一次以激活 service worker。  
3. 若 worker 卡住可强刷。  
4. 确认 VPS 正在提供新的 `pwa/dist` 资源。  

## Server 起不来 / 绑错接口

| 症状 | 原因 |
|---|---|
| 只在 127.0.0.1 | 未设手机密钥（设计如此） |
| 注册 503 / 禁用 | 设了手机密钥但 bootstrap 为空 |
| 反代限流异常 | 开了 `TRUST_PROXY` 却未覆盖 XFF |

## Windows Defender / 杀软杀掉 daemon

自托管者有时会加路径/进程排除（见 [deploy-windows.zh-CN.md](./deploy-windows.zh-CN.md)）。先理解安全代价。

## 仍然卡住

1. 收集 server systemd 与 daemon 控制台的**非机密**日志。  
2. 验证 `/health` 与设备 online 状态。  
3. 按 [e2e-smoke.zh-CN.md](./e2e-smoke.zh-CN.md) 做到第一个失败点。  
4. 贡献者按 PWA → server → daemon → 适配器端到端追踪（[architecture.zh-CN.md](./architecture.zh-CN.md)）。  

切勿把设备令牌、手机密钥或 bootstrap 粘贴到公开 issue。
