# ADR-020 v0.15 Architecture Program

## Status

Accepted and implemented — 2026-08-23. Effective from v0.15.0.
Companion ADRs: ADR-021 through ADR-027. Builds on ADR-008, ADR-011,
ADR-012, ADR-013, ADR-015, ADR-016, and ADR-017.

**Amended for v0.16.0 — 2026-08-28.** The compatibility windows opened by
ADR-021, ADR-025, and ADR-027 are complete. The deprecated alias packages and
intermediate domain façades are removed. The unused `api.ArtifactStore`
contract is also removed under the demand-driven API rule; opaque artifact
references remain application-owned values.

## Context

A full-tree review recorded architecture, runtime, and extension defects
as A / B / C items. Phases 1–4 of the repair program fixed correctness,
security, robustness, and tests without breaking public contracts. The
remaining A-series items are structural: two Memory surfaces, a monolithic
`UnitOfWork`, a dual api/internal model plus a large adapter, an unnamed
worker integration layer, an oversized Runner façade, duplicate identity
types, leftover alias packages, and an unimplemented Artifact store.

Doing all of that in one code drop would hide review seams and raise
regression risk. This ADR records the program order and the rule that
each item lands as its own decision (and, where the code is large, its
own PR) before implementation proceeds.

## Decision

v0.15.0 is the architecture major for this program. Pre-v1 SemVer still
allows breaking public changes on a minor. The work proceeds in this
dependency order:

1. **ADR-021** — `api.Memory[T]` is the only Memory contract; `memory/`
   becomes a deprecated compatibility package.
2. **ADR-022** — split `UnitOfWork` into capability interfaces; keep the
   composite as the transition type.
3. **ADR-023** — converge the dual model by sinking `api` types into
   `internal/core` as the spec types, then delete `internal/core/model`
   and `internal/core/adapter`.
4. **ADR-024** — document five layers, including the worker integration
   layer, and machine-check the new import seams.
5. **ADR-025** — slim the Runner façade (typed methods, domain
   sub-façades, retire no-context helpers).
6. **ADR-026** — unify agent identity and persist multi-agent
   collaboration values through `api` types.
7. **ADR-027** — clean the package map and keep `api.ArtifactStore` as a
   Position D contract with no shipped backend.

The ordered implementation is complete for v0.15: narrow capability
interfaces, canonical `api` model types, explicit domain sub-façades,
machine-checked import seams, and the contract-only Artifact surface.
The deprecated `memory/`, `flow/`, and `blackboard/` aliases remain only
for the separately documented compatibility window.

## Anti-patterns rejected by this ADR

| Anti-pattern | Why it is wrong |
| ------------ | --------------- |
| One PR that deletes the adapter, splits every store, and renames the worker package | Hides regressions behind an unreviewable diff |
| Shipping a framework Memory or Artifact backend "just for demos" | Reopens ADR-012 Position D |
| Treating `memory.Memory[T]` as the v0.8+ canonical surface | Contradicts ADR-013 |

## Impact

Reviewers can cite a numbered ADR instead of re-arguing the program.
Follow-up PRs must not invert the order above without amending this ADR.

## References

- Companion ADRs: ADR-021 … ADR-027
- Storage stance: ADR-012 (Position D)
- Memory stance: ADR-013
- Layer history: ADR-015, ADR-016, ADR-017
