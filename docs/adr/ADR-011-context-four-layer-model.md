# ADR-011 Context Four-Layer Model — Blackboard / Memory / Artifact / ContextSource

## Status

Accepted — enforced from v0.8.0 onward. Anchor document: `docs/product-spec/v0.8.0/07-context.md`. Companion ADR-013 covers why Memory in the kernel is storage-only.

## Context

By v0.7.x the only structured context surface was Blackboard. Several distinct categories of "context" were being shoved into it and onto each other:

1. **Run-scoped working memory** (the original Blackboard purpose).
2. **Cross-run, agent-scoped facts** ("Alice prefers concise summaries") — was being written to Blackboard then re-loaded every run, then never updated, then drifted.
3. **Large binary outputs** (PDFs, screenshots, > 1 MB JSON) — was being base64-stuffed into `BlackboardItem.Payload`, blowing up read latency.
4. **External pointers** (vector index, knowledge graph, file URL, API endpoint) — had no home; were being stored as `BlackboardItem` of type `context` with `Metadata["uri"]`.

Each category has a distinct scope axis and lifetime. Mixing them produced three problems: Blackboard reads got slow, Memory updates never happened, and external references were inconsistently typed.

## Decision

Four layers, four interfaces. Each layer has its own scope axis and lifetime. The framework defines them as **separate types** in `api/` to prevent conflation.

| Layer | Scope | Lifetime | Typical content | Interface |
| ----- | ----- | -------- | --------------- | --------- |
| **Blackboard** | Run | Run | Claims, evidence, findings, task outputs, handoff snapshots | `BlackboardReadWriter` (unchanged) |
| **Memory** | Agent / User / Tenant | Cross-run, long-term | Conversation history, preferences, learned facts | `api.Memory` (new) |
| **Artifact** | Run / Project | Long-term | Binary or large structured output | `api.ArtifactStore` (new) |
| **ContextSource** | Reference | N/A (pointer) | URL, vector index, file path, API endpoint, knowledge graph (reserved) | `api.ContextSource` type (resolution lives in packs) |

### ContextScope axis

A single new enum centralizes the scope vocabulary:

```go
type ContextScope string

const (
    ContextScopeRun    ContextScope = "run"
    ContextScopeTask   ContextScope = "task"
    ContextScopeAgent  ContextScope = "agent"
    ContextScopeUser   ContextScope = "user"
    ContextScopeTenant ContextScope = "tenant"
)
```

`Memory.Scope` uses this. Future ContextSource scoping will also use this. Blackboard remains Run-scoped by construction (RunID is part of every entry).

### Layer-specific decisions

- **Blackboard** stays exactly as-is. The seven built-in `BlackboardItemType` constants are **recommendations, not a closed enum** — `api/types.go` doc-comment clarifies this so user code can introduce types like `diff`, `compile_error`, `metric` without runtime opposition.
- **Memory** in v0.8.0 is a storage interface only. `Write` / `Read(Selector) / Forget`. Subjects keyed by `(Scope, SubjectID)`. Extraction pipelines (`recipe/memory-pyramid/`) are deferred to v0.9.0 — see ADR-013.
- **Memory.Refs / Memory.Layer / Memory.RetrievalStrategy** are not kernel-level fields. ADR-013 (revised) reframes Memory as an optional plugin where the framework defines `Memory[T Identified]` verbs and the application defines `T`. Drill-down chains, layer hints, and retrieval strategy are application schema. The v0.9.0 `recipe/memory-pyramid/` defines its own L0→L3 conventions on top of whatever `T` the application picks; the kernel does not encode the pyramid.
- **Artifact** is content-addressed (ID derives from payload), checksummed (SHA-256), and reference-counted via `BlackboardItem.ArtifactRefs`. Two default backends: `artifact/filesystem/` (disk) and `artifact/inmem/` (tests). S3 / GCS / Azure are user code or community packages.
- **ContextSource** is a type for storing pointers + binding configs. The framework does NOT ship resolvers; packs do. Six kinds are declared: `url`, `vector_index`, `database`, `file`, `api`, `knowledge_graph` (the last reserved for v0.9.0+ resolvers — see MemPalace temporal KG reference design).
- **`AgentProfile.ContextSources` field is NOT added in v0.8.0.** The type exists; the binding to AgentProfile waits until v0.9.0 once resolver patterns are clearer.

### Self-knowledge convention (cross-cutting)

For self-knowledge — an Agent's facts about itself — applications use `Identified.Scope() == ContextScopeAgent` and `Identified.SubjectID() == <AgentID>`. The Runner may resolve the shorthand `SubjectID = "self"` to the live AgentID before the call reaches `Memory[T]`, so a registered backend never sees the literal `"self"`. For replay determinism the resolution uses the AgentID stamped on the originating Run, not the live profile. This is what gives the reserved `hydaelyn.self.memory.read` capability (ADR-009, binding-activated per ADR-013) a precise meaning when an application provides a Memory implementation.

## Why these four, why not three, why not five

- **Three (Blackboard + Memory + Artifact)** would force ContextSource to live in `Metadata` maps. Pack code would re-invent the same URI / Kind / Auth pattern five different ways.
- **Five (adding e.g. KnowledgeGraph as its own top-level layer)** would force KG into the kernel before any KG resolver design has been validated. Reserving `ContextSourceKnowledgeGraph` as one Kind of ContextSource costs nothing and keeps the option open.
- The four chosen layers each cover a distinct quadrant of the (scope, lifetime) plane. Collapsing any two creates the v0.7-era confusion the model exists to prevent.

## Anti-patterns rejected by this ADR

| Anti-pattern | Why it is wrong |
| ------------ | --------------- |
| Storing a PDF as base64 in `BlackboardItem.Payload` | Bloats Blackboard reads; use Artifact + `ArtifactRefs` |
| Storing user preferences on Blackboard | Re-loaded per run, never updated cross-run; use Memory with `Scope=ContextScopeUser` |
| Treating Memory as a run-scoped scratchpad | That is Blackboard; Memory is for cross-run facts only |
| Stuffing a URL into Memory as `Content = "https://..."` | That is a ContextSource; storing strings shaped like pointers in Memory inverts the indirection |
| Adding a new Blackboard `BlackboardItemType` constant via PR to `api/types.go` | The built-in seven are recommendations; user code defines its own types as string constants in user packages |
| Implementing automatic Memory extraction in the runtime | That is a recipe (see ADR-013); the kernel exposes the storage interface only |
| Reading Memory with `SubjectID=""` to fetch "all agents' memories" | Rejected — would cross subject boundaries; selector requires concrete `SubjectID` or `"self"` (resolved by Runner) |
| Treating `ContextSource.Auth` as a credential | It is a **reference to** a credential; the actual secret lives in a secret store the pack resolves at use time |

## Impact

- Pack authors have four typed surfaces to write against instead of one overloaded one.
- Memory backends (sqlite-vec, postgres-pgvector planned for v0.9.0) can be added without changing pack code — the interface is fixed.
- Artifact storage backends (filesystem default; S3/GCS via community) all satisfy the same `ArtifactStore` interface; swapping is a config change.
- The reserved Capability `hydaelyn.self.memory.read` has a precise specification (it reads from a registered `Memory[T]` with `Identified.Scope()=Agent` and `Identified.SubjectID()=<calling AgentID>`) when an application provides a Memory implementation. ADR-013 makes the capability binding-activated rather than framework-built-in.
- Performance: large outputs no longer congest Blackboard reads; Memory updates have a real Write path; URL pointers have a typed home.

## References

- Spec: `docs/product-spec/v0.8.0/07-context.md` (full type definitions, default impls, related work analysis)
- Companion ADR: ADR-013 (Memory kernel vs pipeline — why Memory is storage-only in v0.8.0)
- Related: ADR-006 (Blackboard / Evidence data model — original Blackboard design), ADR-009 (Capability — `hydaelyn.self.memory.read` reservation), ADR-014 (Agent ontology — self-knowledge convention)
- External reference designs cited: TencentDB Agent Memory (L0→L3 pyramid), MemPalace (temporal knowledge graph + pluggable retrieval)
