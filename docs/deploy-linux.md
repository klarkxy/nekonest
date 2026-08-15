> English | [简体中文](./deploy-linux.zh-CN.md)

# Install the Linux host daemon

The daemon runs on the Linux host that owns your native coding-agent threads
and connects outbound to the VPS.

## Prerequisites

- Linux amd64 or arm64
- A reachable NekoNest HTTPS URL and its bootstrap token
- At least one supported agent CLI installed and used once

## 1. Download and verify

```bash
# Use arm64 instead of amd64 on an ARM host.
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

Keep `~/.local/bin` on `PATH`. Building from source is documented in
[development.md](./development.md).

## 2. Register and pair

```bash
export NEKONEST_SERVER=https://nekonest.example.com
export NEKONEST_BOOTSTRAP_TOKEN='same-bootstrap-token-as-vps'
~/.local/bin/nekonest-daemon -register -name "Home Linux"
~/.local/bin/nekonest-daemon -doctor
```

Registration stores private state under `~/.nekonest` and prints phone pairing
material. Open the PWA, choose **Pair computer**, and enter the printed code.
Generate another code later with:

```bash
nekonest-daemon -pair gen
```

The registration environment variables are not needed for normal runs.

## 3. Run with systemd

Create `~/.config/systemd/user/nekonest-daemon.service`:

```ini
[Unit]
Description=NekoNest host daemon
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%h/.local/bin/nekonest-daemon
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
```

Enable it:

```bash
systemctl --user daemon-reload
systemctl --user enable --now nekonest-daemon.service
journalctl --user -u nekonest-daemon -f
```

`loginctl enable-linger "$USER"` is optional when the daemon must keep running
after logout. Only one process may use a daemon config at a time.

## Verify

- `nekonest-daemon -doctor` has no critical config or network error.
- `systemctl --user is-active nekonest-daemon` reports `active`.
- The phone shows the host online and lists a recent native thread.
- A short prompt streams a response.

Controls and attachments vary by installed agent. The PWA's enabled controls
are authoritative; see [agent support](./agent-capability-matrix.md).

## Upgrade and rollback

1. Download and verify the target release before stopping the service.
2. Back up `~/.nekonest` and record the current executable hash.
3. Stop the user service and save the old binary under a unique rollback name.
4. Install the new binary at the existing path and restart the service.
5. Run `-doctor`, inspect the journal, and verify phone online state and a real
   prompt.

If the new daemon fails, stop it, restore the previous binary, and start the
same service. Do not replace or edit native agent stores during an upgrade.

## Related

- [VPS deploy](./deploy-vps.md)
- [Configuration](./configuration.md)
- [Troubleshooting](./troubleshooting.md)
- [Acceptance checklist](./e2e-smoke.md)
