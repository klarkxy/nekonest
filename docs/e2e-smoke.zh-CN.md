> [English](./e2e-smoke.md) | 简体中文

# 端到端冒烟清单

部署或部署敏感变更后的验收路径。产品目标：[v1-product.zh-CN.md](./v1-product.zh-CN.md)。发版：[release.zh-CN.md](./release.zh-CN.md)。迁移：[migration-v1.zh-CN.md](./migration-v1.zh-CN.md)。

## 模式

| 模式 | 环境变量 | 何时用 |
|---|---|---|
| **开放（建议先跑）** | server 与 daemon 均 `NEKONEST_TRANSPORT_MODE=open`；PWA 默认 open | 密封联调完成前的日常 |
| **密封** | server/daemon `sealed`；PWA `VITE_NEKONEST_TRANSPORT_MODE=sealed` | QR 配对 + key package 成功后 |

一窝一种模式。不匹配则拒绝握手（禁止 sealed→open 自动降级）。

## 前置条件（开放模式）

- [ ] VPS 已运行；`GET /health` 返回 `status=nyan~` 以及预期的 `server_version` / `protocol_version`
- [ ] 已设 `NEKONEST_ADMIN_SECRET`（或旧名 `NEKONEST_PHONE_SECRET`）
- [ ] 已设 `NEKONEST_BOOTSTRAP_TOKEN`，注册 daemon 时使用
- [ ] server **与** daemon 均为 `NEKONEST_TRANSPORT_MODE=open`
- [ ] 反代 HTTPS / WSS 正常
- [ ] `NEKONEST_ALLOWED_ORIGINS` 含公网 Origin（推荐）
- [ ] 主机已 `nekonest-daemon -register`；`config.json` 有设备 token
- [ ] `nekonest-daemon -doctor` 关键检查通过（或缺 CLI 的预期失败）
- [ ] Daemon 在线（该配置单实例）
- [ ] 主机上至少一个支持的 agent 有近期主线程会话

## A. 开放模式核心路径

1. 打开 PWA，输入窝 admin 密钥。  
2. 配对：主机 `nekonest-daemon -pair gen`；粘贴 **QR JSON**（推荐）或 6 位码；与 PC 屏幕核对 **指纹**。  
3. 设备列表显示主机 **在线**；页面顶部的网页 / Server 版本一致，每张设备卡片显示该机器自己的 Daemon 版本。故意使用旧 PWA 时显示“立即刷新”，只有旧 Daemon 所在机器提示更新。
4. 主机侧打开/使用 agent，确保有近期线程。  
5. 手机：**目录 → agent → 线程** 可见；有能力宣告时可见 capabilities。  
6. 打开线程；历史加载；发短 prompt；出现流式输出。  
7. 投递 UX：发件箱走向 **committed**（不只是 WS 写出成功）。  
8. 附件（可选）：小 PNG + 文本；agent 读到或明确报错。  
9. 若会话宣告 `interrupt`：打断长任务；进程树不残留。  
10. 停 daemon → **离线**；再启 → **在线**。  
11. 错误密钥 / 已撤销 phone token → 401 / 无法操作。  
12. 发送中断网重连：同一 `client_msg_id`；agent 不双跑。

## B. Codex 控制路径（本机有 CLI 时）

开发基线：**codex-cli 0.144.1** + `codex app-server`。

1. `nekonest-daemon -doctor` 打印 app-server `available` / `ensure`。  
2. 若能力为 `control_mode=app_server` 且 `approve=true`：主机触发真审批 → 手机批准/拒绝生效。  
3. 若 `steer=true`：中途 steer 生效。  
4. 对每个宣告 `spawn=true` 的 agent：`start_thread` 只能进入 daemon **当前已发现**的原生项目目录并集；生命周期 `thread_starting → thread_owned | failed | indeterminate`；无幽灵 nest 行。
5. app-server 不健康时：Codex 保持 `exec_resume`（仅发送/历史/中断）；不假冒 approve/spawn。

## C. 密封模式（第二轮可选）

1. server + daemon + PWA 均设 sealed 并重启。  
2. 用 QR JSON 重新配对，保证 wrap key 一致。  
3. 确认窝侧 DB/日志无新密封流量的 prompt 明文。  
4. key_package 到达后聊天仍可用。  
5. 开放客户端连密封窝（或反过来）被 **拒绝**。

## D. 迁移冒烟（从 v0.1 升级时）

1. 停写入；`nekonest-server -migrate-v1 -data ./data -backup ./data-backup-v1`。  
2. 设备 token 仍可认证；线上库旧明文消息已清除。  
3. 手机重新登录并重新配对。

## 已知限制（不算失败）

- 密封默认是**产品目标**；验收前可用 open  
- Codex app-server 方法名随 CLI 版本变化 — doctor 显示可用时个别 method 仍可能失败  
- 非 Codex：仅兼容续接（不承诺审批/开线程/steer）  
- 附件上限 5 个、各 4 MB（开放路径）  
- Web Push 需 VAPID；密封推送正文保持通用  
- 正式主机：Windows + Linux；macOS 更后  
- 开放模式：VPS 可读应用明文  

## 相关

- [故障排查](./troubleshooting.zh-CN.md)
- [VPS 部署](./deploy-vps.zh-CN.md)
- [Windows 部署](./deploy-windows.zh-CN.md)
- [Linux 部署](./deploy-linux.zh-CN.md)
- [安全](./security.zh-CN.md)
- [v1 产品合同](./v1-product.zh-CN.md)
