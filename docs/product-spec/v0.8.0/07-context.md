# 07 — Context Layer: Four-Tier Model

## Goal

Disambiguate the four distinct kinds of context an Agent reads/writes during a run. Give each a stable public interface so users do not conflate them.

## The four layers

| Layer | Scope | Lifetime | Typical content |
|-------|-------|----------|----------------|
| **Blackboard** | Run | Run | Claims, evidence, findings, task outputs, handoff snapshots |
| **Memory** | Agent / User / Tenant | Cross-run, long-term | Conversation history, learned facts, preferences |
| **Artifact** | Run / Project | Long-term | Binary or large structured output (PDF, image, JSON > 1MB) |
| **ContextSource** | Reference | N/A (pointer) | URL, vector index, file path, API endpoint |

Each layer has its own scope axis and lifetime. Mixing them — for example storing PDFs in Blackboard, or treating Memory as run-scoped — leads to performance and correctness problems.

## ContextScope

`api/context.go` (new):

```go
package api

// ContextScope is the lifetime/visibility axis for any context entry.
type ContextScope string

const (
    ContextScopeRun    ContextScope = "run"
    ContextScopeTask   ContextScope = "task"
    ContextScopeAgent  ContextScope = "agent"
    ContextScopeUser   ContextScope = "user"
    ContextScopeTenant ContextScope = "tenant"
)
```

## Blackboard (unchanged)

`api.BlackboardItem`, `api.BlackboardSelector` continue as defined. The 7 built-in `BlackboardItemType` constants (`claim`, `evidence`, `finding`, `artifact_ref`, `context`, `task_output`, `handoff_context`) remain.

**Documentation clarification** added to `api/types.go`:

```go
// BlackboardItemType classifies the kind of payload a blackboard entry
// carries. The framework ships seven recommended types but does NOT treat
// the set as closed — applications MAY define and use additional types
// (e.g. "diff", "compile_error", "metric") without runtime opposition.
type BlackboardItemType string
```

## Memory

`api/memory.go`:

```go
package api

import "context"

// Identified is the minimum identity contract any memory entity must satisfy.
// The framework uses Scope+SubjectID to enforce isolation and ID for selective
// deletion. Every other field on the entity belongs to the application schema.
type Identified interface {
    ID() string
    Scope() ContextScope
    SubjectID() string
}

// Memory is the optional plugin contract for application memory.
// T is the application-owned entity (ChatMessage, UserPreference,
// LearnedFact, vector chunk, knowledge-graph node, ...). Storage is entirely
// the application's responsibility — the framework ships no backend.
type Memory[T Identified] interface {
    Write(ctx context.Context, entry T) error
    Read(ctx context.Context, sel MemorySelector) ([]T, error)
    Forget(ctx context.Context, sel MemorySelector) (int, error)
}

// MemorySelector filters by identity only. Application fields (tags, content,
// time range, layer) are not part of the kernel contract.
type MemorySelector struct {
    Scope     ContextScope
    SubjectID string
    IDs       []string // optional: empty = no ID restriction
    Limit     int      // optional: 0 = unbounded
}
```

### No reference implementation

The framework ships no Memory backend — neither in-process nor durable. Applications either implement `api.Memory[T]` themselves against their existing database / ORM, or skip the interface entirely and use their own memory mechanism. The framework's runtime never requires a Memory to be configured.

The asymmetry with `StoreProvider` is intentional: `StoreProvider` stores framework-owned control-plane entities (Run, Task, Lease, Event) where reference implementations under `storage/{memory,sqlite,postgres,mysql}` legitimately accelerate adoption. Memory stores application-owned content (chat turns, preferences, learned facts, vectors, knowledge-graph nodes) where the framework cannot predict the shape and a reference implementation would mislead.

### Why not on the Blackboard

Blackboard is Run-scoped. Storing user preferences ("Alice prefers concise summaries") on Blackboard means re-reading them every run, never updating them across runs, and conflating "what I learned this run" with "what I know about Alice". Separate types prevent this confusion.

### Why Memory is not a pipeline

`api.Memory[T]` is deliberately a **storage interface**, not a memory **system**. It exposes Write / Read / Forget over scope-keyed entries. It does not:

- call an LLM to extract facts from raw conversations
- schedule periodic distillation jobs
- generate personas / scenarios / canvases automatically
- maintain a recall pipeline (BM25 + vector + RRF fusion)
- offload context to external files

Each of those is a **procedure** — an opinion about *how* to remember. By doc 09 Principle 3 (`Capability ≠ Procedure ≠ Policy ≠ Runtime`), procedures live outside the kernel. v0.9.0 will ship `recipe/memory-pyramid/` as one canonical procedure implementation; alternative procedures (e.g. domain-specific extractors) can coexist as separate recipes without touching `api.Memory[T]`.

### Self-knowledge convention

For an Agent's facts about itself, applications use `Identified.Scope() == ContextScopeAgent` and `Identified.SubjectID() == <AgentID>`.

To make this addressable without each recipe inventing a string, v0.8.0 documents the following convention:

- A recipe writing a self-knowledge entry MAY use the literal `SubjectID = "self"` as shorthand. The runtime's profile-driven entry path (`Runner.RunFromProfile`) substitutes the actual `AgentID` before the call reaches the registered `Memory[T]`, so storage always sees a concrete identifier.
- A read with `Scope=ContextScopeAgent, SubjectID=""` is rejected (would return cross-agent data). A read with `SubjectID="self"` is allowed only when the call is made inside an active Agent context where the runtime can resolve `self` to the calling AgentID.
- For replay determinism the resolution always uses the AgentID stamped on the originating Run, not the live profile. This keeps self-reads stable across profile renames.

This convention does NOT extend `Memory[T]`'s interface. `Write` / `Read` / `Forget` remain unchanged; the `self` token resolution is a Runner-layer concern. Memory backends never see the literal string `"self"`.

The reserved Capability `hydaelyn.self.memory.read` (see doc 02 and ADR-009 + ADR-013) is the eventual surface that exposes this convention to Agent prompts when an application provides a Memory implementation. ADR-013 makes the capability binding-activated rather than framework-built-in.

### Related work — informs application schema, not kernel schema

Two external projects influenced our thinking on memory design but, under the optional-plugin principle, no longer leave kernel-level marks:

- **TencentDB Agent Memory** — defines an L0 Conversation → L1 Atom → L2 Scenario → L3 Persona pyramid with deterministic drill-down via `node_id` and `result_ref`. Applications and recipes that want this shape encode it in their `T` and in the `recipe/memory-pyramid/` procedure on top of `Memory[T]`. The kernel does not encode the pyramid.
- **MemPalace** — local-first verbatim storage with pluggable retrieval backends and a temporal knowledge graph. Applications that want pluggable retrieval add a `Strategy` field to their `T` (or pass it via a typed query method on their concrete `Memory` impl).

Both projects can be implemented as Hydaelyn recipes on top of `api.Memory[T]` without modifying the kernel — that is the validation that the abstraction is correctly placed.

See also: `ADR-013-memory-kernel-vs-pipeline.md` and `13-memory-optional-plugin.md`.

## Artifact

`api/artifact.go` (new):

```go
package api

import (
    "context"
    "io"
    "time"
)

// Artifact is a large or binary output produced during a run. Artifacts are
// content-addressed (ID is derived from the payload) and reference-counted
// (Blackboard items may carry ArtifactRefs pointing to artifacts).
type Artifact struct {
    ID          string            `json:"id"`
    Name        string            `json:"name,omitempty"`
    ContentType string            `json:"contentType"`
    Size        int64             `json:"size"`
    Checksum    string            `json:"checksum"`       // hex of SHA-256
    Tags        []string          `json:"tags,omitempty"`
    Metadata    map[string]string `json:"metadata,omitempty"`
    CreatedAt   time.Time         `json:"createdAt"`
    CreatedBy   string            `json:"createdBy,omitempty"` // task ID or agent ID
    RunID       string            `json:"runId,omitempty"`
}

// ArtifactStore handles binary content storage. The store is separate from
// the metadata index (which lives in the UnitOfWork).
type ArtifactStore interface {
    Put(ctx context.Context, meta Artifact, content io.Reader) (Artifact, error)
    Get(ctx context.Context, id string) (Artifact, io.ReadCloser, error)
    Stat(ctx context.Context, id string) (Artifact, error)
    Delete(ctx context.Context, id string) error
    List(ctx context.Context, sel ArtifactSelector) ([]Artifact, error)
}

type ArtifactSelector struct {
    RunID     string
    CreatedBy string
    Tags      []string
    ContentType string
    Since     time.Time
    Limit     int
}
```

### BlackboardItem.ArtifactRefs

Existing `BlackboardItem.ArtifactRefs []string` is reinterpreted: each string is an `Artifact.ID` resolved against the configured `ArtifactStore`. No struct change; documentation update only.

### Default implementations

- `artifact/filesystem/` — disk-backed, configured with a base directory. Default for development.
- `artifact/inmem/` — in-process, byte-slice store. Tests only.

S3/GCS/Azure backends are out of scope for v0.8.0 framework; users implement against the interface or wait for community packages.

## ContextSource

`api/context.go`:

```go
// ContextSource is a typed reference to external context the Agent may pull
// from at runtime. Unlike Memory and Artifact, ContextSource does NOT store
// content — it stores a pointer and a binding configuration.
type ContextSource struct {
    ID         string            `json:"id"`
    Kind       ContextSourceKind `json:"kind"`
    URI        string            `json:"uri"`      // semantic varies by Kind
    Auth       string            `json:"auth,omitempty"` // reference to credential, not the credential itself
    Filter     map[string]string `json:"filter,omitempty"`
    Metadata   map[string]string `json:"metadata,omitempty"`
}

type ContextSourceKind string

const (
    ContextSourceURL            ContextSourceKind = "url"
    ContextSourceVector         ContextSourceKind = "vector_index"
    ContextSourceDatabase       ContextSourceKind = "database"
    ContextSourceFile           ContextSourceKind = "file"
    ContextSourceAPI            ContextSourceKind = "api"
    ContextSourceKnowledgeGraph ContextSourceKind = "knowledge_graph" // reserved, v0.9.0+ resolver
)
```

The framework does **not** ship resolvers for these kinds. ContextSource is a declaration; pack/recipe code resolves them. The role of the framework is to give a stable schema for storing and referencing them.

`AgentProfile` does NOT gain a `ContextSources` field in v0.8.0 — that is deferred to v0.9.0 once resolver patterns are clearer. ContextSource type is defined now to give the abstraction a home before packs need it.

## ADR-011 mention

A new ADR (`docs/adr/ADR-011-context-four-layer-model.md`) records the rationale: why four layers, why they don't fold into one another, what conventions apply when an entry could logically live in multiple layers.

## Verification

- `TestMemorySelector_ZeroValueIsValid` — zero-value selector imposes no extra restrictions (in `api/memory_test.go`)
- (Compile-time guarantee in `api/memory_test.go`: `var _ api.Memory[fakeEntry] = (*fakeStore)(nil)` proves the interface is satisfiable by an application-defined `T`.)
- (No reference-implementation tests ship; applications provide their own `Memory[T]` test coverage.)
- `TestArtifact_Filesystem_PutGetRoundTrip`
- `TestArtifact_Filesystem_ChecksumMatchesContent`
- `TestArtifact_Filesystem_DeleteRemovesContent`
- `TestArtifact_Inmem_ConcurrentPut`
- `TestBlackboardItem_ArtifactRefs_ResolveAgainstStore`
- `TestContextSource_JSONRoundTrip` (all 6 Kinds incl. `knowledge_graph`)
