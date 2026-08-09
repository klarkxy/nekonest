> English | [简体中文](./deploy-windows.zh-CN.md)

# Windows daemon deploy

Run the outbound NekoNest daemon on the home PC so the phone can resume existing agent threads.

Configuration reference: [configuration.md](./configuration.md).

## Prerequisites

- Windows PC where you already use at least one supported agent CLI
- At least one existing native thread to seed the daemon's discovered project set; an installed/probed starter may then create threads only inside that set
- Reachable nest URL (`https://…`) and the same `NEKONEST_BOOTSTRAP_TOKEN` as the VPS
- Go 1.22+ if building from source

Supported agents (summary):

| Agent | Native store (typical) | Resume entry | Attachments |
|---|---|---|---|
| Claude Code | `~/.claude/projects` | `claude --resume` | Authorize temp dir; paths in prompt |
| Codex | `~/.codex/sessions` | `codex exec resume` | Native image args; other files via restricted dir + paths |
| Kilo | Kilo / OpenCode local DB | `kilo run --session` | Native `--file` |
| Kimi CLI | `.kimi-code` (legacy `.kimi`) | `kimi --session` | Local paths in prompt; CLI permissions apply |
| Grok Build | `~/.grok/sessions` | `grok --resume` | Local paths in prompt; non-interactive safe mode |

Missing CLI or empty store for one agent does not disable the others.

## 1. Build

```powershell
git clone https://github.com/klarkxy/nekonest.git
Set-Location nekonest\daemon
$env:CGO_ENABLED = "0"
go build -trimpath -ldflags="-s -w" -o nekonest-daemon.exe ./cmd/daemon
```

Place the exe somewhere stable, e.g. `D:\nekonest\bin\nekonest-daemon.exe`.

## 2. Register with the VPS

```powershell
$env:NEKONEST_SERVER = "https://nekonest.example.com"
$env:NEKONEST_BOOTSTRAP_TOKEN = "same-as-vps-bootstrap-token"
.\nekonest-daemon.exe -register -name "Study PC"
```

On success the daemon:

- Writes `%USERPROFILE%\.nekonest\config.json` (`server_url`, `device_id`, `token`, …)
- Prints a **6-digit** phone pair code (short TTL, ~5 minutes)

Enter that code in the PWA under **Pair computer**.

New code later:

```powershell
.\nekonest-daemon.exe -pair gen
```

Custom config path: `-config C:\path\to\config.json`.

## 3. Run

```powershell
.\nekonest-daemon.exe
```

Logs should include authentication for your `device_…` id. Only **one** process may use a given config path (`.daemon.lock`); a second instance exits.

### Autostart — Task Scheduler

1. Task Scheduler → Create Basic Task  
2. Trigger: **At log on**  
3. Action: start `D:\path\to\nekonest-daemon.exe`  
4. Start in: directory containing the exe  

PowerShell (current user at logon):

```powershell
$exe = "D:\path\to\nekonest-daemon.exe"
$action = New-ScheduledTaskAction -Execute $exe -WorkingDirectory (Split-Path $exe)
$trigger = New-ScheduledTaskTrigger -AtLogOn
Register-ScheduledTask -TaskName "NekoNestDaemon" -Action $action -Trigger $trigger -Description "NekoNest"
```

### Autostart — Startup folder fallback

If Task Scheduler is restricted, create a shortcut in the user Startup folder:

```text
%APPDATA%\Microsoft\Windows\Start Menu\Programs\Startup\
```

Point the shortcut at `nekonest-daemon.exe` with “Start in” set to the exe directory.

## 4. Optional: Windows Defender exclusions

Only if AV false-positives block a self-hosted binary you built:

```powershell
# Administrator
Add-MpPreference -ExclusionPath "D:\path\to\daemon-or-bin"
Add-MpPreference -ExclusionProcess "nekonest-daemon.exe"
```

This widens local attack surface—use deliberately.

## 5. Day-to-day usage

1. On the PC, use Claude Code / Codex / Kilo / Kimi CLI / Grok Build normally so threads exist.  
2. Daemon reports the last 7 days of native activity, with running/waiting threads always visible; normal periodic discovery runs about 30 seconds after the prior scan completes.
3. Phone: **directory → agent → thread**, then send prompts or attachments.  
4. Threads without a project directory appear under **未分类**.  
5. Approvals the non-interactive CLI cannot host must be finished on the **PC terminal**.

Attachment limits: max **5** files, **4 MB** each; see [configuration.md](./configuration.md).

## 6. Upgrade

1. Merge/push the approved change and build a new `nekonest-daemon.exe` from
   that exact commit. Record its SHA-256 and verify `-version`.
2. Inspect the actual running process path, command line/config path, and launcher
   or scheduled task. Do not assume the example path or default profile.
3. Verify exactly one daemon owns that config, stop that PID, and move the old
   executable to a unique rollback filename before placing the new binary.
4. **Keep** the active `config.json`, prompt journal, lock siblings, and native
   agent stores unchanged; compare the config hash before and after replacement.
5. Start through the existing launcher. If the new process does not remain alive,
   restore the rollback executable and start it through the same launcher.
6. Confirm a new PID, deployed hash, outbound WSS connection, Server-side daemon
   reconnect, and phone **online** state. Run [e2e-smoke.md](./e2e-smoke.md).
7. For discovery/cache/scheduler changes, sample CPU, native-store read throughput,
   working set, and handles across multiple discovery cycles; compare with the
   pre-deploy baseline rather than relying on build/test success.

Changing `device_id` / `token` requires re-register (new credentials) and a process restart; hot-reload does not swap identity mid-process.

## Related

- [VPS deploy](./deploy-vps.md)
- [Troubleshooting](./troubleshooting.md)
- [Architecture](./architecture.md)
