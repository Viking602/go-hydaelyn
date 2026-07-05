# Hydaelyn v0.9.0 — Roadmap Stub

## Status

**Not started.** This directory reserves the space for v0.9.0 design
docs. Detailed specs are written after v0.8.0 ships and the first
production multi-agent deployments surface real feedback.

## Theme (provisional)

**Scheduler strategy maturity + memory pipeline implementation.**

v0.8.0 ships three reference Schedulers (Sequential, Router,
Supervisor), the typed Handoff primitive, multi-agent Blackboard, and
the interfaces for Memory, Artifact, ContextSource, and storage. It
deliberately defers (a) advanced scheduler strategies and (b) the
implementations that operate on top of the v0.8.0 interfaces.

v0.9.0 picks up exactly those two threads.

## Anchored scope from v0.8.0 deferrals

### Multi-agent strategy strategies (deferred from v0.8.0)

v0.8.0 ships only the three reference Schedulers; v0.9.0 fleshes
out the strategy set:

| Strategy | v0.9.0 deliverable |
|----------|--------------------|
| `DebateScheduler` | N-agent debate with rounds, scoring, and supervisor-led termination. Built on `multiagent.VotingResult`. |
| `MapReduceScheduler` | Fan-out a Task to M agents in parallel, fan-in via configurable aggregator. Uses `EventStore.Sequence` for in-order replay. |
| `DAGScheduler` | Explicit dependency graph between Tasks; supports diamonds and skips per `multiagent.Dispatch.Skip`. |
| `SwarmScheduler` | Dynamic team-size scheduling with admission control via PolicyEngine. Strict TaskBudget summation. |

All must satisfy three-surface reconstruction
(`v0.8.0/11-boundaries.md` Principle 5) — extensive resume kill tests
land alongside the impls.

### Memory pipeline implementations (deferred from v0.8.0)

Each item below was reserved at the spec level in v0.8.0 (docs only —
no `// reserved` code markers or interfaces exist in the tree; verified
2026-06-10). v0.9.0's job is to ship the implementations without
breaking the v0.8.0 schema.

| v0.8.0 reservation | v0.9.0 deliverable |
|---|---|
| `recipe/memory-pyramid/` (planned) | L0→L3 extraction pipeline that operates over an application-provided `memory.Memory[T]`. The recipe defines the `T` shape it requires (atoms, scenarios, personas) and the application either uses that `T` or composes its own with adapters. Reference design: TencentDB Agent Memory L0 Conversation → L1 Atom → L2 Scenario → L3 Persona. |
| `recipe/memory-retrieval/` (planned) | BM25 + semantic + hybrid (RRF) retrieval procedures that wrap an application-provided `memory.Memory[T]` and a vector backend chosen by the application. The framework ships no storage backends (ADR-012 revised, Position D). |
| `ContextSourceKind = "knowledge_graph"` reserved | KG resolver implementation with validity windows. Reference design: MemPalace temporal KG. |
| `recipe/context-canvas/` (planned) | Mermaid-encoded task state in Blackboard with Artifact-backed offload — symbolic short-term memory pattern. |
| `hydaelyn.self.*` reserved Capability namespace (v0.8.0 doc 02) | Ship the four built-ins as framework-provided Capabilities: `hydaelyn.self.profile`, `hydaelyn.self.memory.read`, `hydaelyn.self.history`, `hydaelyn.self.summarize_history`. Read-only queries against AgentProfile / Memory / RunSelector. |

### Observability (deferred from v0.8.0)

- `observe/otel/` exporter implementation (new package — contrary to
  earlier drafts of this stub, v0.8.x ships no `observe/` skeleton;
  verified 2026-06-10)
- Multi-agent trace shape: scheduler tick span → dispatch span → agent loop span → step span → tool span, with parent links across the three-surface reconstruction
- Pluggable trace exporters for Jaeger / Tempo / Honeycomb / DataDog

## Debt carried from v0.8.0 (audited 2026-06-10)

A whole-repo audit on 2026-06-10 found these v0.8.0 spec promises
unimplemented with no documented deferral. Items marked **landed** were
closed by the post-audit debt PRs; the rest belong to v0.9.0 scope
regardless of theme:

- **landed** — `HandoffStore` / `TeamStateStore` / `AgentInstanceStore`
  in `api.UnitOfWork` + conformance suite (spec 07; ADR-016 §6).
- **landed** — usage-metering write path (worker now appends
  `UsageRecord`s; `Runner.AppendUsage/QueryUsage/SumUsageCredits`).
- **landed** — provider stream-initiation retry/backoff and
  `provider.Fallback` model failover.
- **landed** — `Runner.PendingResumeTokens` bulk-recovery enumeration.
- `Team.Start` / `Team.Resume` (spec 05): the spec's signature
  `NewTeam(name, *hydaelyn.Runner)` contradicts ADR-016 §6
  ("multiagent MUST NOT import runner"). v0.9.0 must resolve the
  layering (likely a runner-side integration, per ADR-017) and amend
  spec 05 before implementing.
- `contract/integration` three-surface kill-resume tests (spec 07
  §"Resume integration tests") — depend on the Team integration above.
- Capability layer: `hydaelyn.self.*` reserved-name constants + 
  registration guard, and exports 1/3/4 (MCP tools, CLI commands,
  provider tool definitions) — spec 02. Export 2 (OpenAPI) was
  deferred in release notes.
- `packs/aiops/incident-triage/` worked demo (spec 16) — only the
  skeleton pack exists.
- `ContextSource` as a `Fetch()` interface + `api.Artifact`/store
  (spec 09) — only a config struct shipped.
- `agent.ToolSafety` / `agent.ToolPolicy` engine integration —
  v0.8.0 declares the policy vocabulary, while concrete side-effect
  gating is enforced through `tool.Definition.RequiresActionTask` /
  `worker.GovernedToolBus`. v0.9.0 owns retry/idempotency semantics
  that directly consume `ToolSafety`.
- `multiagent.Handoff` scheduler flow — the type and store contracts
  exist, but reference schedulers do not yet emit, validate against
  `AgentClass.InputSchema`, or persist Handoffs automatically.
- `FailureKindInsufficientEvidence` automatic remediation — the
  v0.8.0 dispatch table is advisory; no reference Scheduler currently
  launches upstream evidence-gathering work from this failure kind.
- MCP server (expose agents/tools outward; only the client shipped)
  and a streaming surface on the durable Runner.

## Out-of-scope for v0.9.0 (deferred further)

- LLM-as-judge eval assertions
- A/B testing infrastructure
- Hosted observability backends beyond `observe/otel/` exporter
- Domain packs (`packs/research`, `packs/devops`, etc.) beyond their v0.8.0 skeletons
- Framework-shipped storage backend (per ADR-012, never)
- Framework-shipped Memory backend (per ADR-013, never)
- v1.0.0 SemVer commitment (v1.0.0 explicitly)

## Process gate

v0.9.0 design doc writing **does not start** until:

1. v0.8.0 has been tagged.
2. At least one downstream multi-agent application has run end-to-end
   in production for at least 2 weeks against the v0.8.0
   `multiagent/` reference Schedulers.
3. At least one bug or feedback report from real production usage is
   filed against `multiagent.Scheduler`, `agent.Engine`, the typed
   Handoff contract, or one of the three new stores (HandoffStore /
   TeamStateStore / AgentInstanceStore).

Without (3), v0.9.0 would design speculatively against unvalidated
interfaces — the trap v0.8.0 chose to avoid by deferring everything
above to a release that learns from real use.

## Pointer back to v0.8.0

- [v0.8.0 README](../v0.8.0/README.md)
- [v0.8.0 architecture boundaries](../v0.8.0/11-boundaries.md)
- [v0.8.0 multi-agent layer](../v0.8.0/05-multi-agent-layer.md)
