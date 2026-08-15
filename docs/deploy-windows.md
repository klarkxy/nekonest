> English | [简体中文](./deploy-windows.zh-CN.md)

# Install the Windows host daemon

The daemon runs on the computer that owns your native coding-agent threads. It
connects outbound to the VPS; no inbound port is required.

## Prerequisites

- Windows amd64
- A reachable NekoNest HTTPS URL and its bootstrap token
- At least one supported agent CLI installed and used once

## 1. Download and verify

```powershell
$asset = "nekonest-daemon-windows-amd64.zip"
$base = "https://github.com/klarkxy/nekonest/releases/latest/download"
Invoke-WebRequest "$base/$asset" -OutFile $asset
Invoke-WebRequest "$base/checksums.txt" -OutFile checksums.txt

$line = Get-Content checksums.txt | Where-Object { $_ -match "  $([regex]::Escape($asset))$" }
if (-not $line) { throw "Checksum entry not found" }
$expected = ($line -split '\s+')[0].ToLowerInvariant()
$actual = (Get-FileHash $asset -Algorithm SHA256).Hash.ToLowerInvariant()
if ($actual -ne $expected) { throw "Checksum mismatch" }

Expand-Archive $asset -DestinationPath D:\NekoNest -Force
D:\NekoNest\nekonest-daemon.exe -version
```

Keep the executable in a stable directory. Building from source is documented
in [development.md](./development.md).

## 2. Register and pair

```powershell
$env:NEKONEST_SERVER = "https://nekonest.example.com"
$env:NEKONEST_BOOTSTRAP_TOKEN = "same-bootstrap-token-as-vps"
D:\NekoNest\nekonest-daemon.exe -register -name "Study PC"
D:\NekoNest\nekonest-daemon.exe -doctor
```

Registration stores private state under `%USERPROFILE%\.nekonest` and prints
phone pairing material. Open the PWA, choose **Pair computer**, and enter the
printed code. Generate a replacement code later with:

```powershell
D:\NekoNest\nekonest-daemon.exe -pair gen
```

The registration environment variables are not needed for normal runs.

## 3. Run at logon

Optional foreground test:

```powershell
D:\NekoNest\nekonest-daemon.exe
```

Stop it with Ctrl+C, then register the current-user logon task and start it:

```powershell
D:\NekoNest\nekonest-daemon.exe install
D:\NekoNest\nekonest-daemon.exe start
D:\NekoNest\nekonest-daemon.exe status
```

`install` writes a per-user scheduled task (`NekoNestDaemon` for the default
config). The task runs as the logged-on user, has no 72-hour time limit, and
does not copy the binary. Keep the executable in a stable directory. A custom
`-config` path gets its own task name.

Only one process may use a daemon config at a time. If the task exits, check
`status` for a leftover manual process before changing the configuration.

## Verify

- `nekonest-daemon.exe status` reports the task installed and the process lock
  held after `start`.
- `nekonest-daemon.exe -doctor` has no critical config or network error.
- The phone shows the host online.
- A recent native thread appears under its project and agent.
- A short prompt streams a response.

Controls and attachments vary by installed agent. The PWA's enabled controls
are authoritative; see [agent support](./agent-capability-matrix.md).

## Upgrade and rollback

1. Download and verify the target release before stopping the current daemon.
2. Back up `%USERPROFILE%\.nekonest` and record the current executable hash.
3. Run `nekonest-daemon.exe stop` and confirm `status` shows the process lock
   free.
4. Rename the old executable to a unique rollback name, then install the new
   executable at the original path.
5. If the executable path changed, run `install` again. Then `start`, run
   `-doctor`, and verify phone online state and a real prompt.

If the new daemon does not stay connected, `stop` it, restore the previous
executable, and `start` the same task. Do not replace or edit native agent
stores during an upgrade.

## Related

- [VPS deploy](./deploy-vps.md)
- [Configuration](./configuration.md)
- [Troubleshooting](./troubleshooting.md)
- [Acceptance checklist](./e2e-smoke.md)
