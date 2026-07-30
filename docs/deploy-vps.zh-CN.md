> [English](./deploy-vps.md) | 简体中文

# VPS 部署（NekoNest Server）

目标：公网 HTTPS + WSS，使手机 PWA 与家中 Daemon 都能连上。

完整环境变量/flags：[configuration.zh-CN.md](./configuration.zh-CN.md)。安全：[security.zh-CN.md](./security.zh-CN.md)。

## 前置

- 可控的 Linux VPS（或同类主机）与公网 DNS 名
- 构建机 Go **1.22+**
- Node.js + **pnpm** 构建 PWA
- 带 TLS 的反代（推荐 Caddy 或 Nginx）
- 两段长随机密钥：手机密钥与 bootstrap 令牌（**必须不同**）

## 1. 编译

```bash
git clone https://github.com/klarkxy/nekonest.git
cd nekonest

cd pwa
pnpm install --frozen-lockfile
pnpm build
# 产物：pwa/dist

cd ../server
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o nekonest-server ./cmd/server
```

上传到 VPS，例如：

```text
/opt/nekonest/
  nekonest-server
  pwa-dist/          # pwa/dist 内容
  data/              # 运行时创建；保持私密
  .env               # 可选 EnvironmentFile（权限 600）
```

## 2. 环境变量

```bash
export NEKONEST_PHONE_SECRET='换成足够长的随机串'
export NEKONEST_BOOTSTRAP_TOKEN='另一段足够长的随机串'
export NEKONEST_ALLOWED_ORIGINS='https://nekonest.example.com'
# 本机前有覆盖转发头的反代时：
export NEKONEST_TRUST_PROXY=1
# 反代不在 loopback 时：
# export NEKONEST_TRUSTED_PROXY_CIDRS='10.0.0.0/8'
# 可选 Web Push：
# export NEKONEST_VAPID_PUBLIC_KEY='…'
# export NEKONEST_VAPID_PRIVATE_KEY='…'
# export NEKONEST_VAPID_SUBJECT='mailto:you@example.com'
```

> [!WARNING]
> 未设置 `NEKONEST_PHONE_SECRET` 时 Server 只绑定 **loopback**（开发模式）。不要公网暴露。已设手机密钥但 bootstrap 为空时，**设备注册禁用**。

手动注册探测（通常由 Windows daemon 完成）：

```bash
curl -X POST "https://nekonest.example.com/api/devices/register" \
  -H "Content-Type: application/json" \
  -H "X-Neko-Bootstrap: $NEKONEST_BOOTSTRAP_TOKEN" \
  -d '{"name":"书房"}'
```

## 3. systemd

`/etc/systemd/system/nekonest.service`：

```ini
[Unit]
Description=NekoNest Server
After=network.target

[Service]
Type=simple
User=nekonest
Group=nekonest
WorkingDirectory=/opt/nekonest
EnvironmentFile=-/opt/nekonest/.env
# 密钥优先放 EnvironmentFile。若必须内联：
# Environment=NEKONEST_PHONE_SECRET=…
# Environment=NEKONEST_BOOTSTRAP_TOKEN=…
# Environment=NEKONEST_ALLOWED_ORIGINS=https://nekonest.example.com
# Environment=NEKONEST_TRUST_PROXY=1
ExecStart=/opt/nekonest/nekonest-server -port 8080 -data /opt/nekonest/data -pwa /opt/nekonest/pwa-dist
Restart=on-failure
RestartSec=3
NoNewPrivileges=true
PrivateTmp=true

[Install]
WantedBy=multi-user.target
```

```bash
sudo useradd --system --home /opt/nekonest --shell /usr/sbin/nologin nekonest
sudo chown -R nekonest:nekonest /opt/nekonest
sudo chmod 600 /opt/nekonest/.env   # 若使用
sudo systemctl daemon-reload
sudo systemctl enable --now nekonest
curl -sS http://127.0.0.1:8080/health
# {"status":"nyan~"}
```

**8080** 仅本机；公网只开 80/443 经反代。

## 4. 反代

### Caddy

```caddy
nekonest.example.com {
  # 覆盖客户端 XFF，使应用看到单一可信跳（配合 NEKONEST_TRUST_PROXY=1）
  reverse_proxy 127.0.0.1:8080 {
    header_up X-Forwarded-For {remote_host}
    header_up X-Real-IP {remote_host}
  }
}
```

### Nginx

支持 WebSocket；**覆盖**（勿盲目追加）客户端提供的 XFF：

```nginx
server {
  server_name nekonest.example.com;
  # 此处终止 TLS…

  location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header X-Forwarded-For $remote_addr;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_read_timeout 3600s;
  }
}
```

## 5. 验收

- [ ] `curl https://nekonest.example.com/health` → `nyan~`
- [ ] 浏览器打开 PWA；手机密钥可用
- [ ] Daemon 可用 bootstrap 注册
- [ ] 手机配对后设备 **online**
- [ ] 完整路径：[e2e-smoke.zh-CN.md](./e2e-smoke.zh-CN.md)

## 6. 升级

1. 从 release tag 或 `main` 重新编译 `nekonest-server` 与 `pwa/dist`。  
2. 在 VPS 替换二进制与 `pwa-dist/`。  
3. **保留** `/opt/nekonest/data`（SQLite + 附件）。  
4. `sudo systemctl restart nekonest`  
5. 再跑冒烟检查。  

标签不会自动部署；生产更新由运维执行（[release.zh-CN.md](./release.zh-CN.md)）。

## 相关

- [Windows Daemon 部署](./deploy-windows.zh-CN.md)
- [配置](./configuration.zh-CN.md)
- [安全](./security.zh-CN.md)
- [排障](./troubleshooting.zh-CN.md)
