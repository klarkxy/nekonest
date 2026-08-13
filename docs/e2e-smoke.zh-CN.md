> [English](./e2e-smoke.md) | 简体中文

# 端到端冒烟清单

部署或部署敏感变更后的验收路径。产品目标：[v1-product.zh-CN.md](./v1-product.zh-CN.md)。发版：[release.zh-CN.md](./release.zh-CN.md)。迁移：[migration-v1.zh-CN.md](./migration-v1.zh-CN.md)。

本清单必须针对**更新后的线上构建**执行，不能只测本地 Mock 或部署前二进制。对于当前
维护猫娘乐园的运行时改动，只要任务没有明确要求 local-only / no-deploy，部署并完成下列
适用检查才算最终验收。替换前记录确切提交与产物哈希并保留回滚副本；证据至少覆盖
公网健康、daemon 重连和此次修改的真实用户路径。部署本身不代表有权打 tag 或发布
GitHub Release。

## 模式

| 模式 | 环境变量 | 何时用 |
|---|---|---|
| **密封（新乐园默认）** | 新 DB 的 Server 不设模式；Daemon 注册时持久化返回值；PWA 读取 `/health` | QR 配对 + key package 后的正常新安装 |
| **开放（管理员选择 / 旧乐园）** | 只在首次创建 open DB 时设 `NEKONEST_TRANSPORT_MODE=open`，或保留现有 open DB/config | 接受 VPS 可见明文的可信中继环境 |

每个乐园只有一种持久化模式。初始化后环境/构建覆盖只作为断言；不匹配则拒绝启动/连接（禁止 sealed→open 自动降级）。

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

1. 打开 PWA，输入乐园 admin 密钥。
2. 配对：主机 `nekonest-daemon -pair gen`；粘贴 **QR JSON**（推荐）或 6 位码；与 PC 屏幕核对 **指纹**。  
3. 设备列表显示主机 **在线**；页面顶部的网页 / Server 版本一致，每张设备卡片显示该机器自己的 Daemon 版本。故意使用旧 PWA 时显示“立即刷新”，只有旧 Daemon 所在机器提示更新。
4. 主机侧打开/使用 agent，确保有近期线程。  
5. 刷新手机 PWA：先显示缓存目录，在线 daemon 随即重扫并回推最新的 **目录 → agent → 线程**。只有有线团的 agent 显示为分组；可新建但当前没有线团的 agent 收在目录的“新建”菜单中。有能力宣告时可见 capabilities。
6. 打开线程；历史加载；发短 prompt；出现流式输出。  
7. 投递 UX：发件箱走向 **committed**（不只是 WS 写出成功）。  
8. 附件（可选）：小 PNG + 文本；agent 读到或明确报错。  
9. 若会话宣告 `interrupt`：打断长任务；进程树不残留。  
10. 停 daemon → **离线**；再启 → **在线**。  
11. 错误密钥 / 已撤销 phone token → 401 / 无法操作。  
12. 发送中断网重连：同一 `client_msg_id`；agent 不双跑。

## B. Codex 控制路径（本机有 CLI 时）

全控制基线：**codex-cli 0.146.0+** + `codex app-server`。

1. `nekonest-daemon -doctor` 报告已安装/最低版本，并探测 initialize、thread/start、turn/start、steer、interrupt、审批决定形状与 requestUserInput 字段。
2. 若能力为 `control_mode=app_server` 且 `approve=true`：主机触发真审批 → 手机批准/拒绝生效。  
3. 在 Codex Plan-mode 线程触发 `requestUserInput`：分别回答选项、Other/自由文本、Secret；倒计时过期后禁用提交，stale/不确定请求不自动重答。本轮不新增 Plan mode 选择器。
4. 运行长回合时用主发送按钮排两条，确认 FIFO 顺序；取消未开始项；中断/失败后队列暂停并由用户恢复。Steer 是独立动作且只修改当前回合。
5. 用一张图片与一个普通文件新建原生线程；二者必须进入同一次首个 `turn/start`，仅在首回合接受且原生 store 认领后跳转。
6. 工作中杀掉 app-server：能力立即降级、会话 error、队列暂停、发送通用失败事件；有界重启后恢复能力，绝不重放结果不明的旧回合/请求。
7. 每个宣告 `spawn=true` 的 agent 只能进入 daemon **当前已发现**的原生项目目录并集；无幽灵 nest 行。
8. 低于 0.146.0 或方法探测失败时保持 `exec_resume`，不得假冒审批、问答、队列、steer、普通文件或 spawn。

## C. 密封模式与通知

1. 用全新数据目录且不设模式；`/health.transport_mode` 必须为 `sealed`。注册/重配，使 Daemon config 与 wrap key 一致。
2. 配置真实 VAPID 并让手机订阅。触发审批、结构化问答、失败与完成；Push 文字保持通用，深链进入目标会话后才解密详情。
3. 覆盖发送/重连和排队重试；同一 `client_msg_id` 必须重放完全相同的 sealed 信封（nonce/ciphertext/AAD 不变）。
4. 用唯一字符串扫描 Server DB/日志：提示词、答案、审批细节、附件名/路径、工具正文都不得出现明文。
5. 已有 open DB 升级后仍报告 open；Server 环境、Daemon config 与 PWA 构建的 open/sealed 任意不匹配均被 **拒绝**。

## D. 迁移冒烟（从 v0.1 升级时）

1. 停写入；`nekonest-server -migrate-v1 -data ./data -backup ./data-backup-v1`。  
2. 设备 token 仍可认证；线上库旧明文消息已清除。  
3. 手机重新登录并重新配对。

## 已知限制（不算失败）

- Codex app-server 由 0.146.0 最低版本与实时 schema/initialize 探测共同门控
- 非 Codex：仅兼容续接（不承诺审批/steer/队列；仅当 `start_capabilities.spawn=true` 时可 `start_thread`）— 见 [agent-capability-matrix.zh-CN.md](./agent-capability-matrix.zh-CN.md)
- 附件上限 5 个、各 4 MB（开放路径）  
- Web Push 需 VAPID；密封推送正文保持通用  
- 正式主机：Windows + Linux；macOS 更后  
- 开放模式：VPS 可读应用明文  

## 协议 1.2 能力验收

1. 抓取实时与重连后的 `session_list`，确认两者都保留 Daemon 生产者版本，并显式包含全部布尔能力与 `unavailable_reasons`。
2. 对隔离的 1.1 fixture 确认旧版发送/中断仍可用；移除生产者版本，或使用缺字段的 1.2，控件必须保持关闭。
3. 在每条可靠且已安装路径排入两条提示词：成功按 FIFO 自动前进；失败/中断暂停后续；重启把未确认 running 项变成 `blocked_indeterminate`；显式跳过后继续且不重放该 `client_msg_id`。
4. 原生开线程分别覆盖首提示词成功/失败 × 所有权有/无四象限；只有双正向可成为 `thread_owned`，长首轮不得被 PWA 计时器终止。
5. 维护中的生产乐园保持其已持久传输模式；sealed 验收只使用隔离的新数据目录，并扫描 Server 数据库/日志，确认无提示词、响应、路径、审批或附件明文。

## 相关

- [故障排查](./troubleshooting.zh-CN.md)
- [VPS 部署](./deploy-vps.zh-CN.md)
- [Windows 部署](./deploy-windows.zh-CN.md)
- [Linux 部署](./deploy-linux.zh-CN.md)
- [安全](./security.zh-CN.md)
- [v1 产品合同](./v1-product.zh-CN.md)
