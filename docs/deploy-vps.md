# VPS 部署（NekoNest Server）

目标：公网 HTTPS + WSS，手机 PWA 与家中 Daemon 都能连上。

## 1. 编译

在有 Go 1.22+ 的机器上：

```bash
cd server
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o nekonest-server ./cmd/server
```

PWA：

```bash
cd pwa
pnpm install
pnpm build
# 产物在 pwa/dist
```

上传到 VPS，例如：

```text
/opt/nekonest/
  nekonest-server
  pwa-dist/     # 把 pwa/dist 内容拷到这里
  data/
```

## 2. 环境变量

```bash
export NEKONEST_PHONE_SECRET='换成足够长的随机串'
# 公网必设：保护 /api/devices/register，否则任何人可注册 daemon 并写消息
export NEKONEST_BOOTSTRAP_TOKEN='另一段足够长的随机串'
# 可选：限制浏览器来源
# export NEKONEST_ALLOWED_ORIGINS='https://nekonest.example.com'
# 可选：Web Push VAPID（自行生成一对 base64url 密钥）
# export NEKONEST_VAPID_PUBLIC_KEY='...'
# export NEKONEST_VAPID_PRIVATE_KEY='...'
# export NEKONEST_VAPID_SUBJECT='mailto:you@example.com'
# 反代在本机前面时：export NEKONEST_TRUST_PROXY=1
# 反代不在 loopback 时还必须声明其网段：
# export NEKONEST_TRUSTED_PROXY_CIDRS='10.0.0.0/24'
```

Daemon 注册时带上 bootstrap（示例）：

```bash
curl -X POST "https://你的域名/api/devices/register" \
  -H "Content-Type: application/json" \
  -H "X-Neko-Bootstrap: $NEKONEST_BOOTSTRAP_TOKEN" \
  -d '{"name":"书房"}'
```

## 3. systemd 示例

`/etc/systemd/system/nekonest.service`：

```ini
[Unit]
Description=NekoNest Server
After=network.target

[Service]
Type=simple
User=nekonest
WorkingDirectory=/opt/nekonest
Environment=NEKONEST_PHONE_SECRET=换成足够长的随机串
Environment=NEKONEST_BOOTSTRAP_TOKEN=另一段足够长的随机串
# Environment=NEKONEST_ALLOWED_ORIGINS=https://nekonest.example.com
ExecStart=/opt/nekonest/nekonest-server -port 8080 -data /opt/nekonest/data -pwa /opt/nekonest/pwa-dist
Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now nekonest
curl http://127.0.0.1:8080/health
```

## 4. 反代（Caddy 示例）

```caddy
nekonest.example.com {
  # Overwrite client XFF so app sees a single trusted hop (set NEKONEST_TRUST_PROXY=1)
  reverse_proxy 127.0.0.1:8080 {
    header_up X-Forwarded-For {remote_host}
    header_up X-Real-IP {remote_host}
  }
}
```

Nginx 注意 WebSocket：

```nginx
location / {
  proxy_pass http://127.0.0.1:8080;
  proxy_http_version 1.1;
  proxy_set_header Upgrade $http_upgrade;
  proxy_set_header Connection "upgrade";
  proxy_set_header Host $host;
  proxy_set_header X-Forwarded-Proto $scheme;
  # 覆盖而不是透传客户端提供的值；仅这样配置后才启用 NEKONEST_TRUST_PROXY=1
  proxy_set_header X-Forwarded-For $remote_addr;
  proxy_set_header X-Real-IP $remote_addr;
  proxy_read_timeout 3600s;
}
```

## 5. 验收

- 浏览器打开 `https://nekonest.example.com` → 输入 phone secret
- `/health` 返回 `nyan~`
- 手机可配对、可见设备
