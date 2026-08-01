> English | [简体中文](./deploy-vps.zh-CN.md)

# VPS deploy (NekoNest Server)

Goal: public HTTPS + WSS so the phone PWA and home daemon can both reach the nest.

Full env/flag reference: [configuration.md](./configuration.md). Security: [security.md](./security.md).

## Prerequisites

- Linux VPS (or any host you control) with a public DNS name
- Go **1.22+** on the build machine
- Node.js + **pnpm** to build the PWA
- Reverse proxy with TLS (Caddy or Nginx recommended)
- Two long random secrets: admin secret and bootstrap token (**different**)

## 1. Build

```bash
git clone https://github.com/klarkxy/nekonest.git
cd nekonest

cd pwa
pnpm install --frozen-lockfile
pnpm build
# output: pwa/dist

cd ../server
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o nekonest-server ./cmd/server
```

Upload to the VPS, for example:

```text
/opt/nekonest/
  nekonest-server
  pwa-dist/          # contents of pwa/dist
  data/              # created at runtime; keep private
  .env               # optional EnvironmentFile (mode 600)
```

## 2. Environment

```bash
export NEKONEST_ADMIN_SECRET='long-random-string'
export NEKONEST_BOOTSTRAP_TOKEN='another-long-random-string'
export NEKONEST_TRANSPORT_MODE='open' # v0.2 operational default
export NEKONEST_ALLOWED_ORIGINS='https://nekonest.example.com'
# Behind a header-overwriting reverse proxy on this host:
export NEKONEST_TRUST_PROXY=1
# If the proxy is not loopback:
# export NEKONEST_TRUSTED_PROXY_CIDRS='10.0.0.0/8'
# Optional Web Push:
# export NEKONEST_VAPID_PUBLIC_KEY='…'
# export NEKONEST_VAPID_PRIVATE_KEY='…'
# export NEKONEST_VAPID_SUBJECT='mailto:you@example.com'
```

> [!WARNING]
> Without `NEKONEST_ADMIN_SECRET` (or its deprecated `NEKONEST_PHONE_SECRET` alias) the server binds **loopback only** (dev mode). Do not expose that mode publicly. With an admin secret set but bootstrap empty, **device registration is disabled**.

Manual register probe (normally the Windows daemon does this):

```bash
curl -X POST "https://nekonest.example.com/api/devices/register" \
  -H "Content-Type: application/json" \
  -H "X-Neko-Bootstrap: $NEKONEST_BOOTSTRAP_TOKEN" \
  -d '{"name":"study-pc"}'
```

## 3. systemd

`/etc/systemd/system/nekonest.service`:

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
# Prefer EnvironmentFile for secrets. If you must inline:
# Environment=NEKONEST_ADMIN_SECRET=…
# Environment=NEKONEST_BOOTSTRAP_TOKEN=…
# Environment=NEKONEST_TRANSPORT_MODE=open
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
sudo chmod 600 /opt/nekonest/.env   # if used
sudo systemctl daemon-reload
sudo systemctl enable --now nekonest
curl -sS http://127.0.0.1:8080/health
# {"status":"nyan~"}
```

Keep port **8080** on localhost; only 80/443 public via the proxy.

## 4. Reverse proxy

### Caddy

```caddy
nekonest.example.com {
  # Overwrite client XFF so the app sees a single trusted hop (set NEKONEST_TRUST_PROXY=1)
  reverse_proxy 127.0.0.1:8080 {
    header_up X-Forwarded-For {remote_host}
    header_up X-Real-IP {remote_host}
  }
}
```

### Nginx

WebSocket-capable location; **overwrite** (do not blindly append) client-supplied XFF:

```nginx
server {
  server_name nekonest.example.com;
  # tls termination here…

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

## 5. Acceptance

- [ ] `curl https://nekonest.example.com/health` → `nyan~`
- [ ] Browser opens PWA; admin bootstrap secret accepted and phone identity created
- [ ] Daemon can register with bootstrap token
- [ ] Phone pairs and sees device **online**
- [ ] Full path: [e2e-smoke.md](./e2e-smoke.md)

## 6. Upgrade

1. Build new `nekonest-server` and `pwa/dist` from a release tag or `main`.
2. Replace binary and `pwa-dist/` on the VPS.
3. **Preserve** `/opt/nekonest/data` (SQLite + attachments).
4. `sudo systemctl restart nekonest`
5. Re-run smoke checks.

Tags are not auto-deployed; production updates are operator-driven ([release.md](./release.md)).

## Related

- [Windows daemon deploy](./deploy-windows.md)
- [Linux daemon deploy](./deploy-linux.md)
- [Configuration](./configuration.md)
- [Security](./security.md)
- [Troubleshooting](./troubleshooting.md)
