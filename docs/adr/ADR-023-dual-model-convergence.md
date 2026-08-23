# ADR-023 Dual-Model Convergence

## Status

Accepted — 2026-08-15. Effective as a *decision* from v0.15.0.
Implementation is incremental and must not start until Phases 1–4
correctness fixes are on the same branch (they are). Adapter deletion is
explicitly out of the v0.15 first drop.

## Context

Public contracts live in `api/`. The durable runner uses a parallel type
system in `internal/core/model`, with `internal/core/adapter` (~4800
lines) converting every command, store row, and error at the façade.
The adapter is the layer that hid wiring bugs such as B1 (ResumeTokens
reading the wrong snapshot).

Two models also double every storage change: a new field must land in
`api`, `model`, both conversion directions, and often `ports`.

## Decision

1. **`api` types become the internal spec types.** `internal/core` and
   `internal/*` domain packages import `api` for persisted values
   (Run, Task, Event, Lease, …). `internal/core/model` shrinks to
   runtime-only types that are not part of the public contract, then
   disappears.
2. **`internal/core/adapter` is deleted store-by-store**, not in one
   commit. Each PR converts one store or command family, updates tests,
   and removes the matching converters.
3. **The public `api` package remains the only host-facing contract.**
   Sinking types inward does not move business logic into `api/`.
4. **Do not rename exported `api` symbols during the migration.**

## Anti-patterns rejected by this ADR

| Anti-pattern | Why it is wrong |
| ------------ | --------------- |
| Deleting the adapter in one PR | Makes B-series regressions unbisectable |
| Inventing a third "canonical" type package | Recreates the dual model |
| Exporting `internal/core/model` to hosts | Breaks the façade rule in ADR-017 |

## Impact

Follow-up PRs cite this ADR and name the store they migrate. Until those
PRs land, `adapter` stays and the façade continues to convert.

## References

- ADR-017 Durable Runner boundary
- ADR-009 public-any contract
- `internal/core/adapter/`
- `api/`
