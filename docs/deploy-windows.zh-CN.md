> [English](./deploy-windows.md) | 简体中文

# 安装 Windows 主机 Daemon

Daemon 运行在保存原生编码智能体线程的电脑上，主动连接 VPS，不需要开放入站
端口。

## 前置条件

- Windows amd64
- 可访问的 NekoNest HTTPS 地址及其注册令牌
- 至少安装并使用过一次某个受支持的智能体 CLI

## 1. 下载并校验

```powershell
$asset = "nekonest-daemon-windows-amd64.zip"
$base = "https://github.com/klarkxy/nekonest/releases/latest/download"
Invoke-WebRequest "$base/$asset" -OutFile $asset
Invoke-WebRequest "$base/checksums.txt" -OutFile checksums.txt

$line = Get-Content checksums.txt | Where-Object { $_ -match "  $([regex]::Escape($asset))$" }
if (-not $line) { throw "Checksum entry not found" }
$expected = ($line -split '\s+')[0].ToLowerInvariant()
$actual = (Get-FileHash $asset -Algorithm SHA256).Hash.ToLowerInvariant()
if ($actual -ne $expected) { throw "Checksum mismatch" }

Expand-Archive $asset -DestinationPath D:\NekoNest -Force
D:\NekoNest\nekonest-daemon.exe -version
```

把可执行文件放在稳定目录。从源码构建见
[development.zh-CN.md](./development.zh-CN.md)。

## 2. 注册并配对

```powershell
$env:NEKONEST_SERVER = "https://nekonest.example.com"
$env:NEKONEST_BOOTSTRAP_TOKEN = "same-bootstrap-token-as-vps"
D:\NekoNest\nekonest-daemon.exe -register -name "Study PC"
D:\NekoNest\nekonest-daemon.exe -doctor
```

注册会把私有状态保存到 `%USERPROFILE%\.nekonest`，并打印手机配对材料。
打开 PWA，选择**配对电脑**，输入打印的配对码。以后需要新码时运行：

```powershell
D:\NekoNest\nekonest-daemon.exe -pair gen
```

正常运行不需要保留注册时使用的环境变量。

## 3. 登录后自动运行

可选的前台测试：

```powershell
D:\NekoNest\nekonest-daemon.exe
```

用 Ctrl+C 停止，再注册当前用户的登录计划任务并启动：

```powershell
D:\NekoNest\nekonest-daemon.exe install
D:\NekoNest\nekonest-daemon.exe start
D:\NekoNest\nekonest-daemon.exe status
```

`install` 会写入当前用户的计划任务（默认配置名为 `NekoNestDaemon`）。任务以
已登录用户运行，没有 72 小时时限，也不会复制二进制。请把可执行文件放在稳定
目录。自定义 `-config` 会使用单独的任务名。

同一份 Daemon 配置只能由一个进程使用。任务退出时，先用 `status` 检查是否还有
手工启动的进程，不要直接修改配置。

## 验证

- `nekonest-daemon.exe status` 在 `start` 后显示任务已安装且进程锁被持有。
- `nekonest-daemon.exe -doctor` 没有关键配置或网络错误。
- 手机上主机显示在线。
- 最近的原生线程出现在对应项目与智能体下。
- 发送短提示词后能看到流式响应。

控制和附件能力取决于已安装智能体。PWA 当前启用的控制是权威结果；见
[智能体支持](./agent-capability-matrix.zh-CN.md)。

## 升级与回滚

1. 停止当前 Daemon 前，先下载并校验目标版本。
2. 备份 `%USERPROFILE%\.nekonest`，记录当前可执行文件哈希。
3. 运行 `nekonest-daemon.exe stop`，并确认 `status` 显示进程锁空闲。
4. 把旧可执行文件改为唯一的回滚文件名，再将新文件放到原路径。
5. 若可执行文件路径变了，再运行一次 `install`。然后 `start`，运行 `-doctor`，
   并验证手机在线和一次真实提示词。

新 Daemon 无法保持连接时，先 `stop`，恢复旧可执行文件，再 `start` 同一任务。
升级期间不要替换或编辑原生智能体存储。

## 相关文档

- [VPS 部署](./deploy-vps.zh-CN.md)
- [配置](./configuration.zh-CN.md)
- [排障](./troubleshooting.zh-CN.md)
- [验收清单](./e2e-smoke.zh-CN.md)
