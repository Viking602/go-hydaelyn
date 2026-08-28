# Venat Active Plan

## Release state

- v0.15.4 is the latest published release.
- v0.16.0 is the active release candidate on `release/v0.16.0`.
- Production pack content, OpenTelemetry, and application artifact backends
  remain in the unversioned backlog.

See the [product specification index](../product-spec/README.md), the
[v0.16.0 candidate notes](../release-notes/v0.16.0.md), and the
[v0.15.4 release notes](../release-notes/v0.15.4.md).

## Current candidate scope

v0.16.0 completes deprecated-package and intermediate-façade removals, deletes
the redundant Flow/workflow and speculative built-in DAG surfaces in favor of
the `Scheduler` protocol, hardens response publication and worker recovery, and
ships the Experimental Harness/session contract with durable lane ownership.
It does not add Harness tools/hooks/budgets or move deferred artifact/memory
pipeline work into the kernel.

## Release gates

Before creating the v0.16.0 release commit or tag:

1. Run focused changed-contract tests.
2. Run `make verify`; it includes `architecture-check`.
3. Run `make ci-local` for staticcheck, vulnerability, and race parity.
4. Confirm release-note links and removed-API migration entries resolve.
5. Create the release commit only from a clean candidate tree.
6. Merge the candidate to `main` before pushing the annotated `v0.16.0` tag.

Candidate evidence on 2026-08-28: gates 1–4 pass. The release commit, merge,
tag, push, and GitHub Release remain intentionally unperformed during
preparation.

## Deferred work

Advanced schedulers, memory pipelines, artifact storage, OpenTelemetry
integration, and production pack content remain in the
[unversioned future backlog](./future-backlog.md).
