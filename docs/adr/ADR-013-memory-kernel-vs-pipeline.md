# ADR-013 Memory as Optional Plugin — interface in the kernel, storage in the application

## Status

Accepted — enforced from v0.8.0 onward. Supersedes the previous ADR-013 *Memory Kernel vs Pipeline* draft, which proposed `memory/inmem/` as a v0.8.0 reference implementation with a kernel-locked `MemoryEntry` schema (Content / Embedding / Refs / Layer / RetrievalStrategy). Anchor document: `docs/product-spec/v0.8.0/13-memory-optional-plugin.md`. Supports `docs/product-spec/v0.8.0/09-boundaries.md` Principle 3.

## Context

The earlier draft of this ADR addressed a real problem (procedures should not be baked into the storage interface) by introducing a layered schema — `MemoryEntry{ Content, Embedding, Refs, Layer, Tags, Metadata }` — and shipping `memory/inmem/` as the v0.8.0 reference. Two problems with that draft became apparent before any code landed:

1. **Memory schemas are domain-specific.** Unlike framework-owned control-plane entities (Run, Task, Lease, Event), memory entities are application content: chat turns, user preferences, learned facts, vector chunks, knowledge-graph nodes, audit traces. The shape varies per application. A reference `MemoryEntry` shaped around "text + embedding + layer" actively misleads applications whose memory looks nothing like that, and the framework cannot enumerate the shapes in advance.
2. **Storage choice is the application's.** Real applications already have a database, an ORM, a migration tool. Asking them to adopt the framework's in-process memory store (or even a "starter SQLite" variant) is friction that buys no value — they will discard it within an iteration.

The same logic does **not** apply to `StoreProvider` (Run / Task / Lease / Event storage). Those entities have framework-decided schemas because they encode framework-owned control flow; reference implementations under `storage/{memory,sqlite,postgres,mysql}` legitimately accelerate adoption without misleading downstream. The asymmetry between Memory (no reference impl) and StoreProvider (reference impls retained) is intentional and tracks who owns the schema.

## Decision

In v0.8.0, **`api.Memory[T Identified]` is an optional plugin contract.** The framework defines the verbs and the isolation boundary. The application defines the entity type `T` and the storage implementation. The framework ships no reference Memory backend.

### What is in the kernel

`api.Memory[T Identified]` exposes exactly three methods:

```go
type Identified interface {
    ID() string
    Scope() ContextScope
    SubjectID() string
}

type Memory[T Identified] interface {
    Write(ctx context.Context, entry T) error
    Read(ctx context.Context, sel MemorySelector) ([]T, error)
    Forget(ctx context.Context, sel MemorySelector) (int, error)
}

type MemorySelector struct {
    Scope     ContextScope
    SubjectID string
    IDs       []string // optional: empty = no ID restriction
    Limit     int      // optional: 0 = unbounded
}
```

The full surface is documented in `docs/product-spec/v0.8.0/13-memory-optional-plugin.md` and `api/memory.go`.

### What is NOT in the kernel

- No `MemoryEntry` type. Application defines `T`.
- No `Content` / `Embedding` / `Tags` / `Metadata` / `CreatedAt` / `ExpiresAt` / `Refs` / `Layer` fields on any framework type. These are application schema.
- No `RetrievalStrategy` enum and no `ErrUnsupportedStrategy` sentinel. Retrieval algorithms are recipe or application concerns.
- No reference implementation. No `memory/inmem/`. No long-term memory backend anywhere in the tree. The framework ships zero Memory backends (and, per ADR-012 revised, zero StoreProvider backends either).
- No runtime dependency on Memory being configured. Code paths that *would* use Memory must check presence and degrade gracefully.

### Capability binding

ADR-009 reserves `hydaelyn.self.memory.read` (`CapabilityNameSelfMemoryRead`). Under this ADR, the capability is **binding-activated**: it becomes available only when the application registers a `Memory[T]` implementation against the runtime. When no Memory is registered, the capability does not appear in any manifest — no error, no stub, no placeholder. Multiple `Memory[T]` registrations distinguish themselves via a registration name (e.g., `chat_history` vs `user_preferences`).

### Reserved fields, retired

The previous ADR draft reserved `Refs`, `Layer`, and `RetrievalStrategy` to avoid post-launch migrations. Under the new principle those reservations are no longer the framework's concern — the application owns `T`, so the application chooses whether and how to express drill-down chains, layer hints, or retrieval strategy. The framework's only obligation is to round-trip whatever the application puts in `T`.

## Anti-patterns rejected by this ADR

| Anti-pattern | Why it is wrong |
| ------------ | --------------- |
| Adding `memory/inmem` back as "just a development convenience" | Re-introduces the schema bias the principle exists to avoid. Applications that want a development convenience implement a 30-line `map[string][]T` themselves. |
| Letting `MemorySelector` carry `Tags []string` or `Metadata map[string]string` | These are application schema; the kernel does not own them. |
| Re-introducing `RetrievalStrategy` so the framework can declare "Substring vs Semantic" | Retrieval algorithm is a procedure; lives in a recipe. |
| Making `Memory[T]` a runtime requirement (panicking or erroring if not configured) | Memory is optional. Code paths that *would* use Memory must check presence and degrade gracefully. |
| Shipping a generic SQL-backed `Memory[T]` based on reflection | Same schema-bias trap: once the framework picks Postgres-vs-SQLite-vs-MySQL or column shape, it has stopped being the application's storage decision. |

## Impact

- v0.8.0 ships a stable, minimal, generic Memory interface that applications plug into using their existing database + ORM, with no framework concessions.
- v0.9.0 procedures (`recipe/memory-pyramid/`, retrieval pipelines) are designed against this interface without forcing a storage opinion on adopters.
- Applications that prefer to skip `api.Memory` entirely and use their own memory mechanism remain first-class citizens.
- The framework retires the planned `memory/inmem/` package and several types/fields (`MemoryEntry`, retrieval strategy enum), shrinking the v0.8.0 public surface and the SemVer commitment.

## References

- Anchor: `docs/product-spec/v0.8.0/13-memory-optional-plugin.md`
- Companion: ADR-009 (Capability — `hydaelyn.self.memory.*` namespace, now binding-activated)
- Related: ADR-008 (framework vs business), ADR-011 (Context four-layer — Memory is one layer), ADR-014 (Agent ontology stance)
- Supersedes parts of: prior draft of this ADR (kernel-vs-pipeline); `docs/release-notes/v0.8.0.md` § "What's not in v0.8.0 yet"; `docs/migration.md` § same.
