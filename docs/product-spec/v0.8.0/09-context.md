# 09 — Context Layer (Four-Tier Model)

> Renumbered from `07-context.md`. Substance preserved. The clarification
> in this revision is that **in-loop ContextManager** (the runtime
> selector/compactor used by `agent.Engine`) is an `agent/` concept
> (`03-agent-loop.md`), distinct from the **kernel context primitives**
> below (Blackboard, Memory, Artifact, ContextSource) which model the
> application-owned context surfaces a Scheduler and Pack code build on.

## Goal

Replace ad-hoc context shoving with four clearly-bounded primitives. Each answers a different question, and each has a different lifecycle, audit path, and storage backend.

## The four primitives

| Primitive | Question it answers | Lifetime | Owner |
|-----------|---------------------|----------|-------|
| **Blackboard** | What does the *current run* see right now? | One Run | Kernel (existing) |
| **Memory** | What do we remember *across runs*? | Beyond run | Application (interface only in kernel) |
| **Artifact** | What did we *produce* that someone might consume later? | Persistent, addressable | Kernel + application storage |
| **ContextSource** | What *external* sources can we read context from? | On-demand | Application |

### In-loop vs application surfaces

- `agent.ContextManager` (in `agent/`, `03-agent-loop.md`) is the
  runtime selector that lives inside the Agent Loop. It chooses what
  to put into the next prompt, compacts history, applies token budget.
  It reads from any of the four primitives below but does not own them.
- The four primitives here are application-facing. Packs and the
  multi-agent layer build on them; the Agent Loop consumes them via
  the in-loop manager.

## Primitive 1 — Blackboard

Existing kernel concept; multi-agent extension is in
`05-multi-agent-layer.md`. The contract here applies to
single-agent runs.

```go
type BlackboardEntry struct {
    Key       string
    Value     json.RawMessage
    WrittenBy AgentInstanceID
    StepID    string
    EvidenceID string
    CreatedAt time.Time
}

type BlackboardStore interface {
    Put(ctx context.Context, runID RunID, entry BlackboardEntry) error
    Get(ctx context.Context, runID RunID, key string) (BlackboardEntry, error)
    Query(ctx context.Context, runID RunID, sel BlackboardSelector) ([]BlackboardEntry, error)
}

type BlackboardSelector struct {
    KeyPrefix    *string
    WrittenBy    *AgentInstanceID
    ItemTypes    []BlackboardItemType
    SinceStepID  *string
}
```

**Required metadata** (kernel-enforced):

- `WrittenBy` MUST be set; anonymous writes rejected
- `StepID` MUST be set when writing from within an Agent Loop step
- `EvidenceID` is optional but recommended for evidence-carrying entries

The Blackboard is run-scoped. Cross-run sharing goes through Memory or Artifact, not Blackboard.

## Primitive 2 — Memory (interface only — ADR-013)

```go
package memory

type Identified interface {
    ID() string
}

type Memory[T Identified] interface {
    Put(ctx context.Context, item T) error
    Get(ctx context.Context, id string) (T, error)
    Query(ctx context.Context, q Query) ([]T, error)
    Delete(ctx context.Context, id string) error
}

type Query struct {
    TextSearch     string
    Filter         map[string]any
    EmbeddingMatch *EmbeddingMatch
    Limit          int
    Offset         int
}
```

**ADR-013 stance** (`15-memory-optional-plugin.md`): the kernel
declares the verbs (`Put`/`Get`/`Query`/`Delete`); the application owns
the storage. The framework ships NO Memory backend. Vector DB, KV
store, file store, in-memory — all application's call.

**Binding contract**: an application binds a `memory.Memory[T]`
instance to an AgentID via `Registry.BindMemory`. The
`hydaelyn.self.memory.read` Capability (`02-capability.md`) becomes
active for that AgentID only when binding has happened. Unbound
agents that try to read self-memory get `ErrMemoryNotBound`.

## Primitive 3 — Artifact

Artifacts are *produced* outputs: a report, a chart, a generated
plan, a redacted log file. They differ from Memory in that:

- Artifacts are produced *by a Run* (have a producing RunID and TaskID).
- Artifacts are addressable by URI (the storage layer hands back a stable URI).
- Artifacts can be consumed by later Runs, exported externally, or attached to communications.

```go
package artifact

type Artifact struct {
    ID         ArtifactID
    URI        string             // e.g. s3://bucket/key, file://path
    MimeType   string
    ProducedBy AgentInstanceID
    RunID      RunID
    TaskID     TaskID
    Metadata   map[string]string
    CreatedAt  time.Time
}

type Store interface {
    Put(ctx context.Context, a Artifact, content io.Reader) (Artifact, error)
    Get(ctx context.Context, id ArtifactID) (io.ReadCloser, Artifact, error)
    Describe(ctx context.Context, id ArtifactID) (Artifact, error)
    List(ctx context.Context, sel Selector) ([]Artifact, error)
}
```

**Kernel responsibility**: define the `Artifact` type and `Store`
interface, ship one reference adapter (filesystem) for `_examples/`
and the demo. Production deployments wire S3 / GCS / Azure Blob /
local volume themselves.

## Primitive 4 — ContextSource

A `ContextSource` is a *pull-on-demand* read of external data into a
prompt or evidence bundle. Examples: a Confluence page, a Jira ticket,
a Slack thread, a database row, a file from a knowledge repository.

```go
type ContextSource interface {
    Name() string
    Fetch(ctx context.Context, ref ContextRef) (ContextDocument, error)
}

type ContextRef struct {
    Kind     string
    ID       string
    Selector map[string]string
}

type ContextDocument struct {
    Source    string
    Ref       ContextRef
    Content   string
    MimeType  string
    Metadata  map[string]string
    Evidence  Evidence
}
```

The kernel ships NO ContextSource adapters. Applications register
ContextSources via `Runner.RegisterContextSource(src)` and the
in-loop `agent.ContextManager` can pull from them.

## How the four primitives compose

```
                    ┌──────────────────────────────┐
                    │ agent.ContextManager (loop)  │
                    └──────────────────────────────┘
                          │      │      │       │
                          ↓      ↓      ↓       ↓
                  ┌───────────────────────────────────┐
                  │ Blackboard  Memory  Artifact      │
                  │  (run)       (xrun)  (addressable)│
                  │              ContextSource (pull) │
                  └───────────────────────────────────┘
```

The in-loop manager:

- Selects relevant Blackboard entries via `BlackboardSelector` (often
  filtered to `EvidenceIDs` referenced by an upstream Handoff)
- Optionally reads Memory if bound for the calling AgentID
- Resolves Artifact references included in `Task.Input`
- Pulls ContextSources for any `ContextRef` cited by the model or by
  the application

Each pull becomes a `UsageRecord` (Kind = `UsageKindOther` with
`Metadata.source = "context_source"`) for auditing.

## Multi-agent context discipline

`05-multi-agent-layer.md` requires every multi-agent Blackboard write
to carry `WrittenBy`, `StepID`, and `EvidenceID`. That discipline is
the same discipline named here for single-agent — the multi-agent
layer just enforces it more strictly because a noisy Blackboard
poisons every downstream Instance.

`agent.ContextManager` for multi-agent runs SHOULD filter Blackboard
reads via `BlackboardSelector.ItemTypes` and `EvidenceIDs` — never
"give me everything." Master spec §10 calls this out as the
single largest source of multi-agent regression.

## Verification

- `TestBlackboardEntry_RequiresWrittenBy`
- `TestBlackboardEntry_RequiresStepIDWhenInLoop`
- `TestBlackboardSelector_FiltersByItemType`
- `TestMemoryUnbound_ReadReturnsErrMemoryNotBound`
- `TestArtifact_PutGetRoundTripFilesystemAdapter`
- `TestContextSource_FetchProducesUsageRecord`
- `TestContextManager_SelectEvidence_RespectsTokenBudget`
- `TestContextManager_MultiagentRun_DefaultsToEvidenceFilter`
