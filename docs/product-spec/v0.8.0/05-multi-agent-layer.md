# 05 — Multi-Agent Layer

> Anchor: ADR-016 Explicit Multi-Agent Scheduler. Master spec §4.

## Goal

Define the `multiagent/` package — a first-class kernel package
that owns multi-agent coordination: scheduling, dispatch, handoff,
team-level blackboard semantics, voting, supervisor patterns.

## Package layout

```
multiagent/
├── doc.go
├── class.go        // AgentClass            (see 04-agent-class.md)
├── instance.go     // AgentInstance         (see 04-agent-class.md)
├── team.go         // Team
├── scheduler.go    // Scheduler interface + Sequential, Router, Supervisor
├── dispatch.go     // Dispatch type
├── handoff.go      // Handoff, HandoffStore (see 07-storage.md)
├── blackboard.go   // multi-agent BlackboardEntry constraints
├── voting.go       // Voting helpers
├── supervisor.go   // Supervisor helpers
└── events.go       // multi-agent event types
```

## Team

```go
package multiagent

// Team is the unit of multi-agent execution. A Team binds AgentClasses,
// a Scheduler, and shared Blackboard / TeamState backed by Runner stores.
type Team struct {
    Name      string
    Classes   []AgentClass
    Scheduler Scheduler

    runner *hydaelyn.Runner // injected via NewTeam
}

// NewTeam constructs a Team. Pack authors typically wrap this in their
// own factory.
func NewTeam(name string, r *hydaelyn.Runner) *Team

// AddRole registers an AgentClass with this Team. Order of AddRole calls
// is significant for SequentialScheduler; ignored by Router/Supervisor.
func (t *Team) AddRole(class AgentClass) *Team

// UseScheduler sets the Team's Scheduler. Must be called before Start.
func (t *Team) UseScheduler(s Scheduler) *Team

// Start launches the Team Scheduler loop for a new Run. Returns the
// RunID; the Scheduler drives execution to completion (or to a paused
// state awaiting approval).
func (t *Team) Start(ctx context.Context, input json.RawMessage) (api.RunID, error)

// Resume picks up a Team that was paused (e.g. by an approval) using
// the previously-issued RunID. State is reconstructed from EventStore +
// TeamStateStore + AgentInstanceStore.
func (t *Team) Resume(ctx context.Context, runID api.RunID) error
```

## Scheduler

```go
package multiagent

// Scheduler decides which agents run next given current TeamState. It
// is stateless across ticks; all coordination state lives in
// TeamStateStore (per ADR-017 §3).
type Scheduler interface {
    // Next produces zero or more Dispatches given current TeamState.
    // Returning zero Dispatches indicates the Team has reached a
    // terminal state (success, failure, or awaiting approval).
    Next(ctx context.Context, state TeamState) ([]Dispatch, error)
}

// TeamState is the read-model the Scheduler consults each tick.
type TeamState struct {
    RunID         api.RunID
    Team          *Team
    Instances     []AgentInstance
    LatestResults map[api.AgentID]agent.Result
    Handoffs      []Handoff
    Blackboard    []BlackboardEntry
    Failures      []agent.AgentFailure
    PendingApprovals []api.ApprovalRequest
    Tick          int
}
```

### Reference implementations

```go
// SequentialScheduler advances through a fixed list of AgentClasses,
// dispatching the next one once the previous Result is valid. On
// AgentFailure with Retryable=true, it retries the same class; on
// Retryable=false it terminates the Team.
type SequentialScheduler struct {
    Order []string // AgentClass.Name in execution order
}

// RouterScheduler reads a discriminator field from the most recent
// TypedReport and routes to the AgentClass keyed by its value.
type RouterScheduler struct {
    // DiscriminatorField is a JSON path within Result.Structured
    // (e.g. "$.next" or "$.severity").
    DiscriminatorField string
    // Routes maps discriminator values to AgentClass.Names.
    Routes map[string]string
    // Default is the AgentClass.Name used when no route matches.
    Default string
}

// SupervisorScheduler designates one AgentClass as the supervisor.
// The supervisor's OutputSchema is { "dispatches": []DispatchDecision }
// and the Scheduler executes those decisions verbatim. Use when you
// need an LLM-driven router.
type SupervisorScheduler struct {
    SupervisorClass string
}
```

Advanced strategies (Debate / MapReduce / DAG / Swarm) are deferred to
v0.9.0; the `Scheduler` interface is intentionally narrow so they slot
in without breaking v0.8.0 callers.

## Dispatch

```go
package multiagent

// Dispatch is the Scheduler's instruction to start (or resume) one
// AgentInstance executing one Task. Runner persists the embedded Task
// via TaskStore.
type Dispatch struct {
    To          api.AgentID
    Task        api.Task          // includes InputSchema, OutputSchema, Budget
    Input       json.RawMessage   // becomes Task.Input
    Budget      api.TaskBudget    // becomes Task.Budget if non-zero
    Expectation OutputExpectation
}

// OutputExpectation lets the Scheduler express how the Result feeds
// back into TeamState selection.
type OutputExpectation struct {
    Required   bool   // if true, Result.Valid==false is a Team-level failure
    StoreAs    string // Blackboard key under which to store TypedReport
    Discriminator string // hint to Router: which JSON path to inspect
}
```

## Handoff (typed)

```go
package multiagent

// Handoff is a structured transfer of context between agents. Persisted
// via HandoffStore (07-storage.md). The framework rejects free-form
// prose handoffs at the kernel level; Reason may contain prose but
// Payload MUST conform to the receiving AgentClass.InputSchema.
type Handoff struct {
    From                 api.AgentID
    To                   api.AgentID
    Reason               string
    Payload              json.RawMessage
    EvidenceIDs          []string
    RequiredOutputSchema json.RawMessage
    CreatedAt            time.Time
}
```

Validation happens at Dispatch construction time: if `Handoff.Payload`
does not validate against the receiving class's `InputSchema`, the
Scheduler MUST return an error from `Next` rather than emitting the
Dispatch.

## Multi-agent Blackboard

```go
package multiagent

// BlackboardEntry is the multi-agent-flavored write to the underlying
// api.BlackboardReadWriter. Required metadata makes coordination
// auditable.
type BlackboardEntry struct {
    Key        string
    Value      json.RawMessage
    WrittenBy  api.AgentID
    StepID     string
    EvidenceID string
    CreatedAt  time.Time
}

// Write enforces the metadata constraint and adapts to the underlying
// BlackboardReadWriter store.
func Write(ctx context.Context, bb api.BlackboardReadWriter, runID api.RunID, e BlackboardEntry) error
```

WrittenBy, StepID are mandatory; EvidenceID is optional. Writes
missing required metadata are rejected with `ErrBlackboardMetadataMissing`.

## Voting

```go
package multiagent

// VotingResult tallies AgentInstance results around a question.
type VotingResult struct {
    Question string
    Tallies  map[string]int // option → count
    Voters   map[string]api.AgentID // option → who voted for it
    Winner   string
}

// MajorityVote aggregates a list of Results around a discriminator
// field and returns the majority option.
func MajorityVote(results []agent.Result, discriminatorPath string) (VotingResult, error)

// QuorumVote requires N out of M agreement.
func QuorumVote(results []agent.Result, discriminatorPath string, quorum int) (VotingResult, error)
```

## Supervisor helpers

```go
// SupervisorDecision is the typed payload a SupervisorScheduler reads
// from the supervisor agent's Result.Structured.
type SupervisorDecision struct {
    Dispatches []Dispatch       `json:"dispatches"`
    Terminate  bool             `json:"terminate"`
    Reason     string           `json:"reason,omitempty"`
}
```

## Multi-agent events

```go
package multiagent

const (
    EventDispatchEmitted   api.EventType = "multiagent.dispatch_emitted"
    EventHandoffPersisted  api.EventType = "multiagent.handoff_persisted"
    EventInstanceSpawned   api.EventType = "multiagent.instance_spawned"
    EventInstanceCompleted api.EventType = "multiagent.instance_completed"
    EventSchedulerTick     api.EventType = "multiagent.scheduler_tick"
    EventTeamTerminated    api.EventType = "multiagent.team_terminated"
    EventSchedulerFailure  api.EventType = "multiagent.scheduler_failure"
)
```

These join the existing `api.EventType` constants. The
`EventSchedulerTick` event lets external Schedulers subscribe rather
than poll.

## Scheduler ↔ Runner sequence

```
1.  Team.Start writes Run, persists initial Task via Runner.
2.  Scheduler.Next(state) → []Dispatch
3.  For each Dispatch:
      - Runner persists Dispatch.Task via TaskStore
      - HandoffStore appends (if Dispatch came from a Handoff)
      - Worker Runtime acquires Lease (LeaseStore.AcquireWithExpectedVersion)
      - agent.Engine.Run(Task, OutputPolicy{Schema: Task.OutputSchema})
      - Returns agent.Result
      - Worker writes TypedReport to Blackboard (via multiagent.Write)
      - Worker emits EventInstanceCompleted; releases lease
4.  Runner emits EventSchedulerTick
5.  Scheduler.Next reads updated TeamState; loop or terminate.
6.  On AgentFailure (Result.Failure != nil) the Scheduler decides:
    retry, escalate to Supervisor, request human approval (via
    Runner.RequestApproval), or terminate.
```

## Hard rules

1. `multiagent/**` imports `api/`, `agent/`, stdlib only.
2. Schedulers never side-effect external systems directly; all side
   effects route through agent tools.
3. Dispatches whose `Handoff.Payload` violates the receiving class's
   InputSchema MUST NOT be emitted.
4. Schedulers MUST be stateless across ticks; all state lives in
   TeamStateStore.
5. AgentInstance.ID is deterministic via `ComputeInstanceID`.

## Verification

- `TestSequentialScheduler_AdvancesInOrder` — N classes, N Dispatches in order
- `TestRouterScheduler_BranchesOnDiscriminator` — different Result.Structured → different routes
- `TestSupervisorScheduler_ExecutesSupervisorDispatches` — supervisor's Result.Structured drives next Dispatches
- `TestHandoff_RejectsPayloadViolatingInputSchema` — Scheduler.Next returns error
- `TestBlackboardEntry_RequiresMetadata` — WrittenBy/StepID enforced
- `TestVoting_MajorityVoteCountsCorrectly`
- `TestTeam_StartThenResume_StateReconstructed` — kill mid-run, Team.Resume reconstructs state
- `TestSchedulerTick_EmittedAfterPersistedResult` — EventSchedulerTick present in EventStore
- `TestSchedulerFailure_TerminalEvent` — Scheduler with no valid Next emits EventSchedulerFailure + terminates Run
- `TestMultiagent_NeverImportsRunner` — sentrux boundary check
