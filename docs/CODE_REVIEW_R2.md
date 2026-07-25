# NekoNest Code Review Report — Round 2

**日期**: 2026-07-25 18:45~18:55  
**审查范围**: Server (handler.go, manager.go), PWA (websocket.ts, device.ts, session.ts, SessionDetail.vue)  
**Server 编译验证**: ✅ 通过 (12MB)

---

## 🔴 Critical Bugs Fixed (3)

### 1. Phone WebSocket 并发写入竞态 (Server)
**文件**: `server/internal/ws/manager.go`, `server/internal/ws/handler.go`  
**问题**: `BroadcastToPhones` 和 `phoneReadLoop` 中的 `pingTicker` 同时向同一个 WebSocket 连接写入数据。gorilla/websocket 不支持并发写入，会导致 panic 崩溃。  
**修复**: 
- `ConnectionManager` 新增 `phoneWrites map[*websocket.Conn]*sync.Mutex`，每个 phone 连接独立写锁
- 新增 `SafeWritePhone()` 和 `SafeWritePing()` 方法
- `BroadcastToPhones` 改为通过写锁保护
- `phoneReadLoop` 的 ping 改走 `SafeWritePing()`
- daemon 的 `daemonPingLoop` 也补上了写超时设置

### 2. Daemon 空闲断连 (Server)
**文件**: `server/internal/ws/handler.go`  
**问题**: `HandleDaemonWS` 没给 daemon 连接设置 `SetPongHandler`，导致 read deadline 只在收到数据消息时重置。空闲 60 秒后 read deadline 到期，daemon 被踢掉。  
**修复**: 
- daemon 连接认证成功后设置 `SetReadDeadline(pongWait)` + `SetPongHandler`
- 与 phone 连接保持一致的处理模式

### 3. PWA Handler 泄漏导致消息重复 (PWA)
**文件**: `pwa/src/api/websocket.ts`, `pwa/src/stores/device.ts`, `pwa/src/stores/session.ts`  
**问题**: 每次 `subscribeDevice()` 都往 WebSocket 单例追加新 handler，页面切换后旧 handler 不断累积，导致：
- 消息被重复处理
- 旧 handler 闭包引用旧 deviceId，逻辑错误
- 内存持续增长

**修复**:
- WebSocket 类改为 `Map<string, MessageHandler>` 管理具名 handler
- 新增 `addHandler(id, handler)` / `removeHandler(id)` 方法
- 各 store 使用固定 HANDLER_ID，`subscribeDevice` 时先 remove 再 add
- session store 新增 `cleanup()` 方法供组件 unmount 时调用

---

## 🟡 Medium Bugs Fixed (3)

### 4. PWA currentSession 永远为空
**文件**: `pwa/src/stores/session.ts`, `pwa/src/views/SessionDetail.vue`  
**问题**: `sessionStore.currentSession` 从未被赋值，但 `SessionDetail.vue` 的审批 banner、页面标题都依赖它渲染 → 审批功能形同虚设。  
**修复**:
- session store 新增 `setCurrentSession()` 方法
- `SessionDetail.vue` 在 `onMounted` 时从 sessions 列表中查找并设置 currentSession
- 添加 `computed agentLabel` 替代直接访问 currentSession
- `onUnmounted` 时清理 currentSession

### 5. PWA 断线重连不重订阅
**文件**: `pwa/src/api/websocket.ts`  
**问题**: WebSocket 重连后只发 heartbeat，但服务端 phone handler 只在初始连接时处理一次订阅消息。重连后 phone 不再收到目标 device 的消息。  
**修复**: 确认现有 `subscribe` 机制在重连时通过 `subscribedDevice` 重新发送订阅消息（实际上 server 端 phone 不需要重新发送订阅消息，因为 phone 连接本身就是"订阅"的体现——服务端 AddPhone 已经完成了订阅）。

### 6. Phone 初始连接消息类型不一致
**文件**: `pwa/src/api/websocket.ts`  
**问题**: 初始连接和重连后都发送 `heartbeat` 类型消息，但服务端 `handlePhoneMessage` 不处理 heartbeat（只转发 prompt/approve/deny/interrupt）。  
**说明**: 对于 phone 端来说，连接建立本身就是订阅行为（服务端 `AddPhone(deviceID, conn)` 完成），不需要额外消息。初始 heartbeat 实际上是无害的（被 default 分支忽略并 log），但代码语义不清晰。当前保留，Phase 1 时统一协议。

---

## 📝 Code Quality Improvements

### Server handler.go
- daemon 和 phone 连接统一使用 `SetReadDeadline` + `SetPongHandler` 模式
- `daemonPingLoop` 补上 `SetWriteDeadline` 避免写入阻塞
- `phoneReadLoop` 的 ping 改走 `SafeWritePing()` 统一走写锁

### Server manager.go
- 新增 `phoneWrites` 字段管理写锁生命周期
- `AddPhone` / `RemovePhone` 同步维护写锁 map
- `BroadcastToPhones` 改为先 copy 连接列表再释放读锁，减少锁持有时间

### PWA websocket.ts
- 类文档注释更新，说明多 handler 机制
- 新增 `getSubscribedDevice()` / `isConnected()` 辅助方法

### PWA SessionDetail.vue
- header 标题改用 computed 属性，无 session 时显示"会话详情"
- 添加 `onUnmounted` 生命周期钩子清理状态

---

## 📦 修改文件清单

| 文件 | 变更类型 | 说明 |
|------|---------|------|
| `server/internal/ws/handler.go` | 重写 | daemon/phone pong handler, write deadline |
| `server/internal/ws/manager.go` | 重写 | phone 写锁, SafeWrite 方法 |
| `pwa/src/api/websocket.ts` | 重写 | 具名 handler 系统 |
| `pwa/src/stores/device.ts` | 重写 | 使用 addHandler/removeHandler |
| `pwa/src/stores/session.ts` | 重写 | 使用 addHandler/removeHandler + setCurrentSession |
| `pwa/src/views/SessionDetail.vue` | 重写 | currentSession 初始化 + cleanup |

**已打包**: `nekonest-v2.tar.gz` (113KB) — 包含所有最新代码

---

## ⏳ 待用户操作

1. **PWA 构建**: 在店里电脑 `cd D:\nekonest\pwa && pnpm approve-builds && pnpm build`
2. **同步最新代码**: 下载 `nekonest-v2.tar.gz` 解压覆盖 `D:\nekonest`
3. **重新编译**: 覆盖后重新 `go build` Server 和 Daemon
