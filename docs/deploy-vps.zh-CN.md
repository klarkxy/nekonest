> [English](./deploy-vps.md) | 简体中文

# 部署 VPS Server

本指南用 Docker Compose 安装 Server 与匹配的 PWA，保持应用端口私有，只通过
反向代理公开 HTTPS/WSS。

## 前置条件

- 安装了 Docker Compose 的 Linux VPS
- 已指向 VPS 的域名
- Caddy、Nginx 或其他支持 WebSocket 的 TLS 反代
- 两个不同的长随机密钥

## 1. 准备部署

```bash
git clone https://github.com/klarkxy/nekonest.git
cd nekonest
cp docker.env.example .env
sudo install -d -m 700 -o 10001 -g 10001 /var/lib/nekonest
```

编辑 `.env`，替换所有占位值，并加入：

```dotenv
NEKONEST_DATA_DIR=/var/lib/nekonest
# 生产环境受控升级建议固定版本：
# NEKONEST_IMAGE=ghcr.io/klarkxy/nekonest-server:vX.Y.Z
```

`NEKONEST_ADMIN_SECRET` 与 `NEKONEST_BOOTSTRAP_TOKEN` 必须使用不同的
值。`NEKONEST_ALLOWED_ORIGINS` 填准确的公网 HTTPS 来源。保护好 `.env`。

新数据目录默认使用 sealed 传输。只有在明确接受 open 模式时，才在第一次启动
前设置 `NEKONEST_TRANSPORT_MODE=open`。以后修改环境变量不能切换已保存模式。

## 2. 启动并检查

```bash
docker compose pull
docker compose up -d
docker compose ps
docker compose logs -f server
```

Compose 只把应用发布到 `127.0.0.1:8080`。配置 TLS 前先检查本机端点：

```bash
curl -fsS http://127.0.0.1:8080/health
```

响应应包含 `"status":"nyan~"`。

## 3. 发布 HTTPS/WSS

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
  # 在这里配置 TLS 证书。

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

只有当反代像示例一样覆盖客户端转发头时，`NEKONEST_TRUST_PROXY=1` 才安全。
代理从其他网络连接时，还要配置 `NEKONEST_TRUSTED_PROXY_CIDRS`。

不要把 8080 暴露到公网。公网只开放 80/443。

## 4. 首次使用检查

1. 打开 `https://nekonest.example.com`。
2. 用管理员密钥完成初始化。
3. 用注册令牌安装并注册 [Windows](./deploy-windows.zh-CN.md) 或
   [Linux](./deploy-linux.zh-CN.md) 主机 Daemon。
4. 配对主机并运行[验收清单](./e2e-smoke.zh-CN.md)。

## 备份

Server 数据目录包含数据库与附件。把它和私有 `.env` 一起备份。简单的冷备份：

```bash
docker compose down
sudo tar -C /var/lib -czf "nekonest-backup-$(date +%Y%m%d-%H%M%S).tar.gz" nekonest
docker compose up -d
```

加密保存备份，并在非生产环境测试恢复。不要只复制 SQLite 主文件而忽略伴随文件
或附件。

## 升级与回滚

1. 阅读目标版本说明，记录当前镜像引用或 digest。
2. 先备份，并确认升级前 `/health` 正常。
3. 将 `NEKONEST_IMAGE` 设为目标不可变 `vX.Y.Z` 镜像。
4. 运行 `docker compose pull && docker compose up -d`。
5. 检查本机与公网 `/health`、日志、主机重连及本次变更的用户流程。

新容器失败时，恢复原镜像引用并重建服务。只有版本说明明确涉及数据迁移时，才
恢复数据备份。

从源码构建 Server 属于贡献者流程，见
[development.zh-CN.md](./development.zh-CN.md)。

## 相关文档

- [配置](./configuration.zh-CN.md)
- [安全](./security.zh-CN.md)
- [排障](./troubleshooting.zh-CN.md)
