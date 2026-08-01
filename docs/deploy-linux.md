> English | [简体中文](./deploy-linux.zh-CN.md)

# Deploy the Linux host daemon

Formal v1 host OS includes **Linux** (with Windows). Baseline smoke targets:
Ubuntu 22.04+ and Debian 12+ on `amd64` / `arm64`.

## Build

From `daemon/`:

```bash
go build -o nekonest-daemon ./cmd/daemon
install -Dm755 nekonest-daemon ~/.local/bin/nekonest-daemon
```

Cross-compile examples:

```bash
GOOS=linux GOARCH=amd64 go build -o nekonest-daemon-linux-amd64 ./cmd/daemon
GOOS=linux GOARCH=arm64 go build -o nekonest-daemon-linux-arm64 ./cmd/daemon
```

## Register and pair

```bash
export NEKONEST_SERVER=https://your-nest.example
export NEKONEST_BOOTSTRAP_TOKEN=...   # when nest admin secret is set
nekonest-daemon -register -name "Home Linux"
nekonest-daemon -pair gen             # prints code + QR JSON + fingerprint
```

Paste the QR JSON into the PWA pair screen and verify the fingerprint.

## Doctor

```bash
nekonest-daemon -doctor
```

Checks OS, transport mode, config, E2E identity file, each agent CLI, and nest `/health`.

## systemd user unit

```bash
mkdir -p ~/.config/systemd/user
cp packaging/nekonest-daemon.service ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now nekonest-daemon.service
loginctl enable-linger "$USER"   # optional: keep running after logout
```

Logs:

```bash
journalctl --user -u nekonest-daemon -f
```

## Transport mode

v0.2 defaults to open transport. You may state it explicitly with:

```bash
export NEKONEST_TRANSPORT_MODE=open
```

on **both** nest server and daemon (and keep the PWA default open). Sealed is an explicit preview mode; mismatched modes reject the handshake.
