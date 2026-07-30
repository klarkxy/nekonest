> English | [简体中文](./e2e-smoke.zh-CN.md)

# End-to-end smoke checklist

Acceptance path after deploy or deploy-sensitive changes. Cutting a release: [release.md](./release.md).

## Preconditions

- [ ] VPS server running; `GET /health` → `{"status":"nyan~"}`
- [ ] `NEKONEST_PHONE_SECRET` set (public)
- [ ] `NEKONEST_BOOTSTRAP_TOKEN` set and used at daemon register
- [ ] HTTPS / WSS work through the reverse proxy
- [ ] `NEKONEST_ALLOWED_ORIGINS` includes the public origin (recommended)
- [ ] Home PC registered; `config.json` holds a real device token
- [ ] Daemon process online (single instance for that config)
- [ ] At least one supported agent CLI has a recent main-thread session on the PC

## Steps

1. Open the PWA on the phone; enter the same phone secret as the VPS.  
2. Pair with the 6-digit code (`-pair gen` if needed); device list shows **online**.  
3. On the PC, open/use a supported agent so a recent thread exists.  
4. On the phone: device → **directory → agent → thread** visible.  
5. Open the session; tap the paperclip; system file picker opens; control is focusable / ~44px touch target.  
6. Choose one PNG &lt; 4 MB and one TXT/Markdown/PDF/JSON; send a prompt that asks the agent to read the files.  
7. Within seconds: agent correctly uses file content, **or** a clear upload / download / CLI error.  
8. After a first-time PWA upgrade from an older build: fully close and reopen the PWA once; later SW updates should auto-refresh once.  
9. Stop the daemon → phone shows device **offline**.  
10. Start the daemon → **online** again.  
11. Wrong phone secret → 401 / cannot operate.  
12. Optional: send a prompt, kill network briefly, restore—outbox should not silently mint a new `client_msg_id` for the same send.  
13. Optional: interrupt a long run if the agent supports it; confirm process tree does not linger on Windows.

## Known limitations (not failures)

- Phone does not create threads; PC-first only  
- Max 5 attachments, 4 MB each  
- Codex / Claude Code / Kilo use their CLI file/image mechanisms  
- Kimi CLI / Grok Build receive daemon-local paths in the prompt; sandbox may block reads  
- Approvals may require the PC terminal  
- Web Push needs full VAPID env; otherwise no real push  
- Daemon targets Windows  
- VPS sees and stores messages/attachments (no E2E encryption)

## Related

- [Troubleshooting](./troubleshooting.md)
- [VPS deploy](./deploy-vps.md)
- [Windows deploy](./deploy-windows.md)
