# Venat Active Plan

## Release state

- v0.14.1 is the latest published release.
- Later-minor work (adapter deletion, production pack content,
  OpenTelemetry, artifact backends) stays in the unversioned backlog.

See the [product specification index](../product-spec/README.md), the
[v0.14.0 specification](../product-spec/v0.14.0/README.md), and the
[v0.14.1 release notes](../release-notes/v0.14.1.md).

## Current candidate scope

No new minor is open. Hosts should take v0.14.1 for the fail-closed
runtime and contract alignment that landed after v0.14.0.

## Release gates

Before creating a later tag:

1. Run `make verify` from a clean release commit. That target includes
   `architecture-check`.
2. Confirm `make architecture-check` is green on its own.
3. Confirm the release commit is contained in `origin/main`.
4. Do not treat unpublished later-minor ADRs as accepted scope.

## Deferred work

Advanced schedulers, memory pipelines, artifact storage, OpenTelemetry
integration, and production pack content remain in the
[unversioned future backlog](./future-backlog.md).
