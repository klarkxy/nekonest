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
3. 在 PC 上打开/使用任一受支持的智能体，产生近期线程
4. 手机进入设备 → 在「目录 → 智能体」下看到对应线程
5. 进入会话，发送一句简短 prompt  
6. 数秒内看到 agent 输出或明确错误提示  
7. 停掉 Daemon → 手机设备变 offline  
8. 再启 Daemon → online  
9. 错误 secret → 401 / 无法操作  

## 已知限制

- 手机端不新建线程，只续写 PC 上已经存在的线程
- 审批能力取决于各智能体的非交互 CLI；不支持时需在 PC 端处理
- 无 Web Push 真推送  
