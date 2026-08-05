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

### Component 2 — `TaskBudget` + `AdmissionReservation` (enforcement)

Per-task limits and aggregate deployment limits have different consistency
requirements and therefore use different mechanisms.

`api.TaskBudget` bounds one task execution. `agent.Engine` enforces its token,
tool-call, wall-clock, and step limits before the next operation and returns a
typed budget-exhausted failure. Runner persists the task and result; it does not
reinterpret the limit.

`api.AdmissionReservation` protects aggregate limits across runs:

- concurrent runs,
- runs started in a time window,
- credits committed or reserved in a time window, and
- a trailing failure circuit breaker.

Admission is a compare-and-reserve operation, not a read-only policy query. A
conforming store checks the aggregate state and creates the reservation in one
atomic transaction. `PreviewAdmission` is read-only and is intended for status
surfaces; only `ReserveAdmission` grants capacity.

The application supplies governance values. The framework owns reservation
lifecycle and consistency. A configuration that enables aggregate governance
MUST use a provider that advertises admission-reservation support; production
startup fails rather than falling back to an in-process mutex.

`PolicyEngine` remains the authorization surface for dispatch, provider, tool,
action, handoff, and publication decisions. It may deny an operation for a
quota-related reason, but it cannot grant aggregate capacity. Capacity is
granted only by a successful reservation.

### Component 3 — typed obligation enforcement (conditional authorization)

`PolicyDecision.Obligations` is executable policy output. Each governed target
uses a typed enforcement method rather than passing an untyped value through a
generic mutator. The built-in target families are:

- Blackboard selections and items,
- tool result payloads,
- handoff context,
- trace exports, and
- user-visible messages.

Built-in obligation kinds are:

- `redact_fields`,
- `selector_only`,
- `require_human_approval`,
- `hide_internal_trace`,
- `mask_tool_output`, and
- `restrict_handoff_context`.

Every known obligation applicable to a target MUST be applied before the value
crosses its boundary. An obligation that is unknown or cannot be applied fails
closed and records `EventPolicyObligationFailed`. Successful enforcement records
`EventPolicyObligationApplied`. The framework never records an obligation and
then returns the original ungoverned value.

## Composition law

```
Measurement   → granular UsageRecord (append-only Store layer)
                       ↓
Task limit    → agent.Engine enforces TaskBudget
                       ↓
Aggregate cap → atomic AdmissionReservation
                       ↓
Authorization → deterministic PolicyEngine chain
                       ↓
Conditional   → typed obligation enforcement
                       ↓
Observable    → durable usage, admission, and policy events
```

The law is: **measurement supplies durable facts; task budgets bound one loop;
admission atomically reserves aggregate capacity; policy authorizes operations;
obligations transform or block governed outputs**. None of these surfaces may
claim the consistency guarantee owned by another.

## Anti-patterns rejected by this ADR

| Anti-pattern | Why it is wrong |
| ------------ | --------------- |
| Reading `MaxTokens` directly in Runner and aborting outside `agent.Engine` | Gives two owners to the same task limit and makes replay ambiguous |
| Checking aggregate quota with `SumCredits` followed by `Allow` | Two workers can observe the same remaining capacity and both enter; use an atomic reservation |
| Storing budget remaining as a field on `Run` | Stateful budget bookkeeping in Run conflicts with replay; usage and reservations are the sources of truth |
| Skipping `UsageRecord` for "internal" calls | Every measured unit is a record; zero credits represents a recorded but free unit |
| Treating `Credits` as currency in framework code | Framework records an `int64`; meaning and conversion remain adapter-defined |
| Passing obligation targets as `any` | It permits mismatched handlers and silent data leakage; enforcement is target-specific and typed |
| Ignoring an unknown or failed obligation | Authorization conditions were not satisfied, so the governed operation must fail closed |

## Impact

- Task execution, aggregate admission, operation authorization, and output
  transformation have distinct owners and durable facts.
- Admission remains correct with multiple processes when the StoreProvider
  satisfies the atomic reservation contract.
- Usage retries are safe only when records carry deterministic identities and
  append is idempotent.
- Applications keep control of limits and policy values; framework code owns
  the lifecycle and replay rules needed to enforce them consistently.

## References

- Spec: `docs/product-spec/v0.8.0/06-governance.md` (full type definitions, built-in obligation table, event names)
- Storage contract: ADR-012 (UsageStore is part of the Layer 1 contract)
- Related: ADR-005 (CapabilityInvoker — the older governance layer at execution time), ADR-008 (framework vs business)
