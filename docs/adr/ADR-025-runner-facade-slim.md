# ADR-025 Runner Façade Slimming

## Status

Implemented — 2026-08-23. Effective from v0.15.0.

**Amended for v0.16.0 — 2026-08-28.** The temporary `RunAdmin`,
`Governance`, and `Blackboard` sub-façades and the remaining duplicate read
helpers are removed. Typed methods directly on `*Runner` are the canonical
surface; raw store methods remain explicitly low-level administration.

## Context

`Runner` accumulated every lifecycle, governance, blackboard, mailbox,
and raw store verb, plus `ExecuteCommand(ctx, api.Command) (any, error)`
and several no-context helpers that hide cancellation and storage
errors. Hosts treat the façade as a god object; generic command
dispatch bypasses typed results.

## Decision

1. **Typed methods remain the supported application API.**
   `ExecuteCommand` stays in v0.15 as a deprecated generic escape hatch
   for replay/admin tools. It is removed in a later minor once those
   tools have typed equivalents. It must not grow new call sites.
2. **Delete no-context public helpers** that already have context
   counterparts:
   `Events`, `Replay`, `ReplayRunState`, `ReadyTasks`,
   `ActiveLeaseCount`, `TraceSpans`, `ResponseOutbox`.
   Callers use `RunEvents`, `ReplayContext`, `ReplayRunStateContext`,
   `ReadyTasksContext`, `ActiveLeaseCountContext`, `ListTraceSpans`,
   `ResponseOutboxContext`.
3. **Expose explicit domain sub-façades** without embedding `*Runner`:
   `Runner.Admin() RunAdmin`, `Runner.Governance() Governance`,
   `Runner.Blackboard() Blackboard`.
   Each value forwards only its named domain methods. Raw store verbs
   remain on `Runner` for source compatibility in v0.15 but are documented
   as administration; they are not promoted through the other façades.
4. **Do not add new no-context methods.**

## Anti-patterns rejected by this ADR

| Anti-pattern | Why it is wrong |
| ------------ | --------------- |
| New `ExecuteCommand` call sites in examples or packs | Reopens the untyped result hole |
| Restoring no-context helpers "for convenience" | Hides `context` and store errors |

## Impact

v0.15 is a source break for callers of deleted no-context methods and for
code that treated a domain sub-façade as the complete Runner. `ExecuteCommand`
still compiles with a deprecation comment.

## References

- ADR-009 public-any contract
- ADR-017 Durable Runner boundary
- `runner.go`, `run.go`, `task.go`, `governance.go`, `response.go`,
  `facade.go`
