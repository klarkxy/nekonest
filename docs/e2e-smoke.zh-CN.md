> [English](./e2e-smoke.md) | 简体中文

# 端到端冒烟清单

发版或部署敏感变更后的验收路径。切割版本见 [release.zh-CN.md](./release.zh-CN.md)。

## 前置

- [ ] VPS Server 运行；`GET /health` → `{"status":"nyan~"}`
- [ ] 已设置 `NEKONEST_PHONE_SECRET`（公网）
- [ ] 已设置 `NEKONEST_BOOTSTRAP_TOKEN` 并在 daemon 注册时使用
- [ ] 反代后 HTTPS / WSS 可用
- [ ] 建议设置 `NEKONEST_ALLOWED_ORIGINS` 含公网来源
- [ ] 家用 PC 已注册；`config.json` 含真实设备令牌
- [ ] Daemon 进程 online（该配置单实例）
- [ ] PC 上至少一个受支持 agent CLI 有近期主线程会话

## 步骤

1. 手机打开 PWA；输入与 VPS 相同的手机密钥。  
2. 用 6 位配对码绑定（需要时 `-pair gen`）；设备列表显示 **online**。  
3. 在 PC 上打开/使用受支持智能体，产生近期线程。  
4. 手机：设备 → 在「**目录 → 智能体 → 线程**」下可见对应线程。  
5. 进入会话，点回形针；系统文件选择器打开；控件可聚焦 / 约 44px 触摸目标。  
6. 选择一个小于 4 MB 的 PNG 和一个 TXT/Markdown/PDF/JSON；发送要求读取附件的提示词。  
7. 数秒内：agent 正确使用文件内容，**或**出现明确的上传 / 下载 / CLI 错误。  
8. 从旧版首次升级 PWA 时：完全关闭并重新打开一次；此后 SW 更新应能自动刷新一次。  
9. 停掉 Daemon → 手机设备变 **offline**。  
10. 再启 Daemon → 再次 **online**。  
11. 错误手机密钥 → 401 / 无法操作。  
12. 可选：发送提示词后短暂断网再恢复——outbox 不应为同一次发送静默换新 `client_msg_id`。  
13. 可选：若 agent 支持则 interrupt 长任务；确认 Windows 上无残留进程树。  

## 已知限制（不算失败）

- 手机不新建线程；必须 PC 优先  
- 附件最多 5 个、每个 4 MB  
- Codex / Claude Code / Kilo 使用各自 CLI 文件/图片机制  
- Kimi CLI / Grok Build 在提示词中收到 daemon 本地路径；沙箱可能阻止读取  
- 审批可能需要 PC 终端  
- Web Push 需完整 VAPID；否则无真实推送  
- Daemon 面向 Windows  
- VPS 可见并存储消息/附件（无 E2E 加密）  

## 相关

- [排障](./troubleshooting.zh-CN.md)
- [VPS 部署](./deploy-vps.zh-CN.md)
- [Windows 部署](./deploy-windows.zh-CN.md)
