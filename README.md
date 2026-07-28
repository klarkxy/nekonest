# 🐱 NekoNest（猫娘窝）

> 自托管：在外面用手机，通过 VPS 续写家里 Windows 上已有的编码智能体线程

```
手机 PWA  ──WSS──►  VPS Server  ◄──出站 WSS──  家 PC Daemon  ──► Agent CLI
```

## 三步自用

### 1. VPS 跑 Server

详见 [docs/deploy-vps.md](docs/deploy-vps.md)。

```bash
cd server && go build -o nekonest-server ./cmd/server
export NEKONEST_PHONE_SECRET='长随机串'
export NEKONEST_BOOTSTRAP_TOKEN='另一段长随机串'   # 公网必设，保护设备注册
# 若前面有 Caddy/Nginx：export NEKONEST_TRUST_PROXY=1
./nekonest-server -port 8080 -data ./data -pwa ../pwa/dist
```

用 Caddy/Nginx 终止 HTTPS，反代到 8080（需支持 WebSocket）。

### 2. Windows 注册 Daemon

详见 [docs/deploy-windows.md](docs/deploy-windows.md)。

```powershell
cd daemon
go build -o nekonest-daemon.exe ./cmd/daemon
$env:NEKONEST_SERVER="https://你的域名"
.\nekonest-daemon.exe -register -name "书房"
# 记下打印的 6 位配对码，然后：
.\nekonest-daemon.exe
```

### 3. 手机打开 PWA

1. 浏览器打开 `https://你的域名`  
2. 输入与 VPS 相同的 `NEKONEST_PHONE_SECRET`  
3. 「配对电脑」输入 6 位码  
4. 在 PC 上先开一个受支持的智能体线程 → 手机按「目录 → 智能体 → 线程」进入并续写

冒烟清单：[docs/e2e-smoke.md](docs/e2e-smoke.md)

## 项目结构

```
nekonest/
├── server/     # VPS 中转 (Go)
├── daemon/     # Windows 守护进程 (Go)
├── pwa/        # 手机端 (Vue 3)
├── protocol/   # 协议 schema
└── docs/       # 部署与验收
```

## 开发

```bash
# Server
cd server && go run ./cmd/server -port 8080 -pwa ../pwa/dist

# Daemon（先 register）
cd daemon && go run ./cmd/daemon

# PWA
cd pwa && pnpm install && pnpm dev
```

本地未设 `NEKONEST_PHONE_SECRET` 时手机 API 不鉴权（仅限开发）。

## 当前能力与限制

| 能力 | 状态 |
|------|------|
| 设备注册 / 配对码 | ✅ |
| Daemon 长连接 + 会话发现 | ✅ Claude Code / Codex / Kilo / Kimi CLI / Grok Build |
| 手机续写已有会话 | ✅ 主路径 |
| 目录 → 智能体 → 线程归类 | ✅ 无目录线程统一进入「未分类」 |
| 手机密钥 | ✅ `NEKONEST_PHONE_SECRET` |
| 手机远程新建线程 | — 本阶段不提供；请先在 PC 创建 |
| 工具审批 | ⚠️ 取决于智能体的非交互 CLI 能力；不支持时回到 PC 处理 |
| 托盘 / 开机安装包 | ⏳ 可选后续 |

## License

MIT
