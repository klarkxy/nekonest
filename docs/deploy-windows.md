# Windows Daemon 部署

## 1. 编译

```powershell
cd daemon
$env:CGO_ENABLED=0
go build -trimpath -ldflags="-s -w" -o nekonest-daemon.exe ./cmd/daemon
```

## 2. 注册到 VPS

```powershell
$env:NEKONEST_SERVER="https://nekonest.example.com"
# 与 VPS 上 NEKONEST_BOOTSTRAP_TOKEN 相同（公网注册必填）
$env:NEKONEST_BOOTSTRAP_TOKEN="另一段长随机串"
.\nekonest-daemon.exe -register -name "书房电脑"
```

成功后会：

- 写入 `%USERPROFILE%\.nekonest\config.json`（含 device_id + token）
- 打印 **6 位手机配对码**

在手机 PWA「配对电脑」输入该码。

需要新配对码时：

```powershell
.\nekonest-daemon.exe -pair gen
```

## 3. 常驻运行

```powershell
.\nekonest-daemon.exe
```

日志应出现 `authenticated as device_...`。

### 开机启动（任务计划程序）

1. 打开「任务计划程序」→ 创建基本任务  
2. 触发器：登录时  
3. 操作：启动程序 → `D:\path\to\nekonest-daemon.exe`  
4. 起始于：exe 所在目录  

或 PowerShell（当前用户登录时）：

```powershell
$action = New-ScheduledTaskAction -Execute "D:\path\to\nekonest-daemon.exe"
$trigger = New-ScheduledTaskTrigger -AtLogOn
Register-ScheduledTask -TaskName "NekoNestDaemon" -Action $action -Trigger $trigger -Description "NekoNest"
```

## 4. Windows Defender 排除（自用）

开发/常驻目录被误杀时：

```powershell
# 管理员
Add-MpPreference -ExclusionPath "D:\path\to\daemon"
Add-MpPreference -ExclusionProcess "nekonest-daemon.exe"
```

## 5. 主路径用法

1. PC 上正常使用 Claude Code、Codex、Kilo、Kimi CLI 或 Grok Build（产生线程）
2. Daemon 每几秒 Discover，上报会话列表
3. 手机按「目录 → 智能体 → 线程」打开已有线程，发送指令（resume）

没有可识别工作目录的线程会进入唯一的「未分类」目录。某个目录下没有某类智能体线程时，不显示该智能体分组。

手机端不提供远程新建线程；请先在 PC 端创建。Daemon 会按本机实际存在的会话存储自动发现，未安装或没有线程的智能体会被忽略。

### 智能体说明

| 智能体 | 本地续写入口 | 备注 |
|---|---|---|
| Claude Code | `claude --resume` | 发现 `~/.claude/projects` |
| Codex | `codex exec resume` | 发现 `~/.codex/sessions` |
| Kilo | `kilo run --session` | 发现本地 Kilo/OpenCode 数据库 |
| Kimi CLI | `kimi --session` | 兼容 `.kimi-code` 当前布局与 `.kimi` 旧布局 |
| Grok Build | `grok --resume` | 发现 `~/.grok/sessions`；手机续写使用非交互安全模式 |

非交互命令不能承载的审批不会在手机端伪装成成功；请回到 PC 终端完成。
