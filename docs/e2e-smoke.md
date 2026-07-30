# 端到端冒烟清单

发版或部署敏感变更时的**验收路径**；维护者切割版本时见 [发版流程](release.md)。

## 前置

- [ ] VPS Server 运行，`/health` 正常  
- [ ] `NEKONEST_PHONE_SECRET` 已设置  
- [ ] HTTPS/WSS 可用  
- [ ] 家 PC 已 `register`，config 含真 token  
- [ ] Daemon 在线  

## 步骤

1. 手机打开 PWA，输入与 VPS 相同的 secret  
2. 用配对码绑定电脑，列表显示 **online**  
3. 在 PC 上打开/使用任一受支持的智能体，产生近期线程
4. 手机进入设备 → 在「目录 → 智能体」下看到对应线程
5. 进入会话，点回形针；确认系统文件选择器打开，按钮可触摸且可用键盘聚焦
6. 选择一个小于 4 MB 的 PNG 和一个 TXT/Markdown/PDF/JSON 文件，提交一句要求读取附件的 prompt
7. 数秒内看到 agent 正确识别文件内容，或看到明确的上传、下载、CLI 执行错误
8. 从旧版首次升级本修复时，完全关闭并重新打开 PWA 一次；此后新版 Service Worker 接管更新时应自动刷新一次
9. 停掉 Daemon → 手机设备变 offline
10. 再启 Daemon → online
11. 错误 secret → 401 / 无法操作

## 已知限制

- 手机端不新建线程，只续写 PC 上已经存在的线程
- 每次最多选择 5 个附件，单个附件不超过 4 MB
- Codex、Claude Code、Kilo 使用各自 CLI 的本地文件授权/附件参数
- Kimi CLI、Grok Build 仅收到提示词中的守护进程本地路径；若 CLI 文件权限不允许读取临时目录，附件会不可用
- 审批能力取决于各智能体的非交互 CLI；不支持时需在 PC 端处理
- Web Push 需配置完整的 VAPID 环境变量；未配置时不会发送真实推送
