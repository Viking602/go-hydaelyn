# Venat Active Plan

## Release state

- v0.14.0 is the latest published release.
- Remaining contract, gate, and documentation alignment lands on this
  branch and does not open a new minor.
- Later-minor work (adapter deletion, production pack content,
  OpenTelemetry, artifact backends) stays in the unversioned backlog.

See the [product specification index](../product-spec/README.md), the
[v0.14.0 specification](../product-spec/v0.14.0/README.md), and the
[v0.14.0 release notes](../release-notes/v0.14.0.md).

## Current candidate scope

This branch finishes the remaining v0.14.x contract and gate work:

- Reserved `hydaelyn.self.*` capability names and `Run.AgentVersion`.
- Additive `RequiresLease` / `RequiresPolicy` on `api.Capability`.
- MCP `ToolsFromCapabilities` only; OpenAPI and CLI stay Deferred.
- Packs no longer ship eval cases in production code.
- Import-boundary and business-word gates cover the live reverse-edge
  bans, including test imports and named exceptions.
- Live `docs/architecture-boundaries.md` and released v0.14.0 docs.

## Release gates

Before merging:

1. Run `make verify` from a clean commit. That target now includes
   `architecture-check`.
2. Confirm `make architecture-check` is green on its own.
3. Do not treat unpublished later-minor ADRs as accepted scope.

## Deferred work

Advanced schedulers, memory pipelines, artifact storage, OpenTelemetry
integration, and production pack content remain in the
[unversioned future backlog](./future-backlog.md).

The separately assessed [architecture safety hardening plan](./architecture-safety-hardening.md)
tracks compatibility-safe leftovers. It does not replace the accepted
scheduler or durable Runner ADRs.
