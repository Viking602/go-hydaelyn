# ADR-014 Agent Ontology Stance — accept structural identity, reject metaphysical identity

## Status

Accepted — enforced from the v0.8.0 roadmap onward. Anchor documents: `docs/product-spec/v0.8.0/04-agent-class.md` (renamed from 03-agent-profile in the v0.8.0 reconstruction), `docs/product-spec/v0.8.0/02-capability.md` (reserved namespace), `docs/product-spec/v0.8.0/09-context.md` (self-knowledge convention), `docs/product-spec/v0.8.0/11-boundaries.md` (ADR-014 mapping).

**Revised 2026-05-24:** the AgentInstance deferral recorded in the original §1 table is withdrawn. With ADR-016 establishing `multiagent/` as a first-class kernel package, AgentInstance becomes a structural-identity concept that lands in v0.8.0. The metaphysical-identity red lines in §2 remain unchanged. See *Revised decision* at the bottom of this ADR.

## Context

During v0.8.0 design a recurring cluster of feature requests surfaced around "does an Agent have a self":

- Should there be an `Agent.Self` type that represents "who I am"?
- Should an Agent be able to call an `UpdateOwnProfile` tool to modify its own definition?
- Should the framework auto-derive personality / style / preferences from past Run history?
- Should long-term memory carry "continuity of self" semantics?

The product motivations behind these requests (Agent Directory, personalized assistant, multi-version canary rollout) are legitimate. But putting them in the kernel introduces:

1. **Domain-vocabulary pollution.** Words like `persona`, `personality`, `self`, `mood`, `preference` in `api/` tax every downstream team that does not do role-playing. ADR-008 explicitly forbids this.
2. **Unauditable write paths.** `UpdateOwnProfile` lets an Agent mutate its own definition inside a Run — this breaks Principle 4 (all side effects auditable) and breaks replay determinism (next replay sees a changed profile).
3. **Metaphysical ambiguity.** Auto-derived personality depends on subjective LLM extraction; the framework has no way to write contract tests against it.
4. **Diffuse responsibility.** If the framework claims to provide "the self", every inconsistency in Agent behavior turns into a framework support ticket.

At the same time, **structural identity** is genuinely necessary. Without it the four `hydaelyn.self.*` capabilities planned for v0.9.0, canary rollout, version traceability, and run-attribution queries are all impossible.

The two need a sharp boundary.

## Decision

Two red lines, neither of which may be crossed.

### 1. Accepted (lands in the kernel in v0.8.0): **structural identity**

Agent metadata that is falsifiable, enumerable, and contract-testable:

| Capability | Form | Spec anchor |
| ---------- | ---- | ----------- |
| Agent lifecycle state | `AgentProfile.Status AgentStatus` (`Active` / `Draft` / `Paused` / `Retired`; empty value is v0.7-compatible Active) | 03-agent-profile.md §AgentStatus |
| Version lineage | `AgentProfile.PreviousVersionID string` | 03-agent-profile.md L33-38 |
| Run attribution | `RunSelector{ AgentID, AgentVersion, Statuses, Since, Until, Limit }` + `RunStore.ListRuns` | 03-agent-profile.md §RunSelector / 05-storage.md §RunSelector |
| Version stamp on Run | `Run.AgentVersion` (stamped by Runner at start) | 03-agent-profile.md |
| Self-knowledge entry point (reserved) | `hydaelyn.self.*` Capability namespace, four reserved names | 02-capability.md §Reserved namespace |
| Self-memory convention | Application's `T` satisfies `Identified` with `Scope() == ContextScopeAgent` and `SubjectID() == <AgentID>`. Runner may resolve `"self"` shorthand to the live AgentID before the call reaches the registered `Memory[T]`. | 13-memory-optional-plugin.md; ADR-013 (revised) |

Enforced rules:

- `RunFromProfile` rejects any profile where `Status != Active` with a typed error (`ErrAgentNotActive`).
- `Registry.RegisterCapability` rejects names that begin with `HydaelynSelfNamespace = "hydaelyn.self."` (except from a designated internal package). Error type: `ErrCapabilityNameReserved`.
- `PreviousVersionID` forms a traversable version chain. It is **only a lineage pointer** — no merge / conflict / inheritance semantics.
- Self-memory substitution happens at the Runner layer, not by introducing a new `Scope` value. This way the Memory store always sees a concrete AgentID and replay determinism is preserved.

### 2. Rejected (does NOT enter the kernel in v0.8.0 or v0.9.0): **metaphysical identity**

Constructs that are not falsifiable, depend on subjective LLM extraction, or break audit / replay:

| Anti-pattern | Reason for rejection |
| ------------ | -------------------- |
| `Agent.Self` type | Treats "the self" as a runtime entity but is not enumerable or contract-testable; this is a Pack concept |
| `UpdateOwnProfile` tool | Breaks Principle 4 (side-effect auditability) and replay determinism; profile changes must be explicit Registry operations |
| Auto-derived personality / persona / preference | LLM extraction cannot be contract-tested; belongs in Packs |
| `mood` / `style` / `tone` fields in the kernel | Domain vocabulary; user code can put these in `AgentProfile.Metadata` |
| `AgentInstance.Lifecycle{ Birth, Death, Reincarnate }` and similar metaphors | Metaphysical ambiguity; belongs in Packs |
| Auto-write Memory entries describing "what kind of self I am" | The framework cannot decide for an Agent what constitutes "I"; writes must come from explicit business logic |

### 3. Immediately-effective hard constraints

- `api/`, `internal/core/**`, `agent/**`, `internal/registry/**`, `internal/capability/**`, `internal/memory/**`, `internal/run/**`, `internal/task/**`, and `internal/blackboard/**` MUST NOT introduce new identifiers, constants, struct fields, or struct tags with these names:

  ```
  Self (as a noun for selfhood)
  Persona / Personality
  Identity (when used to mean "selfhood")
  Mood / Style / Tone / Preference (as Agent attributes)
  Reincarnate / Birth / Death (as lifecycle verbs/nouns)
  ```

  Permitted locations: `packs/`, `recipe/`, `_examples/`, `examples/`, `docs/`, and issue / PR discussion text.

- `Registry.RegisterCapability` and `Capability.Register` MUST contain a rejection branch for `HydaelynSelfNamespace`, backed by the `TestRegistry_RejectsReservedSelfNamespace` contract test (see 02-capability.md §Tests).

- `AgentProfileStore` MUST NOT provide a "patch profile from running context" API. The only write path is explicit Registry registration / upgrade.

- When v0.9.0 implements the four `hydaelyn.self.*` capabilities, all four MUST be `ToolEffectRead`. No implementation may write `AgentProfile` or auto-write `Memory` entries.

### 4. In-scope exceptions

- `packs/{research,support,devops,aiops}` MAY use words like `persona`, `style`, and `tone` in their own domain language, **provided they do not flow back into `api.Capability`'s standard fields**. Such concepts may live in `Capability.Metadata`, in pack-defined types, or in prompt templates.
- `_examples/` and `examples/` follow the same rule as packs.
- `recipe/` packages MAY describe persona-driven behavior in their README but MUST NOT export persona-typed identifiers.

## Impact

- **All v0.8.0 schema fields are locked**: `Status`, `PreviousVersionID`, `RunSelector`, `Run.AgentVersion`, `HydaelynSelfNamespace` + four name constants, `ErrCapabilityNameReserved`, and the self-memory convention (`Identified.Scope() == ContextScopeAgent` with `Identified.SubjectID() == <AgentID>`, see ADR-013).
- **v0.9.0 deliverable is anchored**: implement the four `hydaelyn.self.*` capabilities without changing the v0.8.0 schema. Registered in `docs/product-spec/v0.9.0/README.md`.
- **Any PR may cite this ADR** as the basis for rejecting requests to add Agent selfhood, auto-derive personality, expose `UpdateOwnProfile`, or introduce persona fields into `api/`.
- **Downstream expectation management.** Teams building persona-driven Agents do so via packs/ + `Capability.Metadata` + explicit Memory writes. The framework provides primitives, not concepts.

## Revised decision (2026-05-24) — AgentInstance accepted as structural identity

The original §1 table marked AgentInstance as deferred ("If a multi-instance-per-profile need surfaces, it is layered in v0.9.0 or later"). The v0.8.0 reconstruction (master spec `docs/superpowers/specs/2026-05-24-agent-layer-business-stance.md` §4) and ADR-016 establish `multiagent/` as a first-class kernel package whose Scheduler routinely spawns multiple instances of the same AgentClass within a single Run — for example two `ForensicsAgent` instances investigating two evidence branches in parallel.

Under the original framing this need would be re-implemented per Pack with no shared abstraction. That is incoherent with the framework positioning as a durable typed multi-agent framework. Therefore:

- **AgentInstance is accepted** as a v0.8.0 structural-identity concept, living in `multiagent/instance.go`:

  ```go
  type AgentInstance struct {
      ID    AgentID
      Class AgentClass
      RunID RunID
      State AgentState
  }
  ```

- **AgentInstance.ID is deterministic** from `(RunID, Class.Name, spawn-sequence)`. Replay reconstructs the same instance IDs from EventStore. This preserves Principle 5 (long-running work resumable) and ADR-007 (EventStore replay determinism).

- **AgentClass owns declaration; AgentInstance owns execution-time state.** AgentClass is reused across runs; AgentInstance is run-local.

- **The metaphysical red lines in §2 are unchanged.** No `Self` type, no `UpdateOwnProfile`, no auto-derived personality, no lifecycle metaphors (Birth/Death/Reincarnate). An AgentInstance is a run-scoped execution context — not "the self that emerged this run."

- **AgentProfile (in `api/types.go`) remains as the declarative identity.** AgentClass (in `multiagent/`) is the Scheduler-facing form. The two coexist; Packs may use either or both. AgentProfile.ID matches AgentClass.Name when a Pack wires them together.

- **`AgentInstanceStore` is added to `UnitOfWork`** (per ADR-017 §5). Per ADR-012 Position D the framework ships no implementation.

Why this is safe under the original §2 red lines: AgentInstance is **structural** (deterministic, enumerable, contract-testable), not metaphysical. It carries `ID`, `Class`, `RunID`, `State` — all falsifiable. It carries no persona, mood, or auto-derived behavior. Schedulers reason about AgentInstance for routing decisions, not for "who this agent is."

## References

- Master spec: `docs/superpowers/specs/2026-05-24-agent-layer-business-stance.md` §4
- Design: `docs/product-spec/v0.8.0/04-agent-class.md` (renamed from 03-agent-profile)
- Multi-agent layer: `docs/product-spec/v0.8.0/05-multi-agent-layer.md`
- Reserved namespace: `docs/product-spec/v0.8.0/02-capability.md` §Reserved namespace
- Self-memory convention: `docs/product-spec/v0.8.0/09-context.md` §Self-knowledge convention
- Boundary mapping: `docs/product-spec/v0.8.0/11-boundaries.md`
- v0.9.0 anchor: `docs/product-spec/v0.9.0/README.md`
- Related ADRs: ADR-008 (framework vs business boundary), ADR-001 (Profile vs AgentInstance separation), ADR-013 (Memory kernel vs pipeline), ADR-015 (Strong Bounded Agent Loop), ADR-016 (Explicit Multi-Agent Scheduler), ADR-017 (Durable Runner Boundary)
