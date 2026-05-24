# 06 — Governance: Usage, Budget, Policy Obligation

## Goal

Make agent cost observable and bounded. Make policy obligations enforceable.

## Three concerns, three new components

| Concern | Component | Purpose |
|---------|-----------|---------|
| What did this run cost? | `UsageRecord` + `UsageStore` | Audit, billing, debugging |
| How much can this run cost? | `Budget` + `BudgetPolicy` | Pre-execution and in-flight cost gating |
| Did this policy decision come with conditions? | `PolicyEnforcer` | Apply obligations (redact, mask, require approval) |

## UsageRecord

`api/usage.go` (new):

```go
package api

import "time"

// UsageRecord captures the observable cost of a single billable unit of work.
// Every provider call, tool invocation, and external side-effect produces one
// UsageRecord. Records are append-only and stored via UsageStore.
type UsageRecord struct {
    ID          string    `json:"id"`
    RunID       string    `json:"runId"`
    TaskID      string    `json:"taskId,omitempty"`
    AgentID     string    `json:"agentId,omitempty"`

    Kind        UsageKind `json:"kind"`     // provider | tool | side_effect
    Provider    string    `json:"provider,omitempty"`
    Model       string    `json:"model,omitempty"`
    CapabilityName string `json:"capabilityName,omitempty"`

    InputTokens  int           `json:"inputTokens,omitempty"`
    OutputTokens int           `json:"outputTokens,omitempty"`
    ToolCalls    int           `json:"toolCalls,omitempty"`
    SideEffects  int           `json:"sideEffects,omitempty"`
    Retries      int           `json:"retries,omitempty"`
    Duration     time.Duration `json:"duration,omitempty"`

    // Credits is an abstract cost unit. The framework records but does not
    // assign meaning. Adapters/packs define currency or token-equivalent
    // conversion. int64 (no floats; intended as milli-credits or similar).
    Credits int64 `json:"credits,omitempty"`

    CreatedAt time.Time `json:"createdAt"`
}

type UsageKind string

const (
    UsageKindProvider   UsageKind = "provider"
    UsageKindTool       UsageKind = "tool"
    UsageKindSideEffect UsageKind = "side_effect"
)
```

### UsageStore

`api/store.go` addition:

```go
type UsageStore interface {
    SaveUsageRecord(context.Context, UsageRecord) error
    ListUsageRecords(context.Context, UsageSelector) ([]UsageRecord, error)
    SumUsageCredits(context.Context, UsageSelector) (int64, error)
}

type UsageSelector struct {
    RunID    string
    TaskID   string
    AgentID  string
    Kind     UsageKind
    Since    time.Time
    Until    time.Time
    Limit    int
}
```

### When UsageRecord is written

- After every provider stream completion → `Kind=provider`, tokens populated
- After every successful ToolInvocation → `Kind=tool`
- After every ActionAttempt that targets an external system → `Kind=side_effect`
- Failed attempts that retry: one record per attempt, `Retries` incremented on the final successful record OR a final failure record

### New events

- `EventUsageRecorded`
- `EventBudgetExceeded`
- `EventBudgetWarn` (at e.g. 80% threshold)

## Budget

`api/budget.go` (new):

```go
package api

import "time"

// Budget bounds the cost of a Run or AgentProfile. The runtime checks the
// budget before executing any Capability and after recording each UsageRecord.
type Budget struct {
    MaxCredits    int64         `json:"maxCredits,omitempty"`
    MaxTokens     int           `json:"maxTokens,omitempty"`
    MaxToolCalls  int           `json:"maxToolCalls,omitempty"`
    MaxDuration   time.Duration `json:"maxDuration,omitempty"`
    MaxIterations int           `json:"maxIterations,omitempty"`

    // OnExceeded controls behavior when any limit is reached.
    // - "abort": cancel the run with ErrBudgetExceeded
    // - "pause": pause the run and request approval to continue
    // - "warn":  emit EventBudgetWarn but do not block (use for soft limits)
    OnExceeded BudgetAction `json:"onExceeded,omitempty"`
}

type BudgetAction string

const (
    BudgetActionAbort BudgetAction = "abort"
    BudgetActionPause BudgetAction = "pause"
    BudgetActionWarn  BudgetAction = "warn"
)

// Quota is a budget bound to a subject (user, tenant, agent) for a time window.
type Quota struct {
    SubjectID string        `json:"subjectId"`
    Scope     string        `json:"scope"`  // e.g. "user:alice", "tenant:acme"
    Window    time.Duration `json:"window"` // rolling window
    Limit     Budget        `json:"limit"`
}
```

### BudgetPolicy

`api/policy.go` extension:

```go
// BudgetPolicy is an opt-in PolicyEngine implementation that enforces a Budget
// against accumulated UsageRecords. Compose it into the runtime via
// PolicyEngine chain (multiple PolicyEngines combine; first DENY wins).
type BudgetPolicy struct {
    Budget   Budget
    Store    UsageStore
    Scope    UsageSelector // determines which records to sum
}

func (b BudgetPolicy) Authorize(ctx context.Context, req PolicyRequest) (PolicyDecision, error)
```

Pseudo-implementation:

```go
func (b BudgetPolicy) Authorize(ctx context.Context, req PolicyRequest) (PolicyDecision, error) {
    spent, _ := b.Store.SumUsageCredits(ctx, b.Scope.ForRun(req.RunID))
    if spent >= b.Budget.MaxCredits {
        switch b.Budget.OnExceeded {
        case BudgetActionAbort: return PolicyDecision{Effect: PolicyEffectDeny, Reason: "budget exceeded"}, nil
        case BudgetActionPause: return PolicyDecision{Effect: PolicyEffectRequireApproval, Reason: "budget exceeded"}, nil
        case BudgetActionWarn:  return PolicyDecision{Effect: PolicyEffectAllow, Obligations: []PolicyObligation{{Kind: "warn_budget"}}}, nil
        }
    }
    return PolicyDecision{Effect: PolicyEffectAllow}, nil
}
```

### Composing with GovernancePolicy on AgentProfile

`AgentProfile.Governance.MaxCreditsPerRun` is materialized into a `BudgetPolicy` automatically when `RunFromProfile` constructs the run-scoped policy engine. The user does not need to wire it manually.

## PolicyEnforcer

`api/policy.go` extension:

```go
// PolicyEnforcer applies the obligations attached to a PolicyDecision to
// the data being authorized. Obligations transform data (e.g., redact fields,
// hide trace internals, mask tool output) before it leaves the policy boundary.
type PolicyEnforcer interface {
    Enforce(ctx context.Context, req PolicyRequest, decision PolicyDecision, target any) (any, error)
}
```

### Built-in obligations

`policy/obligations.go` (new):

| Obligation Kind | Behavior |
|----------------|----------|
| `redact_fields` | Remove listed JSON paths from target (target must be JSON-marshalable). Path list in `Obligation.Parameters["paths"]` (comma-separated). |
| `selector_only` | When target is a `BlackboardSelector` result, drop items not matching `Obligation.Parameters["selector"]`. |
| `require_human_approval` | Convert decision to `RequireApproval` if not already; framework already supports this effect. |
| `hide_internal_trace` | Strip TraceSpan internal fields (`InternalNotes`, `RawProviderRequest`) from target. |
| `mask_tool_output` | Replace ToolInvocationResult body with `[redacted]` while preserving metadata. |
| `restrict_handoff_context` | When target is handoff payload, filter Context fields to allowlist in `Obligation.Parameters["fields"]`. |

### Default implementation

`policy.NewDefaultEnforcer()` returns a `PolicyEnforcer` that dispatches by `Obligation.Kind` to the built-in implementations. Users compose custom enforcers by wrapping it.

### Where Enforcer is invoked

The Runner threads `PolicyEnforcer` into:

- Blackboard read path (after PolicyEngine authorize) — applies `redact_fields`, `selector_only`
- Tool invocation result path — applies `mask_tool_output`
- Trace export path — applies `hide_internal_trace`
- Handoff transfer path — applies `restrict_handoff_context`
- User message path — applies `redact_fields`

Each call site is bounded by the obligation kinds it knows; unknown kinds are passed through and logged.

### New events

- `EventPolicyObligationApplied`
- `EventPolicyObligationSkipped` (when target type cannot satisfy obligation)

## Configuration integration

`api/config.go` gains:

```go
type Config struct {
    // ... existing
    PolicyEnforcer PolicyEnforcer  // default: policy.NewDefaultEnforcer()
    UsageStore     UsageStore      // default: derived from StoreProvider
}
```

## Verification

- `TestUsageRecord_WrittenAfterProviderCall`
- `TestUsageRecord_WrittenAfterToolInvocation`
- `TestBudget_AbortBlocksExecutionWhenExceeded`
- `TestBudget_PauseTriggersApprovalRequest`
- `TestBudget_WarnEmitsEventOnly`
- `TestGovernance_MaxCreditsPerRun_MaterializedToBudgetPolicy`
- `TestEnforcer_RedactFields_RemovesPathsFromBlackboardResult`
- `TestEnforcer_MaskToolOutput_PreservesMetadata`
- `TestEnforcer_HideInternalTrace_StripsRawProviderRequest`
- `TestEnforcer_UnknownObligation_PassesThroughWithLog`
- `TestQuota_WindowedSubjectAccounting`
