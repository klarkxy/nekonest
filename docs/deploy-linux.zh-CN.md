> [English](./deploy-linux.md) | 简体中文

# 安装 Linux 主机 Daemon

Daemon 运行在保存原生编码智能体线程的 Linux 主机上，主动连接 VPS。

## 前置条件

- Linux amd64 或 arm64
- 可访问的 NekoNest HTTPS 地址及其注册令牌
- 至少安装并使用过一次某个受支持的智能体 CLI

## 1. 下载并校验

```bash
# ARM 主机把 amd64 换成 arm64。
asset=nekonest-daemon-linux-amd64.tar.gz
base=https://github.com/klarkxy/nekonest/releases/latest/download
curl -fLO "$base/$asset"
curl -fLO "$base/checksums.txt"
grep "  $asset$" checksums.txt | sha256sum -c -

mkdir -p ~/.local/opt/nekonest-daemon ~/.local/bin
tar -xzf "$asset" -C ~/.local/opt/nekonest-daemon
install -m 755 ~/.local/opt/nekonest-daemon/nekonest-daemon ~/.local/bin/nekonest-daemon
~/.local/bin/nekonest-daemon -version
```

确保 `~/.local/bin` 在 `PATH` 中。从源码构建见
[development.zh-CN.md](./development.zh-CN.md)。

## 2. 注册并配对

```bash
export NEKONEST_SERVER=https://nekonest.example.com
export NEKONEST_BOOTSTRAP_TOKEN='same-bootstrap-token-as-vps'
~/.local/bin/nekonest-daemon -register -name "Home Linux"
~/.local/bin/nekonest-daemon -doctor
```

注册会把私有状态保存到 `~/.nekonest`，并打印手机配对材料。打开 PWA，选择
**配对电脑**，输入打印的配对码。以后需要新码时运行：

```bash
nekonest-daemon -pair gen
```

正常运行不需要保留注册时使用的环境变量。

## 3. 用 systemd 运行

注册当前用户单元并启动：

```bash
nekonest-daemon install
nekonest-daemon start
nekonest-daemon status
journalctl --user -u nekonest-daemon -f
```

`install` 会为默认配置写入
`~/.config/systemd/user/nekonest-daemon.service`（自定义 `-config` 使用带哈希
的单元名）并启用它。它不会复制二进制，也不会立刻启动进程。如果退出登录后仍
需常驻，可选运行 `loginctl enable-linger "$USER"`。同一份 Daemon 配置只能由
一个进程使用。

## 验证

- `nekonest-daemon status` 在 `start` 后显示用户单元已安装且处于 active。
- `nekonest-daemon -doctor` 没有关键配置或网络错误。
- `systemctl --user is-active nekonest-daemon` 返回 `active`。
- 手机上主机显示在线，并列出最近的原生线程。
- 发送短提示词后能看到流式响应。

控制和附件能力取决于已安装智能体。PWA 当前启用的控制是权威结果；见
[智能体支持](./agent-capability-matrix.zh-CN.md)。

## 升级与回滚

1. 停止服务前，先下载并校验目标版本。
2. 备份 `~/.nekonest`，记录当前可执行文件哈希。
3. 运行 `nekonest-daemon stop`，并确认 `status` 显示进程锁空闲。
4. 把旧二进制保存为唯一的回滚文件名，再将新二进制安装到原路径。
5. 若可执行文件路径变了，再运行一次 `install`。然后 `start`，运行 `-doctor`、
   检查日志，并验证手机在线和一次真实提示词。

新 Daemon 失败时，先 `stop`，恢复旧二进制，再 `start` 同一服务。升级期间不要
替换或编辑原生智能体存储。

只替换同一路径上的二进制时，已有用户单元会继续使用。移动可执行文件后应再
运行 `install`，使 `ExecStart` 与新路径一致。只有在继续手写单元时，才需要与
[仓库模板](../daemon/packaging/nekonest-daemon.service)比较。

## 相关文档

- [VPS 部署](./deploy-vps.zh-CN.md)
- [配置](./configuration.zh-CN.md)
- [排障](./troubleshooting.zh-CN.md)
- [验收清单](./e2e-smoke.zh-CN.md)
