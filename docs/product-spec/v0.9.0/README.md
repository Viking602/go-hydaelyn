# Hydaelyn v0.9.0 — Roadmap Stub

## Status

**Not started.** This directory reserves the space for v0.9.0 design docs. Detailed specs are written after v0.8.0 ships and the downstream OceanBase 4.x project surfaces real production feedback.

## Theme (provisional)

**Memory and context maturity.** v0.8.0 ships the *interfaces* for Memory, Artifact, ContextSource, and storage adapters. v0.9.0 ships the *implementations* that operate on top of those interfaces — including the layering pipelines and retrieval strategies that v0.8.0 deliberately left as `reserved` enum values.

## Anchored scope from v0.8.0 reserved fields

Each item below corresponds to a `// reserved for v0.9.0+` marker that already exists in v0.8.0 schema. v0.9.0's job is to implement them without breaking the v0.8.0 schema.

| v0.8.0 reservation | v0.9.0 deliverable |
|---|---|
| `recipe/memory-pyramid/` (planned) | L0→L3 extraction pipeline that operates over an application-provided `api.Memory[T]`. The recipe defines the `T` shape it requires (atoms, scenarios, personas) and the application either uses that `T` or composes its own with adapters. Reference design: TencentDB Agent Memory L0 Conversation → L1 Atom → L2 Scenario → L3 Persona. |
| `recipe/memory-retrieval/` (planned) | BM25 + semantic + hybrid (RRF) retrieval procedures that wrap an application-provided `api.Memory[T]` and a vector backend chosen by the application. The framework no longer ships `storage/sqlite-vec/` or `storage/postgres-pgvector/` Memory backends; those belong in application code or community packages. |
| `ContextSourceKind = "knowledge_graph"` reserved | KG resolver implementation with validity windows. Reference design: MemPalace temporal KG. |
| `storage/postgres/` `SupportsBlackboardSubscribe = false` | Implement `LISTEN/NOTIFY`-based Subscribe; flip capability flag to true. |
| `worker/` symbolic short-term memory placeholder | `recipe/context-canvas/` — Mermaid-encoded task state in Blackboard with Artifact-backed offload. Reference design: TencentDB Agent Memory symbolic memory. |
| `hydaelyn.self.*` reserved Capability namespace (doc 02) | Ship the four built-ins as framework-provided Capabilities: `hydaelyn.self.profile`, `hydaelyn.self.memory.read`, `hydaelyn.self.history`, `hydaelyn.self.summarize_history`. Each is a read-only query against AgentProfile / Memory / RunSelector — no profile mutation, no auto-derived persona. |

## Out-of-scope for v0.9.0 (deferred further)

- LLM-as-judge eval assertions
- A/B testing infrastructure
- Hosted observability backends beyond `observe/otel/` skeleton
- Domain packs (`packs/research`, `packs/devops`, etc.) beyond their v0.8.0 skeletons

## Process gate

v0.9.0 design doc writing **does not start** until:

1. v0.8.0 has been tagged.
2. Downstream OceanBase 4.x project has completed its v0.8.0 upgrade and run in production for at least 2 weeks.
3. At least one bug or feedback report from real production usage is filed against `api.Memory`, `api.ArtifactStore`, or `api.ContextSource`.

Without (3), v0.9.0 would design speculatively against unvalidated interfaces — same trap that v0.8.0 chose to avoid by deferring these items.
