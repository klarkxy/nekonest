> [English](./relay-core.md) | 简体中文

# Relay Core 与托管端点边界

本决策记录定义自部署 Server 和 NekoNest Cloud 共同复用的中转边界。它不把托管服务列为 NekoNest v1.0.0 的发布前提，也不代表 Cloud 已经具备公开上线证据。

## 决策

一个 `relaycore.Engine` 只代表一个 Nest；同一个 Nest 可以包含多台经过明确授权的主机。开放的自部署 Server 只创建一个 Engine；私有区域 Cloud Relay 为每个租户创建相互隔离的 Engine，租户选择始终发生在 Engine 之外。

```text
自部署：Daemon / PWA -> Standalone 外壳 -> 单个 Engine -> 本地存储
托管：  Daemon / PWA -> 稳定入口 -> 租户注册表 -> 每租户一个 Engine
                                  ^
                                  | 短期签名授权
                              Cloud 控制面
```

Core 负责线协议、WebSocket 生命周期、连接策略、订阅状态、Prompt 持久化状态流转、sealed 信封路由、手机/设备授权、历史、附件和 Push 调度。存储、认证、附件、Push、审计和时钟使用明确端口或开放适配器。账号身份、价格、订阅、主机席位、区域、placement、D1 和 Cloudflare 不得进入开放 Core。

## 统一服务端点

Daemon 只持久化一个通用 `server_url`，始终连接该 origin 的 `/ws/daemon`。自部署地址指向 Standalone Server；官方地址指向稳定 Cloud 入口。注册可以返回 `connection_state=provisioning`，但不能返回替换用 Relay URL，Daemon 也不再轮询另一个控制面 origin。

经过批准的可重试服务状态会持续重试同一个端点，直到服务就绪或 Daemon 被停止；
它们不消耗通用网络故障的有限重试预算。结构化终止错误或未知错误仍会失败关闭。
非 101 WebSocket 响应以相同的服务错误结构做有界解析，并且绝不跟随重定向。

v0.2.5 自部署配置保持有效。尚未发布的 `control_plane_url` / `activation_poll_path` 配置会收到明确的“重新注册”错误，不能被静默改写。

协议 1.3 增加统一错误载荷：

```json
{
  "error_code": "service_provisioning",
  "message": "Relay placement is not ready",
  "retryable": true,
  "retry_after_seconds": 15
}
```

未知错误码按安全失败处理。可选 `action_url` 只用于展示，不能成为认证或路由依据。

## 托管隔离与商业边界

- 一个 Cloud 账号对应一个租户/Nest。主机席位限制未撤销的主机身份，不限制浏览器标签页或本机 agent session 数量。
- 控制面独占账号、权益、placement 和设备撤销权威，并向 Relay 节点下发签名授权快照；Core 只接收 principal 以及允许、拒绝、撤销结果。
- 永不信任客户端提交的 tenant id。设备凭证或不具备授权能力的手机 route handle 必须先解析出租户，之后请求才进入对应 Engine。
- 首版每租户使用独立 SQLite 与附件目录；不支持 SQLite active-active、跨区域共享文件或双写。
- Cloud 对同一在线 Daemon 身份采用 `reject_new`。复制同一凭证无法证明是第二台物理机器，但不能同时建立两个连接，也不能意外产生两个身份。被拒绝的 Daemon 在同一端点重试，直到旧租约过期。
- 托管模式强制 sealed。Cloud 登录只能引导创建独立手机身份，不能自动创建手机到设备的 grant；每台设备仍需完成既有 E2E 配对。

## 可用性与撤销

授权快照使用 Ed25519 签名，最长五分钟有效，每分钟完整刷新，并至少每 15 秒核对 authorization revision。新 principal 必须获得控制面实时决策。控制面故障时，已认证连接最多维持到当前快照过期；过期后关闭租户连接。正常设备撤销目标是在 15 秒内断开，不等待快照自然到期。

租户固定 home region。区域迁移是单写、generation fenced 的流程：停止新写入，复制并校验 SQLite 与附件，原子切换 placement，然后排空旧节点。客户端永远看不到后端节点 URL，也不存在自动 active-active 切换。

## 发布门禁

本地单元测试只能证明合同，不能证明托管服务。公开收费之前必须完成真实稳定入口、节点身份与轮换、跨租户负向测试、备份恢复、区域迁移/回滚、账号永久擦除、保留政策、监控值守，以及基于同一精确构建的线上端到端证据。
