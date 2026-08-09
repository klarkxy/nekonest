> English | [简体中文](./migration-v1.zh-CN.md)

# Migrate v0.1 → v1.0

This is a **breaking** upgrade. There is no long-term mixed protocol. After migration:

- Daemon **device IDs and token hashes** are preserved (hosts stay registered).
- Server plaintext **messages, prompts, pair codes, push subs, phone identities, key packages, attachments** are cleared (after backup).
- Phones must **re-enter the admin secret (or new admin flow), bootstrap a phone token, and re-pair**.
- Native agent stores on the home PC are **never** modified by nest migration.

## Prerequisites

1. Stop the nest server and all daemons that write to it.
2. Note `NEKONEST_ADMIN_SECRET` / legacy `NEKONEST_PHONE_SECRET` and bootstrap token.
3. Disk space for a full copy of `data/`.

## Steps

```bash
# From server/ binary directory (paths are examples)
./nekonest-server -migrate-v1 -data ./data -backup ./data-backup-v1
```

The command:

1. Copies `nekonest.db` (+ WAL/SHM if present) and `attachments/` into `-backup`.
2. Writes `nekonest.db.sha256` for the backup database.
3. Opens the live DB, runs additive schema migrations, then deletes plaintext content tables listed above.
4. Clears the live attachments directory (restorable from backup).
5. Atomically persists `transport_mode=sealed` in the same transaction as the plaintext cleanup.

Then upgrade binaries (server, daemon, PWA). Do not assert `open`: the migrated database is now sealed. The Server may omit the mode variable or assert the persisted value:

```bash
# Optional assertion; mismatch refuses startup.
export NEKONEST_TRANSPORT_MODE=sealed
export NEKONEST_ADMIN_SECRET=...   # was NEKONEST_PHONE_SECRET
export NEKONEST_BOOTSTRAP_TOKEN=...
```

Start Server, register or update each Daemon so `config.json` persists sealed mode, run `nekonest-daemon -doctor`, generate a new pair code, and re-pair phones. An existing open nest that has **not** run this migration remains open; changing only the environment is intentionally rejected.

## Rollback

Restore the backup tree over `data/` only if you still run **v0.1** binaries. v1 clients cannot speak the old protocol. There is no automatic re-import of wiped nest plaintext into sealed history; recover conversation content from **native agent stores** on the host.

## Threat model notes (post-migration)

- Sealed mode: VPS stores ciphertext; may still see device ID, session ID, timestamps, sizes, connection metadata.
- Open mode: VPS can read application plaintext — admin-only, explicit config.
- Lost phone keys require re-pair; history rebuilds from native stores, not from old VPS ciphertext.
