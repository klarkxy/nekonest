<div align="center">
  <img src="./pwa/public/brand/nekonest-duo.webp" width="360" alt="NekoNest">

  <h1>NekoNest · 猫娘乐园</h1>

  <p><strong>在手机上继续运行 Windows 或 Linux 主机里的编码智能体线程。</strong></p>
  <p>自托管 · 主机只出站连接 · 使用原生会话存储 · 移动端 PWA</p>

  <p>
    <a href="./README.md">English</a> ·
    <a href="#快速开始">快速开始</a> ·
    <a href="./docs/README.zh-CN.md">文档</a> ·
    <a href="#许可证">许可证</a>
  </p>
  <p>
    <a href="https://github.com/klarkxy/nekonest/actions/workflows/ci.yml"><img src="https://github.com/klarkxy/nekonest/actions/workflows/ci.yml/badge.svg?branch=main" alt="CI"></a>
  </p>
</div>

---

NekoNest 把手机 PWA 与你自己电脑上的编码智能体连接起来。主机 Daemon
主动连接 VPS，发现智能体的原生线程，并转发提示词、附件、输出及当前支持的
控制操作；家用电脑不需要开放入站端口。

原生智能体存储始终是会话事实来源。NekoNest 通常用于继续已有线程。只有当
已安装的智能体明确提供新建能力，而且目标项目已在主机上被发现时，手机才能
新建原生线程。

## 工作方式

<div align="center">
  <img src="./docs/images/how-it-works.zh-CN.jpg" width="920" alt="手机连接 VPS，Windows 或 Linux 主机上的 Daemon 主动出站连接并控制本地编码智能体">
</div>

## 支持的智能体

| 智能体 | 支持级别 |
|---|---|
| Claude Code | 继续已有线程；具体控制取决于已安装的 CLI 路径 |
| Codex | `codex app-server` 可用时支持手机完整控制，否则使用兼容回退 |
| Kimi CLI | 兼容继续 |
| Grok Build | 兼容继续 |

能力由运行时检测。PWA 只启用当前 Daemon 明确声明的控制；某项操作不可用时，
先运行 `nekonest-daemon -doctor`。稳定的支持边界见
[智能体支持说明](./docs/agent-capability-matrix.zh-CN.md)。

使用 Codex 时，如果希望本轮只规划并能在手机上回答结构化问题，可在输入框旁启用
**规划模式**。普通模式仍是默认值，用于继续执行编码工作。线程忙碌时发送的提示会
进入持久 FIFO 队列，并可在原生执行开始前取消。
对长时间运行的 Codex turn，由 NekoNest 发起的任务以 app-server 的
`turn/started` / `turn/completed` 状态为权威依据。原生存储回退路径同时检查终止
事件和近期 rollout 活动，不再仅凭 turn 已运行多久就误判任务已经停止。

## 快速开始

### 1. 在 VPS 运行 Server

GHCR 镜像已经包含匹配的 PWA：

```bash
sudo install -d -m 700 -o 10001 -g 10001 /var/lib/nekonest
docker run -d --name nekonest --restart unless-stopped \
  --read-only --cap-drop ALL --security-opt no-new-privileges:true \
  --tmpfs /tmp:rw,noexec,nosuid,nodev,size=64m,uid=10001,gid=10001,mode=1770 \
  -p 127.0.0.1:8080:8080 \
  -v /var/lib/nekonest:/data \
  -e NEKONEST_ADMIN_SECRET='long-random-string' \
  -e NEKONEST_BOOTSTRAP_TOKEN='different-long-random-string' \
  -e NEKONEST_ALLOWED_ORIGINS='https://nekonest.example.com' \
  -e NEKONEST_TRUST_PROXY=1 \
  ghcr.io/klarkxy/nekonest-server:latest
```

用反向代理终止公网 HTTPS/WSS，并保持 8080 端口不对公网开放。Compose、
Caddy/Nginx、备份和升级步骤见 [VPS 部署](./docs/deploy-vps.zh-CN.md)。

### 2. 安装主机 Daemon

先安装并使用至少一个受支持的智能体 CLI，让 NekoNest 有原生线程可发现。
Windows 示例：

```powershell
$asset = "nekonest-daemon-windows-amd64.zip"
$base = "https://github.com/klarkxy/nekonest/releases/latest/download"
Invoke-WebRequest "$base/$asset" -OutFile $asset
Invoke-WebRequest "$base/checksums.txt" -OutFile checksums.txt
# 解压前先用 checksums.txt 校验压缩包。
Expand-Archive $asset -DestinationPath .\nekonest-daemon -Force
Set-Location .\nekonest-daemon
$env:NEKONEST_SERVER = "https://nekonest.example.com"
$env:NEKONEST_BOOTSTRAP_TOKEN = "same-bootstrap-token-as-vps"
.\nekonest-daemon.exe -register -name "Study PC"
.\nekonest-daemon.exe install
.\nekonest-daemon.exe start
```

安装与自启动请分别看 [Windows](./docs/deploy-windows.zh-CN.md) 或
[Linux](./docs/deploy-linux.zh-CN.md) 指南。
智能体的新建线程能力会在 Daemon 启动时检测一次，之后按活跃度刷新：活跃智能体
每五分钟、最近七天用过的智能体每小时、七天没有线程活动的智能体每天检测一次。

### 3. 配对手机

1. 打开 NekoNest 网址，用管理员密钥完成初始化。
2. 选择**配对电脑**，输入 Daemon 打印的配对码。
3. 打开**目录 → 智能体 → 线程**并发送提示词。

安装后运行[验收清单](./docs/e2e-smoke.zh-CN.md)。公网 VPS 必须使用不同的
管理员密钥与注册令牌，并阅读[安全指南](./docs/security.zh-CN.md)。

## 文档

[文档索引](./docs/README.zh-CN.md) 已将安装运维与贡献者参考分开。
用户可见的版本历史见 [CHANGELOG.md](./CHANGELOG.md)。

## 许可证

**Star And Thank Author License (SATA) 2.0**。英文 [LICENSE](./LICENSE)
为准，[LICENSE_zh](./LICENSE_zh) 是便于阅读的中文翻译。
