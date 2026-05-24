# 13 — Memory as Optional Plugin

## Status

Accepted — supersedes the v0.8.0 reference-implementation strategy in earlier drafts of ADR-013 and the "deferred to v0.9.0" language in `docs/release-notes/v0.8.0.md` and `docs/migration.md`.

## Goal

Re-anchor `api.Memory` on a principle the framework can hold indefinitely: **the framework owns the verbs, the application owns the nouns and the storage.** v0.8.0 ships the interface. v0.8.0 does not ship a storage backend — neither in-process nor durable — and the framework's own code paths never require Memory to be configured.

## Why this changes from earlier drafts

Earlier v0.8.0 drafts shipped `memory/inmem/` as a reference implementation alongside a v0.8.0-locked `MemoryEntry` schema (`Content`, `Embedding`, `Refs`, `Layer`, `Tags`, `Metadata`, `RetrievalStrategy`). Two forces push against this:

1. **Memory schemas are domain-specific.** Unlike control-plane entities (Run / Task / Lease / Event) whose shape the framework legitimately fixes, memory entities are application content: chat turns, user preferences, learned facts, vector chunks, knowledge-graph nodes, audit traces. Each application has its own shape and its own retrieval needs. A reference `MemoryEntry` shaped around "text + embedding + layer" actively misleads applications whose memory looks nothing like that — and the framework cannot enumerate the shapes in advance.
2. **Storage choice is the application's.** Real applications already have a database, an ORM, a migration tool. Asking them to adopt the framework's in-process memory store (or even a "starter SQLite memory") is friction that buys no value — they will discard it within an iteration. The honest position: framework defines what it needs to call into the application's existing data layer, and stops there.

The same logic applies to `StoreProvider` (Run / Task / Lease / Event storage) as well — see the 2026-05-24 revision to ADR-012 (Position D). The framework owns the contract and the contract test suite; the framework ships no reference implementation, public or otherwise. The asymmetry this paragraph originally recorded ("StoreProvider entities have a framework-decided schema, therefore ref impls are fine") was the loophole that let Layer 2 survive Memory's purification. Position D closes it.

## Principle

Memory is an **optional plugin** in v0.8.0 and onward:

- The framework defines `api.Memory[T Identified]` — the verbs `Write` / `Read` / `Forget` and the isolation contract via `Identified`.
- The application defines `T` (every field other than identity) and provides the storage implementation.
- The framework ships **no Memory backend** — not in-process, not durable, not as an `_examples` scaffold. Applications either implement `api.Memory[T]` themselves or skip the interface entirely and use their own memory mechanism.
- Framework code paths must function without Memory configured. Memory is a capability the application can plug in, not a runtime dependency.

This stance mirrors the framework's existing positions on Provider drivers (no framework-bundled LLM) and Tools (no framework-bundled tool library): the framework defines the contract, the application supplies the matter.

## Public surface — `api/memory.go`

```go
package api

import "context"

// Identified is the minimum identity contract any memory entity must satisfy.
// The framework uses Scope+SubjectID to enforce isolation and ID for selective
// deletion. Every other field on the entity belongs to the application's schema.
type Identified interface {
    ID() string
    Scope() ContextScope
    SubjectID() string
}

// Memory is the optional plugin contract for application memory.
// T is the application-owned entity (ChatMessage, UserPreference, LearnedFact,
// vector chunk, knowledge-graph node, ...). Storage is entirely the
// application's responsibility — the framework ships no backend.
//
// Applications may also choose not to implement this interface at all and use
// their own memory mechanism. The framework's runtime does not require a
// Memory to be configured.
type Memory[T Identified] interface {
    Write(ctx context.Context, entry T) error
    Read(ctx context.Context, sel MemorySelector) ([]T, error)
    Forget(ctx context.Context, sel MemorySelector) (int, error)
}

// MemorySelector filters by identity only. Filtering by application-specific
// fields (tags, content, time range, layer) is not part of the kernel
// contract. Applications either over-fetch and filter in code, or expose
// typed query methods on their concrete Memory implementation.
type MemorySelector struct {
    Scope     ContextScope
    SubjectID string
    IDs       []string // optional: empty = no ID restriction
    Limit     int      // optional: 0 = unbounded
}
```

What this surface **does not** include (compared to earlier drafts):

- No `MemoryEntry` type — the application defines `T`.
- No `Kind` / `Content` / `Embedding` / `Tags` / `Metadata` / `CreatedAt` / `ExpiresAt` / `Refs` / `Layer` fields on a framework type — these are all application schema.
- No `RetrievalStrategy` enum and no `ErrUnsupportedStrategy` sentinel — retrieval algorithms are recipe / application concerns.

## Capability binding (ADR-009 amendment)

ADR-009 reserves `hydaelyn.self.memory.read` (constant `CapabilityNameSelfMemoryRead`) as a framework-built-in capability scheduled for v0.9.0+ implementation. Under this design, its semantics change from **built-in (framework-provided implementation later)** to **binding-activated (application-provided implementation)**:

- When the application registers a `Memory[T]` implementation against the runtime, `hydaelyn.self.memory.read` becomes available bound to that registration.
- When no Memory is registered, the capability does not appear in any manifest — no error, no stub, no placeholder. Graceful absence.
- A single runtime may register multiple `Memory[T]` implementations for different `T`. Capability binding distinguishes them via the registration name (a `name` qualifier such as `chat_history` vs `user_preferences`).
- This design does **not** add new reserved names (`.memory.write` / `.memory.forget`) under `hydaelyn.self.*`. The `Write` / `Forget` verbs on `Memory[T]` are first-class methods on the interface but are not promoted to reserved capability names — applications expose them as their own capabilities if they want them callable via the Capability surface.

## What gets removed

**Code (under repo root):**

- Delete the `memory/inmem/` package wholesale.
- Rewrite `api/memory.go` to the surface above. Delete the previous `MemoryEntry`, `MemorySelector` shape, `RetrievalStrategy` enum, and `ErrUnsupportedStrategy` sentinel.
- Audit and remove framework call sites that depended on the previous Memory surface (if any landed in v0.8.0 branch).

**Docs:**

- ADR-013 rewritten under the new principle (title becomes *Memory as Optional Plugin — interface in the kernel, storage in the application*).
- `docs/product-spec/v0.8.0/07-context.md` — Memory section rewritten.
- `docs/product-spec/v0.8.0/09-boundaries.md` Principle 3 — wording updated.
- `docs/product-spec/v0.8.0/10-package-structure.md` — drop `memory/inmem/` from the layout.
- `docs/release-notes/v0.8.0.md` — Memory moves from "deferred to v0.9.0" to "shipped as optional plugin interface; no reference implementation by design."
- `docs/migration.md` — same wording adjustment.
- ADR-009 — append an amendment paragraph on binding-activated `hydaelyn.self.memory.*` semantics.

## What stays

- `recipe/memory-pyramid/` placeholder is unchanged. A pyramid recipe is a *procedure* on top of `api.Memory[T]`; the choice to make Memory optional does not affect whether the pyramid recipe makes sense. The recipe in v0.9.0+ documentation must say "bring your own `Memory[T]` implementation," not "use the framework's inmem."
- The `ContextScope` enum stays; it is referenced by `Identified.Scope()` and remains a kernel concept (agent / user / tenant isolation boundary).

## Contract test suite

`contract.RunMemoryContractTests[T]` is **not** delivered in this change. The contract suite needs a generic mechanism for callers to supply (a) a constructor for an empty `Memory[T]`, (b) a sample-`T` factory, (c) a cleanup hook. Designing that suite is a separable, additive change that does not block shipping the interface. Adding it later does not break SemVer.

## Anti-patterns rejected

| Anti-pattern | Why it is wrong |
|--------------|-----------------|
| Adding `memory/inmem` back as "just a development convenience" | Re-introduces the schema bias the principle exists to avoid. Applications that want a development convenience implement a 30-line `map[string][]T` themselves. |
| Letting `MemorySelector` carry `Tags []string` or `Metadata map[string]string` | These are application schema; the kernel does not own them. Filter in application code, or add typed methods to the concrete impl. |
| Re-introducing `RetrievalStrategy` so the framework can declare "Substring vs Semantic" | Retrieval algorithm is a procedure; lives in a recipe, not in the storage interface. |
| Making `Memory[T]` a runtime requirement (panicking or erroring if not configured) | Memory is optional. Code paths that *would* use Memory must check presence and degrade gracefully. |
| Shipping a generic SQL-backed `Memory[T]` based on reflection | Same schema-bias trap: the moment the framework picks Postgres-vs-SQLite-vs-MySQL or column shape, it has stopped being the application's storage decision. |
| Coupling `Memory[T]` resolution to a global registry that requires startup-time registration | Optional plugins should be passed explicitly through `api.Config` (or equivalent) so that runtimes without Memory remain trivially constructable. |

## Impact

- v0.8.0 ships a stable, minimal, generic Memory interface that applications plug into using their existing database + ORM, with no framework concessions.
- v0.9.0 procedures (`recipe/memory-pyramid/`, retrieval pipelines) are designed against this interface without forcing a storage opinion on adopters.
- Applications that prefer to skip `api.Memory` entirely and use their own memory mechanism remain first-class citizens — the framework does not penalize them.
- The framework retires `memory/inmem/` and several public types/fields (`MemoryEntry`, retrieval strategy enum), shrinking the v0.8.0 public surface and the SemVer commitment.

## References

- Supersedes parts of: ADR-013 (memory kernel vs pipeline), `docs/release-notes/v0.8.0.md` § "What's not in v0.8.0 yet," `docs/migration.md` § same.
- Companion: ADR-009 (Capability — `hydaelyn.self.memory.*` namespace).
- Related: ADR-008 (framework vs business), ADR-011 (Context four-layer — Memory is one layer), ADR-014 (Agent ontology stance).
