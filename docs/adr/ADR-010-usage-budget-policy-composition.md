# ADR-010 Usage, Budget, and Policy Composition — measurement is not enforcement, enforcement is policy

## Status

Accepted — enforced from v0.8.0 onward. Anchor document: `docs/product-spec/v0.8.0/06-governance.md`.

## Context

v0.7.x had no first-class accounting layer. Agents could spend unbounded tokens, make unbounded tool calls, and run unbounded time, with no kernel-level way to:

- audit the cost of a run after the fact,
- abort a run once it exceeded a budget,
- pause a run for approval at a budget threshold,
- emit a warning event at 80% of budget,
- attach windowed quotas to a user / tenant / agent,
- enforce a policy decision that says "allow, but with conditions" (e.g., redact fields, mask tool output).

Three concerns were being conflated:

1. **Measurement** — what did this run cost? (audit, billing, debugging)
2. **Enforcement** — was this run allowed to cost that much? (gating)
3. **Conditional authorization** — when policy says "yes, but only if X is applied", who applies X?

Folding all three into a single `MaxTokens` field on Run would have been the v0.7-style shortcut. Three separate components are needed.

## Decision

Three components, three responsibilities, **one composition law**.

### Component 1 — `UsageRecord` + `UsageStore` (measurement)

Append-only records, one per billable unit of work:

- After every provider stream completion → `Kind=provider`, tokens populated.
- After every successful `ToolInvocation` → `Kind=tool`.
- After every `ActionAttempt` targeting an external system → `Kind=side_effect`.
- Failed-then-retried attempts: one record per attempt; the final record carries the `Retries` count.

`UsageRecord.Credits` is **an abstract `int64` cost unit**. The framework records it and never assigns meaning. Adapters and packs define conversion to currency or token-equivalent. No floating-point. Intended interpretation is "milli-credits" or whatever the adapter declares — locked in design default #1 (see `00-overview.md`).

`UsageStore` is part of `api.StoreProvider` (Layer 1 contract from ADR-012) with `SaveUsageRecord`, `ListUsageRecords(UsageSelector)`, and `SumUsageCredits(UsageSelector)`.

### Component 2 — `Budget` + `BudgetPolicy` (enforcement)

A `Budget` declares thresholds (`MaxCredits`, `MaxTokens`, `MaxToolCalls`, `MaxDuration`, `MaxIterations`) and an `OnExceeded` action:

- `BudgetActionAbort` — cancel the run with `ErrBudgetExceeded`.
- `BudgetActionPause` — pause and request approval to continue.
- `BudgetActionWarn` — emit `EventBudgetWarn`, allow execution.

A `Quota` binds a Budget to a `SubjectID` (`user:alice`, `tenant:acme`) over a rolling time `Window`.

**`BudgetPolicy` is a `PolicyEngine`.** That is the composition law: budget enforcement is policy enforcement; it does not get its own enforcement code path. `BudgetPolicy.Authorize` sums recent `UsageRecord` credits via `UsageStore` and returns:

- `PolicyEffectAllow` if under threshold,
- `PolicyEffectDeny` (`OnExceeded=Abort`),
- `PolicyEffectRequireApproval` (`OnExceeded=Pause`),
- `PolicyEffectAllow` with a `warn_budget` obligation (`OnExceeded=Warn`).

PolicyEngines compose by chain; **first DENY wins**. A run is authorized when every chained engine allows.

`AgentProfile.Governance.MaxCreditsPerRun` is materialized into a run-scoped `BudgetPolicy` automatically by `Runner.RunFromProfile`. Users do not wire it manually.

### Component 3 — `PolicyEnforcer` (conditional authorization)

`PolicyDecision.Obligations []PolicyObligation` existed in v0.7 as a slice no one read. v0.8.0 introduces `PolicyEnforcer` to apply obligations to the target data:

```go
type PolicyEnforcer interface {
    Enforce(ctx context.Context, req PolicyRequest, decision PolicyDecision, target any) (any, error)
}
```

Built-in obligation kinds:

- `redact_fields` (drop listed JSON paths from target)
- `selector_only` (restrict BlackboardSelector results)
- `require_human_approval` (escalate to approval flow)
- `hide_internal_trace` (strip `InternalNotes`, `RawProviderRequest`)
- `mask_tool_output` (replace ToolInvocationResult body, preserve metadata)
- `restrict_handoff_context` (filter handoff payload fields by allowlist)

The Runner threads `PolicyEnforcer` into five call sites (Blackboard read, Tool invocation result, Trace export, Handoff transfer, User message). Each site is bounded by the obligation kinds it understands; unknown kinds pass through with `EventPolicyObligationSkipped` logged — not a hard error, because obligations are an open extension surface.

## Composition law

```
Measurement  →  UsageRecord (Store layer)
                     ↓
Enforcement  →  BudgetPolicy implements PolicyEngine
                     ↓
Authorization →  PolicyEngine chain (first DENY wins)
                     ↓
Conditional  →  PolicyEnforcer applies Obligations to target data
                     ↓
Observable   →  Events: UsageRecorded / BudgetWarn / BudgetExceeded /
                        PolicyObligationApplied / PolicyObligationSkipped
```

The single law: **enforcement is policy; measurement feeds policy; conditional authorization is the obligations on a policy decision**. There is no parallel "budget enforcer" or "usage gate" outside `PolicyEngine`.

## Anti-patterns rejected by this ADR

| Anti-pattern | Why it is wrong |
| ------------ | --------------- |
| Reading `MaxTokens` directly in the Runner and aborting outside PolicyEngine | Bypasses the policy chain; if a tenant policy says "allow with warning", direct abort overrides it |
| Storing budget remaining as a field on `Run` | Stateful budget bookkeeping in Run conflicts with replay; the source of truth is the sum over `UsageRecord` |
| Skipping `UsageRecord` for "internal" tool calls | Every billable unit is a record; the runtime cannot decide what counts as "billable" — adapters do, via `Credits` value (zero credits = recorded but free) |
| Treating `Credits` as a currency in framework code | Framework records it as `int64`; meaning is adapter-defined; no FX, no rounding, no localization |
| A custom enforcer that mutates `target` in place when `PolicyEffectDeny` is returned | If access is denied the target should not leak through; enforcer is only invoked for `Allow`+obligations |
| Adding a new obligation kind without registering it in the default enforcer's dispatch | Unknown kinds pass through with `EventPolicyObligationSkipped` — acceptable as an open extension, but pack-defined obligations should declare their kind in pack code with a custom enforcer wrapper |

## Impact

- Agent runs are auditable (`UsageStore` query yields cost), bounded (`BudgetPolicy` in the chain), and conditionally authorized (`PolicyEnforcer` applies obligations).
- Adding a new cost dimension (e.g., GPU minutes) is a new `UsageRecord` field + adapter-defined `Credits` conversion — no Runner or PolicyEngine changes.
- Adding a new obligation kind is a new constant + a new branch in the default enforcer — no Runner changes.
- The shape of `UsageRecord` is the public surface external billing systems integrate against; downstream teams can build dashboards / pricing models on top of it without touching framework code.

## References

- Spec: `docs/product-spec/v0.8.0/06-governance.md` (full type definitions, built-in obligation table, event names)
- Storage contract: ADR-012 (UsageStore is part of the Layer 1 contract)
- Related: ADR-005 (CapabilityInvoker — the older governance layer at execution time), ADR-008 (framework vs business)
