# 08 — Governance: Usage, Budget, Policy Obligations

> Renumbered from `06-governance.md`. The major v0.8.0 change is that
> the per-Task budget unifies under `api.TaskBudget` (ADR-017) instead
> of forking into `agent.LoopBudget` + `runner.RunBudget` + per-tool-call
> budget. The Usage/Quota/PolicyEnforcer surfaces are unchanged.

## Goal

Make resource use, spending limits, and policy enforcement first-class. Three concerns, three surfaces:

| Concern | Surface | Owner |
|---------|---------|-------|
| What did we spend? | `UsageRecord` (with `Credits`), `UsageStore` | Runner records, Adapter defines cost meaning |
| What is the spend limit? | `api.TaskBudget` on `api.Task`, `api.Quota` (per-tenant/agent) | Set by application/recipe, enforced by Engine (Task) and PolicyEngine (Quota) |
| What MUST the system do after a decision? | `Policy.Obligations[]` | PolicyEnforcer dispatches, app handlers execute |

## Surface 1 — Usage

```go
package api

type UsageRecord struct {
    ID          UsageID
    AgentID     AgentID
    RunID       RunID
    TaskID      TaskID

    Kind        UsageKind
    ProviderRef string
    ModelRef    string
    ToolRef     string

    InputTokens  int64
    OutputTokens int64
    Bytes        int64
    Duration     time.Duration
    Credits      int64
    CreditsKind  string

    Metadata  map[string]string
    Timestamp time.Time
}

type UsageKind string

const (
    UsageKindLLMCall        UsageKind = "llm_call"
    UsageKindToolCall       UsageKind = "tool_call"
    UsageKindStorageRead    UsageKind = "storage_read"
    UsageKindStorageWrite   UsageKind = "storage_write"
    UsageKindNetworkEgress  UsageKind = "network_egress"
    UsageKindOther          UsageKind = "other"
)
```

`UsageStore`:

```go
type UsageStore interface {
    Append(ctx context.Context, r UsageRecord) error
    Sum(ctx context.Context, sel UsageSelector) (UsageSummary, error)
    Query(ctx context.Context, sel UsageSelector) ([]UsageRecord, error)
}

type UsageSelector struct {
    AgentID *AgentID
    RunID   *RunID
    TaskID  *TaskID
    Kind    *UsageKind
    Since   *time.Time
    Until   *time.Time
}

type UsageSummary struct {
    Count         int64
    InputTokens   int64
    OutputTokens  int64
    Bytes         int64
    TotalDuration time.Duration
    Credits       int64
    CreditsByKind map[string]int64
}
```

### Credits semantics (locked default)

`Credits` is an **abstract integer cost unit**. The framework records `Credits` for every UsageRecord but does NOT define the conversion rate from `(tokens, bytes, duration)` to `Credits`. Adapters define meaning:

- A Postgres adapter for "1000 input tokens = 1 credit" implements that rule when constructing `UsageRecord`.
- A different deployment may define "$0.0001 = 1 credit."
- For LLM-only deployments, `Credits = InputTokens + OutputTokens` is fine.

`CreditsKind` is a free-form tag so a deployment can split credits by category (`"llm"`, `"storage"`, `"egress"`) for reporting purposes. The framework SUMs them in `CreditsByKind`.

## Surface 2 — TaskBudget (unified per ADR-017)

```go
package api

type TaskBudget struct {
    MaxTokens    int64         `json:"maxTokens,omitempty"`
    MaxWallClock time.Duration `json:"maxWallClock,omitempty"`
    MaxToolCalls int           `json:"maxToolCalls,omitempty"`
    MaxSteps     int           `json:"maxSteps,omitempty"`
}
```

`*TaskBudget` is carried on `api.Task` (`01-public-api.md` Change 4).

**Enforcement boundary**: per-Task budget is enforced by
`agent.Engine`, NOT by `runner.Runner`. The Engine decrements
`Step.BudgetUsed`, and on exhaustion produces
`Result.Failure = &AgentFailure{Kind: FailureBudgetExhausted}`. The
Runner persists the `UsageRecord` and the failing `Result`.

**Aggregation boundary**: `multiagent.Scheduler` MAY observe summed
TaskBudget consumption across all Instances in a Team for team-level
observability or to cut off a runaway Team. But team-level budget
enforcement is the Scheduler's call, not the framework's — there is
no built-in `TeamBudget`. (Schedulers can implement it by summing
`UsageStore.Sum` per-Instance.)

This collapses the previous three-fork
(`agent.LoopBudget`+`runner.RunBudget`+per-tool-call-budget) into one
type with one enforcement owner.

## Surface 2.5 — Quota

`Quota` is a per-tenant or per-agent limit independent of any one Task.

```go
type Quota struct {
    Name     string
    Scope    QuotaScope
    Period   time.Duration
    Limits   QuotaLimits
}

type QuotaScope struct {
    AgentID *AgentID
    RunID   *RunID
    Tenant  string
}

type QuotaLimits struct {
    MaxRuns       int
    MaxTokens     int64
    MaxCredits    int64
    MaxToolCalls  int
}
```

Quota is checked by `PolicyEngine` at the boundary points where it makes sense (Run admission, Tool invocation, Provider call). When exceeded, PolicyEngine emits a `PolicyDecision{Allow: false, Reason: "quota_exceeded"}`.

## Surface 3 — Policy Obligations

```go
type PolicyDecision struct {
    Allow       bool
    Reason      string
    Obligations []Obligation
}

type Obligation struct {
    Kind     ObligationKind
    Params   map[string]any
}

type ObligationKind string

const (
    ObligationRedact       ObligationKind = "redact"
    ObligationLog          ObligationKind = "log"
    ObligationRateLimit    ObligationKind = "rate_limit"
    ObligationRequireApproval ObligationKind = "require_approval"
    ObligationApplyContextFilter ObligationKind = "apply_context_filter"
    ObligationApplyOutputFilter  ObligationKind = "apply_output_filter"
)
```

`PolicyEnforcer`:

```go
type PolicyEnforcer interface {
    Enforce(ctx context.Context, decision PolicyDecision, target any) (any, error)
    RegisterHandler(kind ObligationKind, handler ObligationHandler)
}

type ObligationHandler func(ctx context.Context, ob Obligation, target any) (any, error)
```

**6 built-in obligation kinds, 0 built-in handlers.** Applications register their own handlers. The framework defines the vocabulary; the application defines the behavior. ([13-rollout-plan.md](13-rollout-plan.md) Phase 4 covers the handler registration scaffolding for `_examples/`.)

## Multi-agent governance

Multi-agent extends governance in three small, additive ways:

1. **UsageRecord carries `AgentID` already** — for multi-agent runs,
   AgentID is the AgentInstanceID. UsageStore queries by
   AgentID let a Scheduler ask "how much has forensicsInstanceA spent?"
2. **PolicyEngine decisions can target Dispatches** — e.g. a policy
   that refuses to dispatch to a particular AgentClass on
   tenant-restricted runs. Scheduler MUST consult PolicyEngine before
   emitting a Dispatch.
3. **Quota scope MAY narrow to AgentInstanceID** — Quota.Scope.AgentID
   already supports this; an instance running over budget can be
   rejected on next tool call.

No new types or interfaces. The existing surfaces work.

## Verification

- `TestUsageRecord_JSONRoundTrip`
- `TestUsageStore_SumByAgent`
- `TestUsageStore_CreditsByKindBreakdown`
- `TestTaskBudget_EngineEnforcesAndProducesTypedFailure` —
  `FailureBudgetExhausted` lands on `Result.Failure`
- `TestTaskBudget_PersistedOnTaskAcrossRunnerBoundary`
- `TestQuota_BlocksAtPolicyEngine`
- `TestObligation_Dispatch_RegisteredHandlersInvoked`
- `TestObligation_Dispatch_UnregisteredHandlerErrorsLoudly`
- `TestPolicyEngine_RejectsDispatchToForbiddenClass` (multi-agent)
- `TestUsageStore_SumByAgentInstanceID` (multi-agent)
