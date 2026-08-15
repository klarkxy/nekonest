> English | [简体中文](./relay-core.zh-CN.md)

# Relay Core boundary

This is a contributor note for the reusable data plane in `relaycore/`. It is
not an operator deployment guide or a managed-service readiness statement.

## Boundary

`relaycore.Engine` represents one nest. It owns the shared relay behavior used
by the self-hosted Server and by any managed shell that embeds it:

- authenticated phone and daemon connections
- subscriptions and connection state
- prompt durability transitions
- open and sealed routing
- phone/device grants
- history, attachments, and push dispatch through explicit ports

The embedding shell owns deployment-specific concerns such as public ingress,
account identity, tenant selection, entitlements, placement, billing, storage
construction, and operational policy.

## Embedding rule

- The self-hosted Server constructs one Engine for its data directory.
- A multi-tenant service must select and authorize the tenant before traffic
  reaches an isolated Engine.
- Client-supplied tenant or backend routing values are never authority.
- Engine dependencies are provided through its public ports; deployment details
  do not belong in the Core.

The exported Go API and tests under `relaycore/` are authoritative. Managed
service contracts and rollout gates belong in the managed-service repository,
not in self-hosted operator docs.

## Related

- [Architecture](./architecture.md)
- [Protocol](./protocol.md)
- `relaycore/ports.go`
- `relaycore/engine_test.go`
