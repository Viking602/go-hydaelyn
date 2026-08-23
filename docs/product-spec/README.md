# Venat Product Specification

These documents organize maintained historical design records by release. The
corresponding Git tag is the source for exact shipped code. Unreleased work and
future ideas are labeled separately so that a version directory never doubles
as a promise.

## Versions

| Version | Status | Scope |
|---------|--------|-------|
| [v0.8.0](./v0.8.0/) | Released 2026-05-25 | Durable typed multi-agent foundation, bounded agent loop, explicit scheduler layer, Position D storage contract, context contracts, and evaluation primitives |
| [v0.9.0](./v0.9.0/) | Released 2026-06-29 at `78036a6` | Streaming and scheduler implementations, bounded agent execution, workflow modeling, evaluation and coding-agent surfaces, plus storage and runtime reliability fixes |
| [v0.10.0](../release-notes/v0.10.0.md) | Released 2026-07-16; tag retracted | Agent Skills, MCP conformance, security and CLI fixes, executable architecture boundaries, and release-gate hardening; use v0.10.1 |
| [v0.11.0](../release-notes/v0.11.0.md) | Released 2026-07-18 | Context-aware compaction, tool-call sequencing, and bounded history preparation |
| [v0.12.0](../release-notes/v0.12.0.md) | Released 2026-07-31 | Project, module, package, and CLI rename from Hydaelyn to Venat |
| [v0.13.0](../release-notes/v0.13.0.md) | Released 2026-08-01 | Durable execution recovery, typed outcomes, checkpoint restoration, provider reliability, and Responses API support |
| [v0.14.0](./v0.14.0/) | Released | Executable agent definitions, single-agent lifecycle orchestration, transactional admission, resource claims, granular usage, policy obligations, and storage conformance extensions |
| [v0.14.1](../release-notes/v0.14.1.md) | Released 2026-08-18 | Fail-closed policy, lease, and read APIs; ProcessTool and coding-sandbox I/O; reserved `hydaelyn.self.*`; `Run.AgentVersion`; MCP capability export; architecture-gate hardening |
| [v0.14.2](../release-notes/v0.14.2.md) | Released 2026-08-20 | Skill tool misses return `IsError` results; hashline edits recover by current-file anchors |
| [v0.15.0](../release-notes/v0.15.0.md) | Released 2026-08-23 | Canonical public API boundaries, lossless multimodal messages, strict provider streams, validated concurrent tools, durable turn control, stable Skills, and host-scheduled subagents |
| [Future backlog](../plans/future-backlog.md) | Unversioned | Scheduler expansion, memory and artifact work, OpenTelemetry integration, and production pack content |

The latest published release is v0.15.0.

## Architecture

Venat's documentation map is five layers. The linear picture is not a
hard DAG; reverse-edge bans are the executable rule:

```text
Packs / Workflow / Examples
        ↓ host wiring
Worker integration (worker/)
        ↓
Multi-Agent Layer  (multiagent/)
        ↓
Agent Loop Layer   (agent/)
        ↓
Durable Runner     (root + internal/)
```

The live boundary document is
[`docs/architecture-boundaries.md`](../architecture-boundaries.md).
The executable import-boundary rules live in
[`scripts/check-import-boundaries.sh`](../../scripts/check-import-boundaries.sh).
The historical v0.8.0 architecture record is
[`11-boundaries.md`](./v0.8.0/11-boundaries.md).

## Reading order

1. Read the [latest released notes](../release-notes/v0.15.0.md).
2. Read the [live architecture boundaries](../architecture-boundaries.md)
   and the [v0.8.0 package structure](./v0.8.0/12-package-structure.md).
3. Review the [v0.14.0 specification](./v0.14.0/) for the current
   control-plane surface.
4. Treat the [future backlog](../plans/future-backlog.md) as uncommitted scope.

## Conventions

- A released version directory is a maintained historical design record. Use
  the corresponding Git tag when exact code is required.
- The Agent Skills procedure clarification now present in the v0.8.0 boundary
  document is a post-release interpretation. It does not claim that Agent
  Skills shipped in the v0.8.0 tag.
- Unreleased notes state their status at the top and do not claim availability.
- Deferred work belongs in the unversioned backlog unless a release plan has
  accepted it.
- Architecture decisions live in [`docs/adr`](../adr/README.md).

The current multi-agent architecture boundary is maintained in
[ADR-016](../adr/ADR-016-explicit-multi-agent-scheduler.md).
