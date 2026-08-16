> [English](./troubleshooting.md) | 简体中文

# 排障

从第一个失败边界开始检查：浏览器 → 反向代理 → Server → Daemon → 原生智能体。
收集证据时不要暴露密钥或原生转录。

## PWA 无法打开或初始化被拒

1. 检查 `https://your-nest/health`。
2. 确认反代支持 WebSocket upgrade，并转发到私有 Server 端口。
3. 确认 `NEKONEST_ALLOWED_ORIGINS` 准确包含浏览器来源。
4. 重新输入管理员密钥。被拒绝的值不能保存为有效手机凭据。
5. 升级后界面仍旧时，完全关闭 PWA，重新打开，再做一次强制刷新。

第一次连接到有意选择的 open 实例会要求确认，这不是传输模式不匹配。

## Daemon 无法注册

- `NEKONEST_SERVER` 必须是可访问的 HTTPS 基础地址。
- `NEKONEST_BOOTSTRAP_TOKEN` 必须与 Server 一致。
- 公网 Server 配置了管理员密钥却没有注册令牌时，会拒绝新注册。
- 主机时间与 TLS 信任库必须正常。
- 若设置了传输模式断言，它必须与 Server 已保存模式一致。

修改任何文件前先运行 `nekonest-daemon -doctor`。

## 主机一直离线

1. 运行 `nekonest-daemon status`，确认主机服务已安装且进程锁被持有。
2. 确认只有一个 Daemon 进程使用该配置。
3. 在 Daemon 日志中检查鉴权、TLS 或重连错误。
4. 确认主机能向猫娘乐园建立出站 WSS 连接。
5. 确认 Server 健康且没有反复重启。
6. 凭据已撤销或从另一安装复制时，撤销旧主机并重新注册，不要手改
   `config.json`。

## 看不到项目或线程

- 先在主机上使用一次受支持的智能体，确保存在原生线程。
- 确认智能体 CLI、原生存储与 Daemon 属于同一个系统用户。
- 运行 `-doctor`，并查看 PWA 显示的不可用原因。
- Cursor 需要 Agent CLI（`cursor-agent`）。桌面编辑器和无关的 `agent.exe`
  （包括 Grok Build）都不会被接受。CLI 不在常见路径时，设置
  `NEKONEST_CURSOR_CLI`。Cursor IDE 注入的 `CURSOR_AGENT` 不是 CLI 路径。
  Agent 线程位于 `~/.cursor/projects/*/agent-transcripts`；仅有 composer 记录
  不够。
- ZCode 当前不可用。桌面/TUI 会话可能还在，但上游 `zcode login` 会报
  `OAuth response is not valid JSON`，因此 NekoNest 不声明发现、发送或新建。
- Daemon 在线后刷新设备页。
- 较旧、子智能体、侧链或纯合成记录可能被有意隐藏；在主机上重新打开旧主线程
  即可恢复活跃度。
- 无法识别目录的线程位于**未分类**。

NekoNest 不浏览任意文件夹，也不创建只存在于猫娘乐园的幽灵线程。只有当所选
智能体对已发现项目明确声明新建能力时，手机才显示新建入口。

## 某个控制被禁用

正在运行的 Daemon 所声明的能力是权威结果。常见原因包括 CLI 缺失、原生探测
失败、只支持兼容继续，或该智能体请求只能在主机终端完成。

不要绕过禁用状态。运行 `-doctor`，必要时更新对应智能体 CLI，再重连 Daemon。

最近七天没有可见线程活动的智能体每天只重新探测一次。安装或升级这类 CLI 后，
重启 Daemon 可立即执行启动探测；普通手机重连不会强制拉起所有休眠 CLI。

## 出现 CLI 控制台窗口

当前 Windows 安装会让 Daemon 和后台智能体探测无窗口运行。重新执行
`nekonest-daemon.exe install` 刷新受管理的计划任务启动器，再运行 `stop` 和
`start`。`status` 仍应报告 Daemon 可执行文件，而计划任务动作使用 `wscript.exe`。

## 提示词卡住或投递结果不确定

1. 检查线程是否正在运行，或正在等待输入/审批。
2. 只有 PWA 启用中断时才使用中断；否则检查主机进程和终端。
3. 重连期间保持 Daemon 运行。第一条提示词可能已经越过智能体边界时，不要用新
   ID 重发同一操作。
4. NekoNest 报告不确定或队列阻塞时，使用 PWA 明确显示的继续/跳过操作，不要
   编辑状态文件。

对由 NekoNest 启动的长程 Codex 任务，活动 app-server turn 是“仍在运行”的正面
证据；`turn/completed`、中断、失败或 app-server 退出会结束该状态。若任务从其他
Codex 界面启动，仅凭 rollout 文件长时间没有新内容，无法证明进程仍然存活。
NekoNest 会把近期原生活动作为辅助证据，并在较保守的无活动窗口后才把未终止记录
当作孤儿；如需区分“安静运行”和“外部任务卡死”，仍应检查主机进程。

## 附件失败

- 每次提示词最多 5 个文件，每个最多 4 MB。
- 使用 JPEG、PNG、WebP、GIF、TXT、Markdown、PDF 或 JSON。
- 检查 Server 磁盘空间及 Daemon 对临时下载位置的访问。
- 即使上传成功，智能体沙箱也可能拒绝本地路径。PWA 只显示该智能体声明的附件
  级别。

## 手机没有出现审批或问题

只有原生且仍然有效的智能体事件才能创建审批或结构化输入界面。兼容路径需要在
主机终端完成请求。若某项控制在重连前存在，先运行 `-doctor` 并等待新的能力目录，
不要假设它仍可用。

若要接收 Codex 的结构化问题，请在发送提示前选择**规划模式**。普通执行模式不会
提供 Codex 的结构化提问工具；如果规划模式没有出现，说明当前 app-server 探测未
确认所需 API。

## Web Push 收不到

- 在 Server 上配置全部三个 VAPID 变量。
- 使用 HTTPS，并允许浏览器通知权限。
- 轮换 VAPID 密钥后，重新打开 PWA 并重建订阅。
- Push 是可选功能，应单独验证应用内流程。

## Server 无法启动

| 症状 | 常见原因 |
|---|---|
| 只监听 loopback | 没有管理员密钥；这是安全的开发模式。 |
| 注册被禁用 | 已设置管理员密钥，但注册令牌为空。 |
| 传输模式不匹配 | 环境断言与数据目录里保存的模式不同。 |
| 权限错误 | Server 运行身份无法私有持有数据目录或密钥文件。 |
| 限流识别到错误客户端 | 开启了代理信任，但没有正确覆盖转发头或配置可信 CIDR。 |

## 收集有效日志

1. 用 `NEKONEST_LOG_FORMAT=json` 复现一次。
2. `NEKONEST_LOG_LEVEL=debug` 只短时间启用。
3. 记录组件版本、`/health`、Daemon `-doctor` 和第一个失败边界。
4. 分享前删除密钥、令牌、私钥、路径、提示词、附件和原生转录。

## 相关文档

- [配置](./configuration.zh-CN.md)
- [安全](./security.zh-CN.md)
- [Windows 主机](./deploy-windows.zh-CN.md)
- [Linux 主机](./deploy-linux.zh-CN.md)
- [验收清单](./e2e-smoke.zh-CN.md)
