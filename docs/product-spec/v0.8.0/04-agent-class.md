# 04 — AgentClass, AgentInstance, AgentProfile, Registry

> Anchor: ADR-014 (revised — AgentInstance accepted) + ADR-016 (Scheduler).
> Renamed from `03-agent-profile.md` in the v0.8.0 reconstruction.

## Goal

Define the three identity-bearing types in the v0.8.0 reconstruction
and their relationships:

- **AgentProfile** — declarative identity, persisted, registered once,
  referenced by ID (existing concept, lightly extended).
- **AgentClass** — Scheduler-facing definition: instructions, schemas,
  tools, model preference, LoopPolicy, capabilities. Lives in
  `multiagent/`.
- **AgentInstance** — execution-time instance of an AgentClass bound
  to a specific Run. A Run may have multiple instances of the same
  class.

Plus the **Registry** for AgentProfile + Capability lookup.

## Relationship between the three

```
AgentProfile  (api/)         declarative, persisted, addressed by ID
     │
     │  consumed by
     ▼
AgentClass    (multiagent/)  Scheduler-facing, runtime configuration
     │
     │  instantiated by Scheduler per Run
     ▼
AgentInstance (multiagent/)  one execution context, Run-scoped, deterministic ID
```

A Pack typically:

1. Registers AgentProfiles into the Registry (one per Class.Name).
2. Defines AgentClasses that reference Profiles by ID.
3. Lets Scheduler spawn AgentInstances as the team progresses.

## AgentProfile (existing — extension)

`api/types.go`:

```go
// AgentProfile is the declarative identity of an Agent: ID, role,
// instructions, model preference, capability authorization, triggers,
// and per-agent governance limits. Registered once, referenced by ID
// throughout the runtime.
type AgentProfile struct {
    // --- Identity ---
    ID       string            `json:"id"`
    Role     string            `json:"role,omitempty"`
    Groups   []string          `json:"groups,omitempty"`
    Metadata map[string]string `json:"metadata,omitempty"`

    // --- Versioning ---
    Version           string `json:"version,omitempty"`
    PreviousVersionID string `json:"previousVersionId,omitempty"`

    // --- Lifecycle ---
    Status AgentStatus `json:"status,omitempty"`

    // --- Behavior ---
    Instructions string      `json:"instructions,omitempty"`
    Model        ModelPolicy `json:"model,omitempty"`

    // --- Authorization ---
    Capabilities []string `json:"capabilities,omitempty"`

    // --- Triggering ---
    Triggers []Trigger `json:"triggers,omitempty"`

    // --- Per-agent governance ---
    Governance GovernancePolicy `json:"governance,omitempty"`
}
```

`AgentStatus`, `ModelPolicy`, `ModelFallback`, `Trigger`, `TriggerType`,
`GovernancePolicy` — unchanged from the prior 03-agent-profile.md spec.
The runtime rejects `RunFromProfile` on non-Active profiles.

## AgentClass (new — `multiagent/class.go`)

```go
package multiagent

import (
    "encoding/json"

    "github.com/Viking602/go-hydaelyn/agent"
    "github.com/Viking602/go-hydaelyn/api"
)

// AgentClass is the Scheduler-facing definition of an agent role. An
// AgentClass is reusable across runs; multiple AgentInstances per run
// share the same Class.
type AgentClass struct {
    // Name identifies the class. Convention: lower_snake or hyphen-case
    // (e.g. "forensics", "containment"). When wired to an AgentProfile,
    // AgentClass.Name == AgentProfile.ID.
    Name string

    // Description is human-readable.
    Description string

    // Instructions is the system prompt for instances of this class.
    Instructions string

    // Model overrides AgentProfile.Model when set.
    Model ModelRef

    // Tools lists tools instances of this class may invoke. References
    // are resolved against the runtime's tool registry.
    Tools []ToolRef

    // InputSchema is the JSON Schema for the Task.Input this class
    // accepts. Scheduler validates Dispatch.Input against this.
    InputSchema json.RawMessage

    // OutputSchema is the JSON Schema for the Result.Structured this
    // class produces. Engine validates / repairs against it via
    // OutputPolicy.
    OutputSchema json.RawMessage

    // LoopPolicy controls Engine's loop behavior for instances of this
    // class (max steps, step timeout, allow parallel tools).
    LoopPolicy agent.LoopPolicy

    // Capabilities lists api.Capability values this class is allowed
    // to invoke. AND-combined with AgentProfile.Capabilities at
    // resolution time.
    Capabilities []api.Capability
}

// ModelRef and ToolRef are lightweight references resolved by the
// Runner against its configured providers and tool registry.
type ModelRef struct {
    Provider string
    Model    string
}

type ToolRef struct {
    Name string
}
```

## AgentInstance (new — `multiagent/instance.go`)

```go
package multiagent

import (
    "github.com/Viking602/go-hydaelyn/api"
)

// AgentInstance is a Run-scoped execution context for an AgentClass.
// A Scheduler may spawn multiple instances of the same class within
// one Run (e.g. parallel branches of investigation).
type AgentInstance struct {
    // ID is deterministic from (RunID, Class.Name, SpawnSequence) so
    // replay reconstructs the same instance IDs.
    ID api.AgentID

    // Class is the AgentClass this instance was spawned from.
    Class AgentClass

    // RunID anchors the instance to the Run that spawned it.
    RunID api.RunID

    // SpawnSequence is the per-Class-per-Run monotonic spawn counter
    // (starts at 0). Together with RunID and Class.Name it determines
    // the instance ID deterministically.
    SpawnSequence int

    // State is the instance's per-execution mutable state. Persisted
    // via AgentInstanceStore so kill-resume reconstructs it.
    State AgentState
}

type AgentState struct {
    Status      InstanceStatus
    LastStepIdx int
    Failure     *agent.AgentFailure
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

type InstanceStatus string

const (
    InstanceIdle      InstanceStatus = "idle"
    InstanceRunning   InstanceStatus = "running"
    InstanceCompleted InstanceStatus = "completed"
    InstanceFailed    InstanceStatus = "failed"
)
```

ID derivation (deterministic, replay-friendly):

```go
// ComputeInstanceID returns the canonical AgentInstance.ID for a given
// (RunID, ClassName, SpawnSequence) triple. Same triple always yields
// the same ID. Implementations MUST use this helper rather than
// generating UUIDs.
func ComputeInstanceID(runID api.RunID, className string, spawnSeq int) api.AgentID
```

## Registry (existing — unchanged from prior spec)

The Registry interface, AgentSelector, CapabilitySelector,
in-process implementation, and store interfaces (AgentProfileStore,
CapabilityStore) are carried over from the prior 03-agent-profile.md
without change. The relevant additions to `UnitOfWork` are noted in
07-storage.md.

## Profile-driven run entry (existing)

```go
// Runner.RunFromProfile starts a Run using an AgentProfile.ID.
func (r *Runner) RunFromProfile(ctx context.Context, profileID string, input api.RunInput) (api.Run, error)
```

For multi-agent runs (Team-based), Packs use the Team entry point
instead:

```go
// multiagent.(*Team).Start runs the Team's Scheduler until terminal.
func (t *Team) Start(ctx context.Context, input json.RawMessage) (api.RunID, error)
```

Internally `Team.Start` resolves each AgentClass against the Registry
(`AgentProfile.ID == AgentClass.Name`), validates the team's
configuration, and calls into Runner / Scheduler.

## RunSelector + Run attribution (existing)

`RunSelector{AgentID, AgentVersion, ...}` and `Run.AgentVersion`
unchanged. For multi-agent runs, `Run` carries the *Team's owner
profile* (typically the supervisor / intake class). Per-instance
attribution lives in `AgentInstanceStore`, queryable by RunID.

## Migration

- Existing `AgentProfile` users: no change. AgentClass is additive.
- Existing `runner.RegisterAgent(profile)`: still works.
- Code reading `AgentInstance` from old drafts (none existed in
  shipped code) — see 14-migration-guide.md.

## Hard rules

1. AgentClass and AgentInstance live ONLY in `multiagent/`. Engine
   (`agent/`) never imports them.
2. AgentInstance.ID is deterministic via `ComputeInstanceID`. Random
   ID generation is rejected.
3. AgentProfile remains the durable, registered identity. AgentClass
   is the runtime-facing configuration. Two-name, one-thing pattern
   is deliberate; documentation must explain the role of each.

## Verification

- `TestAgentClass_InputSchemaValidatesDispatchInput` — Scheduler rejects Dispatch with input violating Class.InputSchema
- `TestAgentInstance_DeterministicID` — same (RunID, ClassName, SpawnSeq) → same ID across processes
- `TestAgentInstance_MultipleInstancesPerClassPerRun` — two ForensicsAgent instances per run, both addressable
- `TestAgentInstance_StateSurvivesKillResume` — kill mid-instance, resume reconstructs InstanceStatus + LastStepIdx
- `TestAgentProfile_JSONRoundTrip` — including Status, PreviousVersionID
- `TestRegistry_RegisterAndList` — by Role / Group / Capability
- `TestRunSelector_FiltersByAgentVersion` — same as prior spec
- `TestTeam_StartResolvesClassToProfile` — AgentClass.Name → AgentProfile.ID via Registry
