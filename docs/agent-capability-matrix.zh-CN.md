> [English](./agent-capability-matrix.md) | 简体中文

# 智能体支持与运行时能力

NekoNest 根据已安装的智能体路径检测能力。本页定义稳定的支持政策，不维护随
版本变化的逐项控制矩阵。

## 支持级别

| 智能体 | 稳定角色 |
|---|---|
| Claude Code | 兼容继续原生线程 |
| Codex | 通过 `codex app-server` 完整控制，并提供兼容回退 |
| Kimi CLI | 兼容继续原生线程 |
| Grok Build | 兼容继续原生线程 |
| ZCode | 当前不可用。现行目录不得声明发现、发送或新建 |
| Cursor | 仅在已安装 Cursor Agent CLI 时兼容继续原生线程 |

兼容继续涵盖发现、原生所有权、历史、提示词执行/流式输出、中断和附件，但只有
已安装路径明确声明时才可用。它不承诺手机审批、转向、结构化输入、队列、新建
线程或某种固定附件方式。

Cursor 无头继续使用 print/`stream-json`，并带上 `--force` 与 `--trust`，避免
CLI 停在本机审批提示；手机审批/拒绝仍然不受支持。发现读取
`~/.cursor/projects/*/agent-transcripts/<id>/<id>.jsonl`，以及存在时的
`~/.cursor/chats`。像 `d-0-code-nekonest` 这样的工作区 slug 会在可能时还原成
主机上已有的目录。路径附件使用 `--add-dir`。协议仍可解析 `zcode`，但现行目录不声明它：上游
`zcode login` 会因 `oauth/cli/init` 返回 404 而报
`OAuth response is not valid JSON`，无头发送/新建因此无法得到
`~/.zcode/cli/config.json`。

Codex 是唯一受支持的完整控制角色。即使是 Codex，也只有在已安装 app-server
路径通过运行时探测后，PWA 才会启用对应控制；回退路径可能只提供较小能力集。

Codex 的结构化问题只在用户明确启用的**规划模式**回合中可用。PWA 仍以普通执行
为默认值，因为规划模式用于制定计划和询问决策，而不是直接完成实现工作。

## 以运行时为准

PWA 对每个线程使用 Daemon 当前的能力目录。缺失或未知标志一律视为不支持，
从而安全处理混合版本安装，以及独立于 NekoNest 变化的智能体 CLI。

控制不可用时：

1. 阅读 PWA 显示的原因。
2. 在主机运行 `nekonest-daemon -doctor`。
3. 确认目标智能体 CLI 与 Daemon 属于同一个系统用户。
4. 必要时更新或修复该 CLI，再重连 Daemon。

不要依据本页绕过界面门禁。

## 稳定保证

- 原生智能体存储是发现、历史与所有权的事实来源。
- 一个智能体缺失或损坏不会禁用其他智能体。
- 只有所选适配器证明原生所有权后，线程才能被路由。
- 不从转录文本猜测智能体请求；审批和输入界面必须来自当前原生信号。
- 新建线程只允许已发现项目，且仅在所选原生启动器明确声明后出现。
- 不支持的控制会说明原因，不会静默无效。
- 智能体 stderr 是诊断信息，不是助手正文。

## 不在范围内

NekoNest 不提供任意文件系统浏览、只存在于猫娘乐园的通用会话、模拟审批或猜测
转向。如果兼容 CLI 需要它无法无头暴露的交互，请在主机终端完成。

## 相关文档

- [排障](./troubleshooting.zh-CN.md)
- [验收清单](./e2e-smoke.zh-CN.md)
- [架构](./architecture.zh-CN.md)
