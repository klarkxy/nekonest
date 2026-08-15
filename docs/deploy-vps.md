> English | [简体中文](./deploy-vps.zh-CN.md)

# Deploy the VPS Server

This guide installs the Server and matching PWA with Docker Compose, keeps the
application port private, and publishes only HTTPS/WSS through a reverse proxy.

## Prerequisites

- A Linux VPS with Docker Compose
- A DNS name pointing to the VPS
- Caddy, Nginx, or another WebSocket-capable TLS proxy
- Two different long random secrets

## 1. Prepare the deployment

```bash
git clone https://github.com/klarkxy/nekonest.git
cd nekonest
cp docker.env.example .env
sudo install -d -m 700 -o 10001 -g 10001 /var/lib/nekonest
```

Edit `.env` and replace every placeholder. Add:

```dotenv
NEKONEST_DATA_DIR=/var/lib/nekonest
# Recommended for a controlled production upgrade:
# NEKONEST_IMAGE=ghcr.io/klarkxy/nekonest-server:vX.Y.Z
```

Use distinct values for `NEKONEST_ADMIN_SECRET` and
`NEKONEST_BOOTSTRAP_TOKEN`. Set `NEKONEST_ALLOWED_ORIGINS` to the exact public
HTTPS origin. Keep `.env` private.

New data directories default to sealed transport. Choose open transport only
deliberately, by setting `NEKONEST_TRANSPORT_MODE=open` before the first start.
The stored mode cannot be switched later by editing the environment.

## 2. Start and inspect

```bash
docker compose pull
docker compose up -d
docker compose ps
docker compose logs -f server
```

The Compose file publishes the application only on `127.0.0.1:8080`. Check it
locally before configuring TLS:

```bash
curl -fsS http://127.0.0.1:8080/health
```

The response should contain `"status":"nyan~"`.

## 3. Publish HTTPS/WSS

### Caddy

```caddy
nekonest.example.com {
  reverse_proxy 127.0.0.1:8080 {
    header_up X-Forwarded-For {remote_host}
    header_up X-Real-IP {remote_host}
  }
}
```

### Nginx

```nginx
server {
  server_name nekonest.example.com;
  # Configure TLS certificates here.

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

`NEKONEST_TRUST_PROXY=1` is safe only when the proxy overwrites forwarded client
headers as shown. If the proxy connects from another network, also configure
`NEKONEST_TRUSTED_PROXY_CIDRS`.

Keep port 8080 firewalled from the public internet. Only ports 80/443 should be
public.

## 4. First-use check

1. Open `https://nekonest.example.com`.
2. Complete setup with the admin secret.
3. Install and register a [Windows](./deploy-windows.md) or
   [Linux](./deploy-linux.md) host daemon with the bootstrap token.
4. Pair the host and run the [acceptance checklist](./e2e-smoke.md).

## Back up

The Server data directory contains the database and attachments. Back it up as
one unit, together with the private `.env` file. A simple cold backup is:

```bash
docker compose down
sudo tar -C /var/lib -czf "nekonest-backup-$(date +%Y%m%d-%H%M%S).tar.gz" nekonest
docker compose up -d
```

Store backups encrypted and test restoration away from production. Do not copy
only the SQLite file while ignoring its companion files or attachments.

## Upgrade and rollback

1. Read the target release notes and record the current image reference/digest.
2. Take a backup and confirm `/health` before changing anything.
3. Set `NEKONEST_IMAGE` to the target immutable `vX.Y.Z` image.
4. Run `docker compose pull && docker compose up -d`.
5. Verify local and public `/health`, logs, host reconnect, and the changed user
   workflow.

If the new container fails, restore the previous image reference and recreate
the service. Restore data only when release notes identify a data migration that
requires it.

Building the Server from source is a contributor workflow; see
[development.md](./development.md).

## Related

- [Configuration](./configuration.md)
- [Security](./security.md)
- [Troubleshooting](./troubleshooting.md)
