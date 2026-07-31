# Venat Active Plan

## Release state

- v0.9.0 is the latest published release. It was tagged on 2026-06-29 at
  `78036a6`.
- v0.10.0 is the recommended next version and remains unreleased.
- The current work is a stabilization release, not a scheduler, memory, or
  product-pack expansion.

See the [product specification index](../product-spec/README.md), the
[v0.9.0 release record](../product-spec/v0.9.0/README.md), and the
[unreleased v0.10.0 notes](../release-notes/v0.10.0.md).

## Current candidate scope

The v0.10.0 candidate contains:

- Agent Skills trusted discovery, standards-compatible parsing, registry
  resolution, explicit and model-driven activation, bounded resource reads,
  and compaction-safe context. Skills remain instruction bundles and do not
  grant tool permissions or execute scripts automatically.
- Runtime and workspace hardening merged after v0.9.0.
- An MCP client backed by `github.com/modelcontextprotocol/go-sdk`, including
  official stdio lifecycle handling and Streamable HTTP session behavior.
- Go 1.25.12 as the pinned toolchain, with the prior reachable standard-library
  vulnerability removed.
- CLI version output derived from Go build metadata.
- A `go list` based import-boundary gate and a release workflow that rejects
  tags outside `main`, pins analysis tools, runs architecture checks, and tests
  the installed tag surface.

## Release gates

Before creating a v0.10.0 tag:

1. Run `make verify`, `make ci-local`, and `make architecture-check` from a
   clean release commit.
2. Run the MCP stdio and Streamable HTTP integration tests, including request
   cancellation and session close.
3. Install the CLI from an isolated tag fixture, then test `version`, `--help`,
   and an invalid command.
4. Build and test a temporary consumer that imports `skill` from the same tag.
5. Confirm the release commit is contained in `origin/main`.
6. Review the breaking changes and migration notes in the v0.10.0 release
   notes.

The release workflow repeats these checks before it creates a GitHub Release.
No tag or release should be created until the candidate is committed to `main`
and the full gate passes.

## Deferred work

Advanced schedulers, memory pipelines, artifact storage, OpenTelemetry
integration, and production pack content are not part of this stabilization
release. They remain in the [unversioned future backlog](./future-backlog.md).

The separately assessed [architecture safety hardening plan](./architecture-safety-hardening.md)
tracks compatibility-safe fixes extracted from the broader A-K proposal. It
does not replace the accepted scheduler or durable Runner ADRs.
