> [English](./deploy-windows.md) | 简体中文

# Windows Daemon 部署

在家用 PC 上运行出站 NekoNest Daemon，使手机可续写已有智能体线程。

配置参考：[configuration.zh-CN.md](./configuration.zh-CN.md)。

## 前置

- 已在使用至少一个受支持 agent CLI 的 Windows PC
- 至少已有一个原生线程，用于构成 daemon 的已发现项目集合；已安装且探测通过的 starter 之后也只能在该集合内新建线程
- 可达的窝地址（`https://…`）以及与 VPS 相同的 `NEKONEST_BOOTSTRAP_TOKEN`
- 从源码构建时需要 Go 1.22+

支持的智能体（摘要）：

| 智能体 | 原生存储（典型） | 续写入口 | 附件 |
|---|---|---|---|
| Claude Code | `~/.claude/projects` | `claude --resume` | 授权临时目录；路径写入提示词 |
| Codex | `~/.codex/sessions` | `codex exec resume` | 原生图片参数；其他文件经受限目录 + 路径 |
| Kilo | Kilo / OpenCode 本地 DB | `kilo run --session` | 原生 `--file` |
| Kimi CLI | `.kimi-code`（旧 `.kimi`） | `kimi --session` | 提示词中的本地路径；受 CLI 权限约束 |
| Grok Build | `~/.grok/sessions` | `grok --resume` | 提示词路径；非交互安全模式 |

缺少某一 CLI 或空存储不影响其他智能体。

## 1. 编译

```powershell
git clone https://github.com/klarkxy/nekonest.git
Set-Location nekonest\daemon
$env:CGO_ENABLED = "0"
go build -trimpath -ldflags="-s -w" -o nekonest-daemon.exe ./cmd/daemon
```

将 exe 放到稳定路径，例如 `D:\nekonest\bin\nekonest-daemon.exe`。

## 2. 注册到 VPS

```powershell
$env:NEKONEST_SERVER = "https://nekonest.example.com"
$env:NEKONEST_BOOTSTRAP_TOKEN = "与-vps-相同的-bootstrap"
.\nekonest-daemon.exe -register -name "书房电脑"
```

成功后 daemon 会：

- 写入 `%USERPROFILE%\.nekonest\config.json`（`server_url`、`device_id`、`token` 等）
- 打印 **6 位**手机配对码（短 TTL，约 5 分钟）

在 PWA「配对电脑」中输入该码。

之后需要新码：

```powershell
.\nekonest-daemon.exe -pair gen
```

自定义配置路径：`-config C:\path\to\config.json`。

## 3. 常驻运行

```powershell
.\nekonest-daemon.exe
```

日志应出现针对你的 `device_…` 的鉴权信息。同一配置路径只允许**一个**进程（`.daemon.lock`）；第二实例会退出。

### 开机启动 — 任务计划程序

1. 任务计划程序 → 创建基本任务  
2. 触发器：**登录时**  
3. 操作：启动 `D:\path\to\nekonest-daemon.exe`  
4. 起始于：exe 所在目录  

PowerShell（当前用户登录时）：

```powershell
$exe = "D:\path\to\nekonest-daemon.exe"
$action = New-ScheduledTaskAction -Execute $exe -WorkingDirectory (Split-Path $exe)
$trigger = New-ScheduledTaskTrigger -AtLogOn
Register-ScheduledTask -TaskName "NekoNestDaemon" -Action $action -Trigger $trigger -Description "NekoNest"
```

### 开机启动 — 启动文件夹备用

任务计划无权限时，在用户 Startup 文件夹建快捷方式：

```text
%APPDATA%\Microsoft\Windows\Start Menu\Programs\Startup\
```

指向 `nekonest-daemon.exe`，并将“起始位置”设为 exe 目录。

## 4. 可选：Windows Defender 排除

仅当杀软误杀你自行构建的自托管二进制时：

```powershell
# 管理员
Add-MpPreference -ExclusionPath "D:\path\to\daemon-or-bin"
Add-MpPreference -ExclusionProcess "nekonest-daemon.exe"
```

会扩大本机攻击面——请有意使用。

## 5. 日常用法

1. 在 PC 上正常使用 Claude Code / Codex / Kilo / Kimi CLI / Grok Build，产生线程。  
2. Daemon 按短间隔 Discover 并上报会话列表。  
3. 手机：按 **目录 → 智能体 → 线程** 打开，发送提示词或附件。  
4. 无可识别项目目录的线程进入「**未分类**」。  
5. 非交互 CLI 无法承载的审批须在 **PC 终端**完成。  

附件限额：最多 **5** 个、每个 **4 MB**；见 [configuration.zh-CN.md](./configuration.zh-CN.md)。

## 6. 升级

1. 编译新的 `nekonest-daemon.exe`。  
2. 停止正在运行的 daemon。  
3. 替换 exe。  
4. **保留** `%USERPROFILE%\.nekonest\config.json`（及 journal/lock 旁系文件）。  
5. 再启动；在手机上确认 online。  
6. 冒烟：[e2e-smoke.zh-CN.md](./e2e-smoke.zh-CN.md)。  

更改 `device_id` / `token` 需重新注册（新凭据）并重启进程；热更不会在进程中途切换身份。

## 相关

- [VPS 部署](./deploy-vps.zh-CN.md)
- [排障](./troubleshooting.zh-CN.md)
- [架构](./architecture.zh-CN.md)
