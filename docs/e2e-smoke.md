# 端到端冒烟清单

## 前置

- [ ] VPS Server 运行，`/health` 正常  
- [ ] `NEKONEST_PHONE_SECRET` 已设置  
- [ ] HTTPS/WSS 可用  
- [ ] 家 PC 已 `register`，config 含真 token  
- [ ] Daemon 在线  

## 步骤

1. 手机打开 PWA，输入与 VPS 相同的 secret  
2. 用配对码绑定电脑，列表显示 **online**  
3. 在 PC 上打开/使用 Claude Code，产生近期会话  
4. 手机进入设备 → 会话列表出现对应 session  
5. 进入会话，发送一句简短 prompt  
6. 数秒内看到 agent 输出或明确错误提示  
7. 停掉 Daemon → 手机设备变 offline  
8. 再启 Daemon → online  
9. 错误 secret → 401 / 无法操作  

## 已知限制

- 新建会话：实验性，可能失败  
- 审批：仅当 agent 进程仍在跑且吃 stdin `y`/`n` 时有效  
- 无 Web Push 真推送  
