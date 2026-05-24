# ADR-014 Agent Ontology Stance — accept structural identity, reject metaphysical identity

## Status

Accepted — enforced from the v0.8.0 roadmap onward. Anchor documents: `docs/product-spec/v0.8.0/03-agent-profile.md`, `docs/product-spec/v0.8.0/02-capability.md` (reserved namespace), `docs/product-spec/v0.8.0/07-context.md` (self-knowledge convention), `docs/product-spec/v0.8.0/09-boundaries.md` line 119 (ADR-014 mapping).

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

## References

- Design: `docs/product-spec/v0.8.0/03-agent-profile.md` (full Status / PreviousVersionID / RunSelector / AgentVersion specification)
- Reserved namespace: `docs/product-spec/v0.8.0/02-capability.md` §Reserved namespace
- Self-memory convention: `docs/product-spec/v0.8.0/07-context.md` §Self-knowledge convention
- Boundary mapping: `docs/product-spec/v0.8.0/09-boundaries.md` line 119
- v0.9.0 anchor: `docs/product-spec/v0.9.0/README.md`
- Related ADRs: ADR-008 (framework vs business boundary), ADR-001 (Profile vs AgentInstance separation), ADR-013 (Memory kernel vs pipeline)
