# Venat Active Plan

## Release state

- v0.15.0 is the latest published release.
- Later-minor work (production pack content, OpenTelemetry, and artifact
  backends) stays in the unversioned backlog.

See the [product specification index](../product-spec/README.md), the
[v0.14.0 control-plane specification](../product-spec/v0.14.0/README.md), and
the [v0.15.0 release notes](../release-notes/v0.15.0.md).

## Current candidate scope

No new minor is open. Hosts should take v0.15.0 for the canonical public API,
lossless provider messages, governed tools, live turn control, and durable
subagent scheduler boundary.

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
