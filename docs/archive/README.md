# 归档（非现行合同）

本目录保存已经收起的历史文档，用来核对旧决策。
今天的产品说明、部署指南和验收标准在上一层 `docs/`。

现行来源从 [docs/README.md](../README.md) / [docs/README.zh-CN.md](../README.zh-CN.md) 进入。

## 已归档的过期合同与未执行迁移

这些文件描述的是尚未打出的 v1.0.0 标签，或只适用于仍停在 v0.1 数据目录的迁移。
现行产品是 **v0.2**。不要按这里的「还必须做成什么」去改活文档或代码，除非维护者
明确重新启动 v1.0.0 发版工作。

| 文档 | 说明 |
|---|---|
| [v1-product.md](./v1-product.md) · [v1-product.zh-CN.md](./v1-product.zh-CN.md) | 冻结的 v1.0.0 目标合同快照 |
| [migration-v1.md](./migration-v1.md) · [migration-v1.zh-CN.md](./migration-v1.zh-CN.md) | v0.1 → v1.0 破坏性离线迁移；现行 v0.2 乐园不要跑 |

## 施工快照

早期施工与多代理交付阶段的冻结记录。

| 文档 | 说明 |
|---|---|
| [PHASE1_REPORT.md](./PHASE1_REPORT.md) | 第一阶段施工报告 |
| [CODE_REVIEW_R2.md](./CODE_REVIEW_R2.md) | 第二轮代码评审记录 |
| [multi-agent-plan.md](./multi-agent-plan.md) | 多代理交付计划 |
| [multi-agent-todo.md](./multi-agent-todo.md) | 多代理待办清单 |

## 现行来源

| 主题 | 英文 | 简体中文 |
|---|---|---|
| 产品边界与快速开始 | [`README.md`](../../README.md) | [`README.zh-CN.md`](../../README.zh-CN.md) |
| 文档索引 | [`docs/README.md`](../README.md) | [`docs/README.zh-CN.md`](../README.zh-CN.md) |
| 工程约定 | [`AGENTS.md`](../../AGENTS.md) | （仅英文） |
| VPS / Windows / Linux 部署 | [`deploy-vps`](../deploy-vps.md) · [`deploy-windows`](../deploy-windows.md) · [`deploy-linux`](../deploy-linux.md) | 对应 `*.zh-CN.md` |
| 配置 / 安全 / 架构 / 能力矩阵 | [`configuration`](../configuration.md) · [`security`](../security.md) · [`architecture`](../architecture.md) · [`agent-capability-matrix`](../agent-capability-matrix.md) | 对应 `*.zh-CN.md` |
| 验收 / 发版 | [`e2e-smoke`](../e2e-smoke.md) · [`release`](../release.md) · [`CHANGELOG.md`](../../CHANGELOG.md) | 对应 `*.zh-CN.md` |

引用本目录中的结论前，请对照当前代码与上述活文档核实。
