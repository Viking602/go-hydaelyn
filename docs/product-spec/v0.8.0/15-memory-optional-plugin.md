# 15 — Memory as an Optional Plugin

> Renumbered from `13-memory-optional-plugin.md`. Substance preserved
> verbatim from ADR-013. The only changes are cross-references updated
> to the renumbered file set (07-storage, 09-context, 02-capability).

## Decision

`api.Memory[T Identified]` is part of the kernel — the verbs (`Put`/`Get`/`Query`/`Delete`) live in `memory/`. The framework ships **no** backend implementation. Vector DB / KV / file / in-memory backends are application-owned.

This document is the single source for the architectural rule. ADR-013 is the authoritative ADR.

## Why optional

- Memory is a *capability* an agent may or may not need.
- Backends span Pinecone, Weaviate, pgvector, Redis, Mongo, S3-as-KV, plain files. Shipping any one would force a dependency the framework cannot defend.
- The framework's job is to give the agent a *binding* surface: "when memory is registered for this AgentID, the agent can read/write it." The backend itself is replaceable.

## Interface (the only thing in the kernel)

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

type EmbeddingMatch struct {
    Vector    []float32
    Threshold float32
}
```

That is the entirety of the kernel surface. No `MemoryProvider`, no
`MemoryStore`, no `MemoryStoreCapabilities`. Backends are pure user
code.

## Binding

```go
func (r *Registry) BindMemory(agentID AgentID, m memory.Memory[T]) error
```

After binding, `hydaelyn.self.memory.read` (`02-capability.md`)
becomes active for that AgentID. The `agent.ContextManager` may also
pull from bound Memory when assembling context.

Unbound agents that attempt self-memory reads receive
`ErrMemoryNotBound`. There is no implicit fallback to a "default"
backend — the framework refuses to invent one.

## Cross-references

- `07-storage.md` — Position D for run-scoped storage. Memory is
  *not* a run-scoped store and does not appear in `UnitOfWork`.
  Memory lives across runs.
- `09-context.md` — Memory is one of the four context primitives;
  the in-loop `agent.ContextManager` consumes it when bound.
- `02-capability.md` — `hydaelyn.self.memory.read` is the reserved
  capability that activates only on bound AgentIDs.
- ADR-013 — full architectural justification.

## What the framework explicitly does NOT ship

- A vector DB backend
- A file-based backend (even for `_examples/`)
- A reference KV backend
- Migration helpers between backends
- A "default" memory provider

The framework will accept community backend modules as separate
go-modules (e.g. `github.com/<org>/hydaelyn-memory-pgvector`) but
will not vendor any of them into the kernel.

## Verification

- `TestMemoryInterface_PutGetRoundTrip_BoundBackend` — using a test-only fake registered by the test, not by the framework
- `TestMemoryUnbound_ReadReturnsErrMemoryNotBound`
- `TestRegistry_BindMemory_ActivatesSelfMemoryReadCapability`
- `TestMemoryQuery_FilterEmbeddingMatch_BothInteract`
- `grep -r "package memory" memory/` returns interface file only; no backend impl
- `grep -r "memory.Memory\[" memory/` returns interface only; no concrete `struct` implementing it
