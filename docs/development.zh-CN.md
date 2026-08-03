> [English](./development.md) | 简体中文

# 本地开发

NekoNest monorepo 贡献者环境。跨层不变量：[AGENTS.md](../AGENTS.md)。产品合同：[README.zh-CN.md](../README.zh-CN.md)。

## 前置

| 工具 | 说明 |
|---|---|
| Go 1.22+ | `server/` 与 `daemon/` 各自独立 module |
| Node.js + pnpm | PWA；使用已提交的 `pnpm-lock.yaml` |
| 可选 agent CLI | 仅 Windows 上做只读发现冒烟时需要 |
| codegraph（可选） | 代码导航；改源码后 `codegraph sync` |

## 仓库布局

```text
nekonest/
├── protocol/          # protocol.json（手动类型）
├── server/            # module github.com/nekonest/server
├── daemon/            # module github.com/nekonest/daemon
├── pwa/               # Vue 3 + TypeScript + Pinia
├── docs/              # 运维 + 贡献者文档
├── tools/             # 品牌资源构建脚本
├── AGENTS.md
├── README.md
└── README.zh-CN.md
```

**没有根 Go module**。勿把 `_archive/`、`go-sdk/`、`gocache/`、`.pnpm-store/`、`bin/`、`data/`、已构建 PWA 或原生 agent 存储当作应用源码。

## 本地 Server（loopback 开发模式）

**未**设置 `NEKONEST_PHONE_SECRET` 时：

- Server 仅绑定 **`127.0.0.1`**
- 日志提示手机鉴权关闭
- 若 `NEKONEST_ALLOWED_ORIGINS` 为空，可能注入默认本机来源供 CORS
- bootstrap 亦空时注册可能开放——**仅开发**

```powershell
cd pwa
pnpm install --frozen-lockfile
pnpm build

cd ..\server
go run ./cmd/server -port 8080 -data ./data -pwa ../pwa/dist
```

更接近生产的本地运行可设置手机密钥 + bootstrap，仍用 loopback 或本地反代。

PWA 开发服：

```powershell
cd pwa
pnpm dev
```

按分支将开发客户端指向本地 server URL。

## 本地 Daemon

在 Windows 上，使用已注册配置（或指向本地 server）：

```powershell
cd daemon
go run ./cmd/daemon
```

注册：

```powershell
$env:NEKONEST_SERVER = "http://127.0.0.1:8080"
# 类公网本地可选：$env:NEKONEST_BOOTSTRAP_TOKEN = "…"
go run ./cmd/daemon -register -name "dev-pc"
go run ./cmd/daemon
```

适配器冒烟（只读发现辅助）：

```powershell
go run ./cmd/adapter_smoke
```

冒烟测试中切勿改写用户原生 agent 存储。

## 测试

Windows 上推荐显式模块命令：

```powershell
# Server
Set-Location server
go test -count=1 ./...
go vet ./...

# Daemon
Set-Location ..\daemon
go test -count=1 ./...
go vet ./...

# PWA
Set-Location ..\pwa
pnpm install --frozen-lockfile
pnpm test
pnpm type-check
pnpm build
```

### PWA 截图回归

仓库内置 Windows/Chromium Playwright 截图套件。运行时会自动在 `127.0.0.1:18080` 启动确定性 HTTP/WebSocket Mock，并在 `127.0.0.1:5173` 启动 Vite；两个端口须空闲。Mock 使用 PWA 的真实 REST 与线消息形状，但绝不读取原生 agent 存储。

```powershell
Set-Location pwa
pnpm exec playwright install chromium

# 与已提交黄金截图比较。
pnpm test:visual

# 仅在确认 UI 变更符合预期后替换黄金截图。
pnpm test:visual:update

# 打开最近一次运行生成的 HTML 报告。
pnpm test:visual:report
```

黄金截图与 `e2e/visual/visual.spec.ts` 放在一起；`test-results/` 与 `playwright-report/` 是已忽略的本地产物。主矩阵为 `390×844`、简体中文、浅色主题，并抽查窄屏、桌面、深色主题和英文布局。视觉运行还会检查预期页面状态、console/page error、横向溢出、主要触控尺寸、投递状态，以及 Codex `start_thread` 首条提示词的归属。

如需对已经启动的本地 PWA/server/daemon 真栈做只读截图冒烟，设置 PWA 地址及可选设备/会话 ID。手机令牌或管理员密钥只通过当前 PowerShell 会话传入，不要写入文件：

```powershell
$env:NEKONEST_VISUAL_BASE_URL = 'http://127.0.0.1:5173'
$env:NEKONEST_VISUAL_PHONE_TOKEN = '<临时手机令牌>'
$env:NEKONEST_VISUAL_PHONE_ID = '<手机 ID>'
# 旧版/管理员本地鉴权也可改用 NEKONEST_VISUAL_ADMIN_SECRET。
$env:NEKONEST_VISUAL_DEVICE_ID = '<设备 ID>'
$env:NEKONEST_VISUAL_SESSION_ID = '<会话 ID>'
pnpm test:visual:live
```

真栈命令默认只截取设备列表、设备详情和会话详情。只有显式设置 `NEKONEST_VISUAL_SEND_PROMPT` 时才会向所选会话发送该文本，因此只能对一次性测试线程使用。真栈截图属于运行产物，不参与黄金基线比较。

Unix 可用根目录 `Makefile` 的 `test`、`server`、`daemon`、`pwa`（daemon 默认交叉编译 Windows）。

跨层协议或目录变更：跑**全部三个**套件，然后在仓库根：

```powershell
git diff --check
codegraph sync
codegraph status
```

## PWA 国际化与主题

- 文案：`pwa/src/i18n/locales/zh-CN.ts`（默认）与 `en.ts` — 键集合须一致（`pnpm test` 含对齐检查）。
- 运行时：`vue-i18n` Composition API；stores/utils 用 `@/i18n` 的 `tGlobal`。
- 用户偏好：`localStorage` 的 `nekonest_locale`（`zh-CN` \| `en`）与 `nekonest_theme`（`system` \| `light` \| `dark`）。
- 线协议枚举与 agent 商品名保持英文；只翻译 UI 壳层。
- Agent 转录 / Markdown 正文永不走 i18n。

新增界面文案：两个 locale 文件同时加 key，再用 `t()` / `tGlobal()`，勿在视图硬编码中英文。

## 协议与智能体变更

线协议变更须触及所有适用面——见 [protocol.zh-CN.md](./protocol.zh-CN.md) 清单与 AGENTS.md「Wire protocol」。

新增智能体：

1. Daemon 适配器 + 注册表  
2. 如需则 server 类型 / 持久化  
3. PWA `types/protocol.ts`、`config/agents.ts`、资源  
4. `protocol/protocol.json`  
5. 测试 + README/文档智能体表  

## 格式与风格

- 改动的 Go 文件跑 `gofmt`
- 遵循既有 TypeScript/Vue 风格；不做顺手大重构
- 除非依赖声明变更，勿手改 lockfile
- 代码默认不擅自加注释；文档为自由 Markdown

## 勿提交

密钥、`data/`、设备 `config.json`、附件 blob、agent 转录、构建产物、归档包、覆盖率 DB、`.codegraph/codegraph.db`。

## 文档语言布局

| 路径模式 | 语言 |
|---|---|
| `README.md`、`docs/*.md` | 英文（主） |
| `README.zh-CN.md`、`docs/*.zh-CN.md` | 简体中文镜像 |
| `AGENTS.md`、`CHANGELOG.md` | 仅英文 |
| `docs/archive/` | 冻结历史（非现行合同） |

## 相关文档

- [架构](./architecture.zh-CN.md)
- [协议](./protocol.zh-CN.md)
- [配置](./configuration.zh-CN.md)
- [端到端冒烟](./e2e-smoke.zh-CN.md)
- [发版](./release.zh-CN.md)
