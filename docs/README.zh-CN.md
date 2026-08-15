> [English](./README.md) | 简体中文

# NekoNest 文档

这里是当前自托管产品的现行文档。历史目标合同与未使用的迁移说明冻结在
[archive/](./archive/) 中。

## 按目标阅读

| 目标 | 阅读顺序 |
|---|---|
| 了解并试用 NekoNest | [项目 README](../README.zh-CN.md) |
| 安装公网猫娘乐园 | [VPS](./deploy-vps.zh-CN.md) → [Windows 主机](./deploy-windows.zh-CN.md) 或 [Linux 主机](./deploy-linux.zh-CN.md) → [验收](./e2e-smoke.zh-CN.md) |
| 维护已有实例 | [配置](./configuration.zh-CN.md)、[安全](./security.zh-CN.md)和[排障](./troubleshooting.zh-CN.md) |
| 修改项目 | [本地开发](./development.zh-CN.md)、[架构](./architecture.zh-CN.md)和[协议](./protocol.zh-CN.md) |
| 发布版本 | [发版流程](./release.zh-CN.md) |

## 用户与运维指南

| 文档 | 用途 |
|---|---|
| [VPS 部署](./deploy-vps.zh-CN.md) | 在 HTTPS 后运行 Server 与 PWA |
| [Windows 主机](./deploy-windows.zh-CN.md) | 注册主机并用 `nekonest-daemon install` 管理自启动 |
| [Linux 主机](./deploy-linux.zh-CN.md) | 注册主机并用 `nekonest-daemon install` 管理 systemd 用户单元 |
| [配置](./configuration.zh-CN.md) | 受支持的运维设置与数据位置 |
| [安全](./security.zh-CN.md) | 信任模型、密钥、备份与加固 |
| [排障](./troubleshooting.zh-CN.md) | 按症状恢复 |
| [验收清单](./e2e-smoke.zh-CN.md) | 验证新安装或升级后的实例 |
| [智能体支持](./agent-capability-matrix.zh-CN.md) | 稳定支持级别与运行时能力规则 |

## 贡献者与维护者参考

| 文档 | 用途 |
|---|---|
| [架构](./architecture.zh-CN.md) | 稳定的组件与数据所有权边界 |
| [协议](./protocol.zh-CN.md) | 兼容规则与权威 schema 位置 |
| [本地开发](./development.zh-CN.md) | 本地环境与验证命令 |
| [发版](./release.zh-CN.md) | 维护者发布门禁 |
| [Relay Core](./relay-core.zh-CN.md) | 自托管与托管服务共享的数据面边界 |
| [品牌资源](./brand-art.zh-CN.md) | 从已批准原图重建发布资源 |

[知乎介绍稿](./zhihu-intro.zh-CN.md) 是发布文案，不是运维参考。

## 事实来源

| 主题 | 权威来源 |
|---|---|
| 产品行为 | 根 README 与正在运行的界面 |
| 可用控制 | PWA 能力状态与 `nekonest-daemon -doctor` |
| Server/Daemon 参数 | 已安装二进制的 `-help` 输出 |
| 容器设置 | `compose.yaml` 与 `docker.env.example` |
| 线上字段与消息 | `protocol/protocol.json` |
| 发布自动化 | `.github/workflows/release.yml` |

文档说明如何使用这些入口，不复制每个内部字段和实现分支。英文文件是主版本；
面向运维的变更必须在同一改动中更新对应的 `.zh-CN.md` 镜像。
