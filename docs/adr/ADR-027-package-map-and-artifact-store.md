# ADR-027 Package Map and ArtifactStore Contract

## Status

Accepted — 2026-08-15. Effective from v0.15.0. Amends ADR-011's
"filesystem and inmem Artifact backends" clause. Alias-package deletion
is deferred to a later minor (same window as `memory/`).

## Context

The public tree still carries thin alias packages (`flow/`,
`blackboard/`) and a second Memory package (`memory/`). ADR-011 designed
`api.ArtifactStore` with framework-shipped `artifact/filesystem` and
`artifact/inmem` backends. Those backends were never implemented;
release notes deferred the interface. Position D (ADR-012) later forbade
framework-shipped store implementations.

`api/pipeline.go` still exposes six optional host hooks
(IntentAnalyzer … TaskMonitor). They are used by `AdvanceRun`. They are
not a second runtime.

`internal/blackboard` Exchange/Claim/Finding types were deleted in
Phase 4; the live path is generic `BlackboardItem` plus handler/service.

## Decision

1. **`flow/` and `blackboard/` stay as deprecated aliases** in v0.15.
   New code imports `api`. A later minor deletes the packages.
2. **`memory/` follows ADR-021** (deprecated, deleted later).
3. **`api.ArtifactStore` is a Position D contract only.** The framework
   ships the type and verbs (`Put` / `Get` / `Describe` / `List`). It
   ships no filesystem, inmem, or cloud backend. Applications implement
   the interface against their own blob store. This supersedes ADR-011
   §"two default backends".
4. **Keep the six pipeline interfaces.** They are optional
   customization, not a parallel engine. Do not add a seventh without
   an ADR.
5. **Do not merge or split `internal/core` in this drop.** File-level
   splits that preserve exported aliases may land later without a new
   ADR if they do not change import paths.

## Anti-patterns rejected by this ADR

| Anti-pattern | Why it is wrong |
| ------------ | --------------- |
| Adding `artifact/filesystem` as a "demo" backend | Reopens Position D |
| Deleting `flow/` / `blackboard/` in the same drop as the deprecation | Denies hosts a compile window |
| Treating pipeline hooks as a second durable runtime | Violates ADR-008 Principle 2 |

## Impact

Hosts can implement Artifact storage without waiting for a framework
backend. Alias packages warn at doc time. Pipeline remains the
AdvanceRun customization seam.

## References

- ADR-011 (amended), ADR-012 Position D, ADR-006 (superseded)
- `api/artifact.go`, `api/pipeline.go`
- `flow/flow.go`, `blackboard/blackboard.go`
