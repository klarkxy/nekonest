> English | [简体中文](./relay-core.zh-CN.md)

# Relay Core and managed endpoint boundary

This decision record defines the reusable relay boundary shared by the
self-hosted Server and NekoNest Cloud. It does not make the managed service a
v1.0.0 requirement and it is not evidence that a public Cloud deployment is
ready.

## Decision

`relaycore.Engine` represents exactly one nest. A nest may contain multiple
positively authorized host identities. The open standalone Server constructs
one Engine; a private regional Cloud Relay constructs one isolated Engine per
tenant and keeps tenant selection outside the Engine.

```text
self-host: daemon / PWA -> standalone shell -> one Engine -> local stores
managed:   daemon / PWA -> stable ingress -> tenant registry -> one Engine per tenant
                                      ^
                                      | signed, short-lived authorization
                                Cloud control plane
```

The Core owns the wire protocol, WebSocket lifecycle, connection policy,
subscription state, prompt durability transitions, sealed-envelope routing,
phone/device grants, history, attachments, and push dispatch. Storage,
authentication, attachment, push, audit, and clock dependencies are explicit
ports or open adapters. Account identity, prices, subscriptions, host-slot
entitlements, regions, placement, D1, and Cloudflare remain outside the open
Core.

## Endpoint contract

The daemon persists one generic `server_url` and always connects to that
origin's `/ws/daemon`. A self-hosted URL names the standalone Server; an
official URL names a stable Cloud ingress. Registration may report
`connection_state=provisioning`, but it never returns a replacement Relay URL
and the daemon never polls a separate control-plane origin.

Approved retryable service states keep retrying that same endpoint until the
service becomes ready or the daemon is stopped; they do not consume the capped
generic network-failure budget. A structured terminal or unknown error still
stops fail closed. Non-101 WebSocket responses are bounded, parsed as the same
service-error shape, and never followed as redirects.

Existing v0.2.5 self-hosted configuration remains valid. The unreleased
`control_plane_url` / `activation_poll_path` handoff is rejected with an
explicit re-registration error rather than silently reinterpreted.

Protocol 1.3 adds a common error payload:

```json
{
  "error_code": "service_provisioning",
  "message": "Relay placement is not ready",
  "retryable": true,
  "retry_after_seconds": 15
}
```

Unknown error codes fail safely. `action_url`, when present, is display-only;
it is never an authentication or routing authority.

## Managed isolation and commercial boundary

- One Cloud account maps to one tenant/nest. Host slots limit non-revoked host
  identities, not concurrent browser tabs or native agent sessions.
- The control plane is the only authority for account, entitlement, placement,
  and device-revocation state. It supplies signed authorization snapshots to
  Relay nodes; the Core sees principals and allow/deny/revoke outcomes only.
- A client-supplied tenant id is never trusted. Device credentials and opaque
  phone route handles resolve the tenant before a request reaches its Engine.
- Each tenant starts with a separate SQLite database and attachment root.
  Active-active SQLite, cross-region shared files, and dual writes are not
  supported.
- Cloud uses `reject_new` for a duplicate live daemon identity. Copying the
  same credential cannot prove a second physical machine, but it cannot create
  two simultaneous connections or consume two identities accidentally. The
  rejected daemon retries the same endpoint until the stale lease expires.
- Managed transport is sealed. Cloud login can bootstrap an independent phone
  identity but cannot create phone-to-device grants; each device still requires
  the existing E2E pairing flow.

## Availability and revocation

Authorization snapshots are Ed25519-signed, valid for at most five minutes,
refreshed every minute, and reconciled against authorization revisions at
least every 15 seconds. A new principal requires a live control-plane decision.
During a control-plane outage, an already authenticated connection may remain
only until its current snapshot expires; expiry closes the tenant connections.
Normal device revocation targets a 15-second disconnect and does not wait for
snapshot expiry.

Tenants are pinned to a home region. A region move is a single-writer,
generation-fenced operation: quiesce, copy and verify SQLite plus attachments,
atomically switch placement, then drain the old node. There is no client-visible
backend URL and no automatic active-active failover.

## Release gates

Local unit tests prove contracts, not a managed service. Public paid use remains
blocked until a real stable ingress, node identity/rotation, cross-tenant
negative tests, backup/restore, region migration/rollback, permanent account
purge, retention policy, monitoring/on-call, and exact-build live E2E evidence
all pass.
