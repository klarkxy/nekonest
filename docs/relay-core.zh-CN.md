> [English](./relay-core.md) | 简体中文

# Relay Core 边界

这是 `relaycore/` 可复用数据面的贡献者说明，不是运维部署指南，也不代表托管服务
已经可发布。

## 边界

`relaycore.Engine` 表示一个猫娘乐园实例，负责自托管 Server 与任何嵌入它的托管
外壳共同使用的转发行为：

- 已鉴权的手机与 Daemon 连接
- 订阅与连接状态
- 提示词持久化状态转换
- open 与 sealed 路由
- 手机/设备授权
- 通过显式端口提供历史、附件和 Push 分发

嵌入外壳负责部署特有内容，例如公网入口、账号身份、租户选择、权益、放置、计费、
存储构造和运维政策。

## 嵌入规则

- 自托管 Server 为自己的数据目录构造一个 Engine。
- 多租户服务必须先选择并授权租户，再让流量进入隔离的 Engine。
- 客户端提供的租户或后端路由值永远不能成为权威。
- Engine 依赖通过公开端口注入；部署细节不进入 Core。

以 `relaycore/` 下导出的 Go API 与测试为准。托管服务合同和发布门禁属于托管服务
仓库，不应进入自托管运维文档。

## 相关文档

- [架构](./architecture.zh-CN.md)
- [协议](./protocol.zh-CN.md)
- `relaycore/ports.go`
- `relaycore/engine_test.go`
