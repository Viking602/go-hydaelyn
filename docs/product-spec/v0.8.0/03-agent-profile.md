# 03 — AgentProfile Extension and Registry

## Goal

Make `AgentProfile` a complete declarative definition of an Agent: identity, instructions, model preferences, capability set, triggers, and governance limits. Add a Registry that stores, lists, and resolves Profiles and Capabilities by selector.

## Decision recap

- We **extend** `api.AgentProfile` instead of introducing a new `AgentDefinition` type. Reasoning is recorded in ADR-001 (Profile = identity) and ratified during v0.8.0 design review.
- All new fields are `omitempty` and additive.
- `AgentInstance` (proposed in ADR-001 but never implemented) remains deferred. If a multi-instance-per-profile need surfaces, it is layered in v0.9.0 or later without breaking v0.8.0 AgentProfile.

## Final AgentProfile shape

`api/types.go`:

```go
// AgentProfile is the declarative description of an Agent: identity,
// behavioral guidance, model preference, capability authorization, trigger
// declarations, and per-agent governance limits. AgentProfile is registered
// once and referenced by ID throughout the runtime.
type AgentProfile struct {
    // --- Identity (v0.7 fields, unchanged) ---
    ID       string            `json:"id"`
    Role     string            `json:"role,omitempty"`
    Groups   []string          `json:"groups,omitempty"`
    Metadata map[string]string `json:"metadata,omitempty"`

    // --- Versioning ---
    // Version is the profile schema version. Format unspecified.
    Version string `json:"version,omitempty"`

    // PreviousVersionID points to the AgentProfile this one supersedes.
    // Empty for the first version. Forms a unidirectional lineage chain so
    // that run history written against an older AgentID/AgentVersion remains
    // attributable to the same logical Agent after upgrades. The framework
    // does NOT enforce that the referenced profile exists; it is metadata.
    PreviousVersionID string `json:"previousVersionId,omitempty"`

    // --- Lifecycle ---
    // Status declares whether the Agent is eligible to be invoked. Empty is
    // treated as AgentStatusActive for backward compatibility with v0.7
    // profiles. Registry.RunFromProfile MUST reject statuses other than
    // AgentStatusActive; trigger transports MUST skip non-active profiles.
    Status AgentStatus `json:"status,omitempty"`

    // --- Behavior ---
    // Instructions is the system prompt or persistent guidance for the agent.
    // Free-form text. Templating, if needed, is the caller's responsibility.
    Instructions string `json:"instructions,omitempty"`

    // Model declares the preferred model and parameters. Provider drivers
    // honor this; pack/recipe code MAY override per-call.
    Model ModelPolicy `json:"model,omitempty"`

    // --- Authorization surface ---
    // Capabilities lists Capability.Name values the agent is permitted to
    // invoke. Empty means "no restriction beyond PolicyEngine"; non-empty
    // is an explicit allowlist. Governance.AllowedCapabilities and
    // Governance.DeniedCapabilities further refine this.
    Capabilities []string `json:"capabilities,omitempty"`

    // --- Triggering ---
    // Triggers declare when the agent should be spawned. Execution lives in
    // transport/scheduler, transport/webhook, transport/event. The field
    // here is data-only; runtime kernel does NOT execute triggers.
    Triggers []Trigger `json:"triggers,omitempty"`

    // --- Per-agent governance ---
    Governance GovernancePolicy `json:"governance,omitempty"`
}
```

## AgentStatus

```go
// AgentStatus is the lifecycle state of an AgentProfile. It governs whether
// the runtime treats the profile as eligible for new runs and triggers.
type AgentStatus string

const (
    // AgentStatusActive: the Agent accepts new runs and trigger dispatches.
    // Empty string is treated as Active for backward compatibility.
    AgentStatusActive AgentStatus = ""

    // AgentStatusDraft: the profile exists but is not yet eligible to run.
    // Useful for staging a new version before promotion.
    AgentStatusDraft AgentStatus = "draft"

    // AgentStatusPaused: the Agent temporarily refuses new runs but its
    // history is preserved and the profile may be reactivated. Existing
    // in-flight runs continue.
    AgentStatusPaused AgentStatus = "paused"

    // AgentStatusRetired: the Agent is permanently inactive. The profile is
    // kept for historical attribution (RunSelector still resolves against
    // retired profiles) but no new runs or triggers fire.
    AgentStatusRetired AgentStatus = "retired"
)
```

The runtime treats `Active` as the only invocation-eligible state. `Draft` and `Paused` are explicit refusals; `Retired` additionally signals to UI/CLI that the profile should be hidden from selection by default.

## ModelPolicy

```go
// ModelPolicy is the declarative model preference for an Agent.
type ModelPolicy struct {
    Provider    string  `json:"provider,omitempty"`
    Model       string  `json:"model,omitempty"`
    Temperature float64 `json:"temperature,omitempty"`
    MaxTokens   int     `json:"maxTokens,omitempty"`

    // Fallbacks lists alternative provider/model pairs to try if the primary
    // fails. Order matters.
    Fallbacks []ModelFallback `json:"fallbacks,omitempty"`
}

type ModelFallback struct {
    Provider string `json:"provider"`
    Model    string `json:"model"`
}
```

## Trigger

```go
// Trigger declares a condition under which the Runner should start a run for
// this AgentProfile. Trigger evaluation and dispatch live in transport
// adapters, not in the runtime kernel.
type Trigger struct {
    ID      string            `json:"id"`
    Type    TriggerType       `json:"type"`
    Source  string            `json:"source,omitempty"`
    Filter  map[string]string `json:"filter,omitempty"`
    Enabled bool              `json:"enabled,omitempty"`
}

type TriggerType string

const (
    TriggerManual   TriggerType = "manual"
    TriggerSchedule TriggerType = "schedule" // Source = cron expr; transport/scheduler
    TriggerWebhook  TriggerType = "webhook"  // Source = path/route; transport/webhook
    TriggerEvent    TriggerType = "event"    // Source = event type; transport/event
)
```

## GovernancePolicy

```go
// GovernancePolicy bounds an AgentProfile's runtime cost and capability use.
// The runtime enforces these in coordination with PolicyEngine.
type GovernancePolicy struct {
    MaxRunsPerDay       int           `json:"maxRunsPerDay,omitempty"`
    MaxConcurrentRuns   int           `json:"maxConcurrentRuns,omitempty"`
    MaxCreditsPerRun    int64         `json:"maxCreditsPerRun,omitempty"`
    MaxRuntime          time.Duration `json:"maxRuntime,omitempty"`
    AllowedCapabilities []string      `json:"allowedCapabilities,omitempty"`
    DeniedCapabilities  []string      `json:"deniedCapabilities,omitempty"`

    // ApprovalRequiredFor lists ToolEffectType values that always require
    // human approval before execution, regardless of PolicyEngine output.
    ApprovalRequiredFor []ToolEffectType `json:"approvalRequiredFor,omitempty"`
}
```

## Registry interface

New file: `api/registry.go`

```go
package api

import "context"

// Registry stores AgentProfiles and Capabilities and answers lookups by
// selector. It is the canonical agent / capability directory backing
// transport (CLI, MCP, HTTP) and the Runner's profile-driven entrypoints.
type Registry interface {
    // Agents
    RegisterAgent(ctx context.Context, profile AgentProfile) error
    GetAgent(ctx context.Context, id string) (AgentProfile, error)
    ListAgents(ctx context.Context, sel AgentSelector) ([]AgentProfile, error)
    UnregisterAgent(ctx context.Context, id string) error

    // Capabilities
    RegisterCapability(ctx context.Context, capability Capability) error
    GetCapability(ctx context.Context, name string) (Capability, error)
    ListCapabilities(ctx context.Context, sel CapabilitySelector) ([]Capability, error)
    UnregisterCapability(ctx context.Context, name string) error
}

// AgentSelector filters AgentProfiles. All set fields combine with AND.
type AgentSelector struct {
    IDs          []string
    Roles        []string
    Groups       []string
    Tags         []string
    Version      string
    Capabilities []string // require profile.Capabilities to include all of these
}

// CapabilitySelector filters Capabilities. All set fields combine with AND.
type CapabilitySelector struct {
    Names      []string
    Tags       []string
    EffectType ToolEffectType // zero value matches any
    Idempotent *bool
}
```

## Default Registry implementation

In-process implementation in `internal/registry/`, exposed via `Runner`:

```go
// runner.go (additions)
func (r *Runner) Registry() api.Registry { return r.rt.Registry() }
```

The in-process Registry stores into the active `UnitOfWork` via new store interfaces:

```go
// api/store.go additions
type AgentProfileStore interface {
    SaveAgentProfile(context.Context, AgentProfile) error
    LoadAgentProfile(context.Context, string) (AgentProfile, error)
    ListAgentProfiles(context.Context, AgentSelector) ([]AgentProfile, error)
    DeleteAgentProfile(context.Context, string) error
}

type CapabilityStore interface {
    SaveCapability(context.Context, Capability) error
    LoadCapability(context.Context, string) (Capability, error)
    ListCapabilities(context.Context, CapabilitySelector) ([]Capability, error)
    DeleteCapability(context.Context, string) error
}
```

Added to `UnitOfWork`:

```go
type UnitOfWork interface {
    // ... existing
    AgentProfiles() AgentProfileStore
    Capabilities()  CapabilityStore
}
```

## Run attribution: RunSelector

Two `AgentProfile` extensions above (`PreviousVersionID`, `Status`) only carry weight if run history can be queried by Agent identity. v0.8.0 adds `RunSelector` to make that lookup a stable public type.

`api/types.go` (addition):

```go
// RunSelector filters Run records. All set fields combine with AND. Empty
// selector matches all runs subject to store implementation limits.
type RunSelector struct {
    // IDs restricts the result to specific Run IDs.
    IDs []string `json:"ids,omitempty"`

    // AgentID restricts the result to runs started against this profile ID.
    // Combined with AgentVersion, this answers "what has this Agent (this
    // specific version) done."
    AgentID string `json:"agentId,omitempty"`

    // AgentVersion restricts the result to runs whose AgentProfile.Version
    // matches. Empty matches any version (i.e. all versions of AgentID).
    AgentVersion string `json:"agentVersion,omitempty"`

    // Statuses restricts by Run.Status (running, completed, failed, ...).
    Statuses []RunStatus `json:"statuses,omitempty"`

    Since time.Time `json:"since,omitempty"`
    Until time.Time `json:"until,omitempty"`
    Limit int       `json:"limit,omitempty"`
}
```

The corresponding store method lives on `RunStore` (see doc 05):

```go
// api/store.go (addition)
type RunStore interface {
    // ... existing methods
    ListRuns(ctx context.Context, sel RunSelector) ([]Run, error)
}
```

For the lookup to work, `Run` records MUST carry the AgentID and AgentVersion they ran under. Existing `Run` already stores `AgentID`; v0.8.0 adds:

```go
type Run struct {
    // ... existing fields
    AgentVersion string `json:"agentVersion,omitempty"`
}
```

The Runner populates `Run.AgentVersion` from `AgentProfile.Version` at run-start time. This is additive and `omitempty`; v0.7 stores read with empty `AgentVersion` continue to work.

## Profile-driven run entry

`Runner.RunFromProfile(ctx, profileID, input)` looks up profile, validates governance, resolves capabilities, calls `QueueRun`.

```go
// run.go (addition)
func (r *Runner) RunFromProfile(ctx context.Context, profileID string, input api.RunInput) (api.Run, error)
```

`api.RunInput` is a new struct wrapping the user-supplied request and optional context:

```go
type RunInput struct {
    Request  string            `json:"request,omitempty"`
    Context  map[string]string `json:"context,omitempty"`
    TraceID  string            `json:"traceId,omitempty"`
    ParentID string            `json:"parentId,omitempty"`
}
```

## Migration

- Existing code calling `runner.RegisterAgent(profile)` continues to work. The new Registry is layered above; `RegisterAgent` remains a convenience that delegates to `Registry().RegisterAgent`.
- Existing AgentProfile constructions need not set any new fields; all are `omitempty`.

## Verification

- `TestAgentProfile_JSONRoundTrip` including new fields (`Status`, `PreviousVersionID`)
- `TestRegistry_RegisterAndList` — register 5 agents, query by Role/Group/Capability
- `TestRegistry_CapabilityAllowlist` — profile with `Governance.AllowedCapabilities` denies invocations outside the set
- `TestRunFromProfile_LooksUpAndQueues` — register profile, RunFromProfile produces a Run with profile fields applied
- `TestRunFromProfile_RejectsNonActiveStatus` — Draft/Paused/Retired profiles fail RunFromProfile with a typed error
- `TestRunFromProfile_StampsAgentVersion` — started Run carries `AgentID` + `AgentVersion` from the profile
- `TestRunSelector_FiltersByAgentVersion` — register two versions of the same AgentID, start one run each, RunSelector with `AgentVersion` returns only the matching run
- `TestAgentProfile_PreviousVersionIDChain` — upgrading a profile by registering a new version with `PreviousVersionID` set to the previous; `Registry.ListAgents` returns both; lineage is walkable
- `TestModelPolicy_ProviderFallback` — primary provider error triggers fallback driver
