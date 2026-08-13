> [English](./README.md) | 简体中文

# NekoNest 文档

NekoNest 的现行运维与贡献者文档。产品概览：[../README.zh-CN.md](../README.zh-CN.md) · [../README.md](../README.md)。

英文使用短路径（`docs/foo.md`）；简体中文镜像使用 `.zh-CN.md` 后缀。

## 从这里开始

| 文档 | 说明 |
|---|---|
| [../README.zh-CN.md](../README.zh-CN.md) | 产品介绍、快速开始、**现行 v0.2 边界** |
| [v1-product.zh-CN.md](./v1-product.zh-CN.md) | **v1.0.0 冻结目标合同**（Codex 优先 E2E；超出现行 v0.2 里程碑） |
| [deploy-vps.zh-CN.md](./deploy-vps.zh-CN.md) | 构建并运行 VPS Server |
| [deploy-windows.zh-CN.md](./deploy-windows.zh-CN.md) | 注册并运行 Windows Daemon |
| [deploy-linux.zh-CN.md](./deploy-linux.zh-CN.md) | 注册并运行 Linux Daemon（systemd 用户单元） |
| [migration-v1.zh-CN.md](./migration-v1.zh-CN.md) | v0.1 → v1.0 破坏性离线迁移 |
| [e2e-smoke.zh-CN.md](./e2e-smoke.zh-CN.md) | 部署后验收清单 |
| [troubleshooting.zh-CN.md](./troubleshooting.zh-CN.md) | 症状 → 排查 |

## 参考

| 文档 | 说明 |
|---|---|
| [v1-product.zh-CN.md](./v1-product.zh-CN.md) | 冻结 v1.0.0 目录：Windows+Linux、Codex 全控制、默认密封 |
| [configuration.zh-CN.md](./configuration.zh-CN.md) | 环境变量、flags、配置文件、限额 |
| [security.zh-CN.md](./security.zh-CN.md) | 信任模型、密钥、加固 |
| [agent-capability-matrix.zh-CN.md](./agent-capability-matrix.zh-CN.md) | **分 harness 现行能力矩阵**（Codex / Claude / Kimi / Grok） |
| [architecture.zh-CN.md](./architecture.zh-CN.md) | 组件、发现、提示词路径 |
| [relay-core.zh-CN.md](./relay-core.zh-CN.md) | Relay Core、Standalone 与 Cloud 的边界及稳定端点合同 |
| [protocol.zh-CN.md](./protocol.zh-CN.md) | 线协议信封、消息类型、REST |
| [development.zh-CN.md](./development.zh-CN.md) | 本地开发与测试 |
| [release.zh-CN.md](./release.zh-CN.md) | 维护者发版 |
| [brand-art.zh-CN.md](./brand-art.zh-CN.md) | 重建 PWA 品牌资源 |
| [../CHANGELOG.md](../CHANGELOG.md) | 用户可见历史（英文） |
| [../AGENTS.md](../AGENTS.md) | 工程不变量（英文） |

## 中英对照

| English | 简体中文 |
|---|---|
| [README.md](../README.md) | [README.zh-CN.md](../README.zh-CN.md) |
| [v1-product.md](./v1-product.md) | [v1-product.zh-CN.md](./v1-product.zh-CN.md) |
| [deploy-vps.md](./deploy-vps.md) | [deploy-vps.zh-CN.md](./deploy-vps.zh-CN.md) |
| [deploy-windows.md](./deploy-windows.md) | [deploy-windows.zh-CN.md](./deploy-windows.zh-CN.md) |
| [e2e-smoke.md](./e2e-smoke.md) | [e2e-smoke.zh-CN.md](./e2e-smoke.zh-CN.md) |
| [configuration.md](./configuration.md) | [configuration.zh-CN.md](./configuration.zh-CN.md) |
| [security.md](./security.md) | [security.zh-CN.md](./security.zh-CN.md) |
| [agent-capability-matrix.md](./agent-capability-matrix.md) | [agent-capability-matrix.zh-CN.md](./agent-capability-matrix.zh-CN.md) |
| [architecture.md](./architecture.md) | [architecture.zh-CN.md](./architecture.zh-CN.md) |
| [relay-core.md](./relay-core.md) | [relay-core.zh-CN.md](./relay-core.zh-CN.md) |
| [protocol.md](./protocol.md) | [protocol.zh-CN.md](./protocol.zh-CN.md) |
| [development.md](./development.md) | [development.zh-CN.md](./development.zh-CN.md) |
| [troubleshooting.md](./troubleshooting.md) | [troubleshooting.zh-CN.md](./troubleshooting.zh-CN.md) |
| [release.md](./release.md) | [release.zh-CN.md](./release.zh-CN.md) |
| [brand-art.md](./brand-art.md) | [brand-art.zh-CN.md](./brand-art.zh-CN.md) |
| [README.md](./README.md)（本索引） | [README.zh-CN.md](./README.zh-CN.md) |

## 归档与合同

- **现行 v0.2 产品边界：** [../README.zh-CN.md](../README.zh-CN.md) 中的「当前边界（v0.2）」与今日运维指南。
- **目标 v1.0.0 产品合同：** [v1-product.zh-CN.md](./v1-product.zh-CN.md) — 朝完整版推进时，以它为准，不再沿用 v0.x 妥协。
- **冻结施工快照：** [archive/](./archive/)。**不是**产品合同。引用前请对照当前代码、README、AGENTS.md，以及（v1 工作）`v1-product.zh-CN.md`。
