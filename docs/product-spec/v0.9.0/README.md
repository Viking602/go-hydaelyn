# Hydaelyn v0.9.0

## Status

Released on 2026-06-29. Tag `v0.9.0` points to commit
[`78036a6`](https://github.com/Viking602/go-hydaelyn/commit/78036a68ca9fd1902ce60fb7d0f3f99bd053d953).

This file is a historical release record. It does not describe the current
unreleased branch.

## What shipped

v0.9.0 expanded the v0.8.0 foundation in five areas:

- Live agent streaming, sequential, router, and supervisor schedulers, a typed
  DAG with bounded concurrency, field-level fan-in, deep schema checks, and
  team-level streaming.
- A bounded agent loop with task budgets, typed failures, structured-output
  validation and repair, panic isolation, partial-trace preservation, model
  resolution, and subagent-as-tool support.
- Declarative workflow modeling that compiles to the existing `multiagent`
  graph, plus the redesigned `EvalCase` and `Harness` evaluation surface.
- A sandboxed coding agent with hashline edits and workspace boundary checks.
- Storage and runtime reliability work, including the three multi-agent stores,
  usage-record persistence, provider retry and fallback, and resume-token
  enumeration.

The release also translated ADR-001 through ADR-008 to English and rewrote the
project README around the current architecture.

The complete published change list is available in the
[`v0.8.1...v0.9.0` comparison](https://github.com/Viking602/go-hydaelyn/compare/v0.8.1...v0.9.0).

## Known gaps at release

v0.9.0 did not ship the previously proposed debate, MapReduce, or swarm
schedulers. It also did not ship a framework memory backend, an artifact store,
an OpenTelemetry exporter, or production-grade pack implementations. These
items now live in the [unversioned future backlog](../../plans/future-backlog.md)
without a release commitment.

The framework still follows Position D storage ownership: applications own
schema and storage implementations, while Hydaelyn owns the contracts and
conformance suite. The optional `Memory[T]` contract also ships without a
framework backend.

## Architecture references

- [v0.8.0 architecture boundaries](../v0.8.0/11-boundaries.md)
- [v0.8.0 package structure](../v0.8.0/12-package-structure.md)
- [ADR-012 storage contract](../../adr/ADR-012-storage-contract-position-c.md)
- [ADR-013 optional memory](../../adr/ADR-013-memory-kernel-vs-pipeline.md)
- [ADR-016 multi-agent scheduler](../../adr/ADR-016-explicit-multi-agent-scheduler.md)
- [ADR-017 durable runner boundary](../../adr/ADR-017-durable-runner-boundary.md)
