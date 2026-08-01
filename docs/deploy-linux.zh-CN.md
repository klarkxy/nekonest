> [English](./deploy-linux.md) | 简体中文

# 部署 Linux 主机 Daemon

v1 正式主机 OS 包含 **Linux**（与 Windows）。验收基线：Ubuntu 22.04+、Debian 12+，架构 `amd64` / `arm64`。

## 构建

在 `daemon/` 下：

```bash
go build -o nekonest-daemon ./cmd/daemon
install -Dm755 nekonest-daemon ~/.local/bin/nekonest-daemon
```

交叉编译示例：

```bash
GOOS=linux GOARCH=amd64 go build -o nekonest-daemon-linux-amd64 ./cmd/daemon
GOOS=linux GOARCH=arm64 go build -o nekonest-daemon-linux-arm64 ./cmd/daemon
```

## 注册与配对

```bash
export NEKONEST_SERVER=https://your-nest.example
export NEKONEST_BOOTSTRAP_TOKEN=...   # 窝已设 admin secret 时需要
nekonest-daemon -register -name "Home Linux"
nekonest-daemon -pair gen             # 打印配对码 + QR JSON + 指纹
```

在 PWA 配对页粘贴 QR JSON，并与电脑屏幕核对指纹。

## Doctor

```bash
nekonest-daemon -doctor
```

检查 OS、传输模式、配置、E2E 身份文件、各 agent CLI，以及窝 `/health`。

## systemd 用户单元

```bash
mkdir -p ~/.config/systemd/user
cp packaging/nekonest-daemon.service ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now nekonest-daemon.service
loginctl enable-linger "$USER"   # 可选：注销后仍保持运行
```

日志：

```bash
journalctl --user -u nekonest-daemon -f
```

## 传输模式

v0.2 默认 open 传输，也可在 server 与 daemon 显式设置：

```bash
export NEKONEST_TRANSPORT_MODE=open
```

（PWA 默认 open）。sealed 为显式预览模式；模式不匹配会拒绝握手。
