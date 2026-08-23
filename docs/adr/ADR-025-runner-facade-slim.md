# ADR-025 Runner Façade Slimming

## Status

Accepted — 2026-08-15. Effective from v0.15.0.

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
3. **Expose domain sub-façades** that embed `*Runner` so hosts can pass
   a narrower value:
   `Runner.Admin() RunAdmin`, `Runner.Governance() Governance`,
   `Runner.Blackboard() Blackboard`.
   Raw store verbs (`SaveRun`, `Begin`, `StoreProvider`, …) stay on
   `Runner` in v0.15 and are documented as administration. A later PR
   may stop promoting them on `Runner` once `Admin()` is the documented
   path.
4. **Do not add new no-context methods.**

## Anti-patterns rejected by this ADR

| Anti-pattern | Why it is wrong |
| ------------ | --------------- |
| New `ExecuteCommand` call sites in examples or packs | Reopens the untyped result hole |
| Restoring no-context helpers "for convenience" | Hides `context` and store errors |

## Impact

v0.15 is a source break for callers of the deleted no-context methods.
`ExecuteCommand` still compiles with a deprecation comment.

## References

- ADR-009 public-any contract
- ADR-017 Durable Runner boundary
- `runner.go`, `run.go`, `task.go`, `governance.go`, `response.go`,
  `facade.go`
