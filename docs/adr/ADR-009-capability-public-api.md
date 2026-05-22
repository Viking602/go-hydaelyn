# ADR-009 Capability Public API — declaration is not execution, execution is not enforcement

## Status

Accepted — enforced from v0.8.0 onward. Anchor document: `docs/product-spec/v0.8.0/02-capability.md`. Supports `docs/product-spec/v0.8.0/09-boundaries.md` Principle 3 ("Capability ≠ Procedure ≠ Policy ≠ Runtime").

## Context

Before v0.8.0, anything an Agent could call existed only as an `api.Tool` — an executable binding. There was no declarative type that separated:

- **What** the thing is (name, schema, side-effect class)
- **How** it runs (code that executes)
- **Whether** it is allowed (PolicyEngine authorization, lease requirement, retry / timeout)

Three problems followed from this collapsing:

1. **No portable export.** MCP, OpenAPI, LLM tool-call APIs, and CLI generators all needed a schema-bearing description, but the only thing available was a Go closure. Each export grew ad-hoc adapter code that re-derived the same fields from `Tool`.
2. **Procedure pollution.** Because `Tool.Description` was the only schema-shaped field exposed to LLMs, teams encoded step-by-step usage instructions there. That conflated *what the thing is* with *how to use it*, which made tools un-reusable across different prompt strategies.
3. **Reserved-name vacuum.** Without a declarative namespace, there was no place to reserve identifiers like `hydaelyn.self.*` for framework-built-in capabilities planned for v0.9.0+. The first user who registered a tool named `hydaelyn.self.profile` would force a rename later.

A declarative layer needed to exist alongside `Tool`, not replace it.

## Decision

Introduce **`api.Capability`** as the declarative schema for anything callable by an Agent. Keep `api.Tool` as the execution binding. Keep `internal/capability` (ADR-005) as the enforcement layer. Three layers, three owners:

```
api.Capability         (declaration:  what)
api.Tool               (execution:    how)
internal/capability    (enforcement:  timeout/retry/permission)
```

### `api.Capability` shape

- Identity: `Name` (recommended convention `provider.action`), `Version`, `Description`.
- Schema: `InputSchema` / `OutputSchema` as `json.RawMessage` so adapters choose their preferred JSON Schema dialect.
- Classification: `EffectType` (reuses existing `ToolEffectType`), `Idempotent` (metadata, framework does not enforce).
- Runtime requirements: `RequiresLease`, `RequiresPolicy` (the runtime rejects bypass attempts).
- Free-form: `Tags []string`, `Metadata map[string]string` (framework ignores `Metadata` except for serialization).

### `api.CapabilityManifest`

A versioned bundle of capabilities exported by one external system or one pack. The unit of consumption for all four export targets.

### `Tool.AsCapability()`

A Tool produces its Capability view via an additive method. Defaults are conservative: `RequiresPolicy=true`, `RequiresLease = Tool.RequiresActionTask`, `Idempotent=false`. Tool authors may publish a richer Capability separately if they have stronger guarantees.

### Four exports, all driven from the same manifest

- **MCP Tool** — `transport/mcp/export.go::ToolsFromCapabilities`
- **OpenAPI Operation** — `transport/openapi/export.go::DocumentFromManifest`
- **CLI Command** — `cmd/capabilitycli/generator.go::CommandsFromManifest`
- **LLM Tool Definition** — `provider/tooldef.go::ToolDefinitionFromCapability`

A single source of truth means an MCP tool, an OpenAPI operation, a CLI subcommand, and an LLM tool definition for the same Capability cannot drift out of sync.

### Reserved namespace `hydaelyn.self.*`

`const HydaelynSelfNamespace = "hydaelyn.self."` plus four name constants (`CapabilityNameSelfProfile`, `CapabilityNameSelfMemoryRead`, `CapabilityNameSelfHistory`, `CapabilityNameSelfSummarizeHistory`). `Registry.RegisterCapability` rejects user registrations under this prefix with `ErrCapabilityNameReserved`. v0.8.0 ships the reservation; v0.9.0+ ships the read-only implementations.

Rationale for reserving without shipping: once production deployments populate Capability allowlists, claiming the namespace later would either collide or force renames. The cost of reserving in v0.8.0 is one constant block plus one registration guard. The benefit is permanent.

Related ADR: ADR-014 records the broader Agent ontology stance that motivates these four names being read-only and framework-provided.

## Anti-patterns rejected by this ADR

Each is named so future PR reviewers have a concrete handle to refuse:

| Anti-pattern | Why it is wrong |
| ------------ | --------------- |
| A Capability whose `Description` encodes step-by-step usage instructions | Conflates declaration with procedure; belongs in a prompt template or recipe README |
| A PolicyEngine that chooses which Capability to call | Conflates policy with procedure; the engine decides allow/deny, not selection |
| A recipe that hardcodes a policy decision (`if user == "admin" then allow`) | Bypasses PolicyEngine entirely; recipes ask the engine, they do not embody it |
| A Tool that decides whether it should run by reading config or env | Bypasses PolicyEngine; the Tool executes and reports, the engine authorizes |
| Storing live LLM context in `Capability.Metadata` | Metadata is for adapter-specific labels, not run-time data — that belongs on Blackboard or in the call args |
| Skipping `Tool.AsCapability()` and hand-writing one per export | Introduces drift; if hand-writing is unavoidable, write the Capability and feed `AsCapability()` lookalike code review |

## Impact

- The four exports (MCP, OpenAPI, CLI, LLM tool def) all consume `CapabilityManifest` — adding a fifth target (e.g., GraphQL schema, gRPC stub) is mechanical.
- v0.9.0 can ship the four `hydaelyn.self.*` capabilities without an API-shape debate: their shape is already fixed by `api.Capability`.
- Code review now has a concrete rule to reject "let's add a `usage` string to the Tool description" patches: such usage belongs in the prompt template or recipe README, not in `Capability.Description`.
- Capability becomes the unit downstream teams build their Agent Directory / Marketplace / Internal Tool Catalog around.

## Amendment 2026-05-22 — `hydaelyn.self.memory.read` is binding-activated

The original Decision section described `CapabilityNameSelfMemoryRead` as a reservation for a framework-built-in capability scheduled to ship in v0.9.0+. ADR-013 (revised) supersedes the storage half of that plan: the framework ships no Memory backend. To keep this ADR consistent with that decision, the semantics of `hydaelyn.self.memory.read` change from **built-in (framework-provided implementation later)** to **binding-activated (application-provided implementation)**:

- When the application registers an `api.Memory[T]` implementation against the runtime, `hydaelyn.self.memory.read` becomes available bound to that registration.
- When no Memory is registered, the capability does not appear in any manifest — no error, no stub, no placeholder. Graceful absence.
- A single runtime may register multiple `Memory[T]` for different `T`. Capability binding distinguishes them via a registration name (`chat_history` vs `user_preferences`, etc.).
- This amendment does **not** add new reserved names (`.memory.write` / `.memory.forget`) under `hydaelyn.self.*`. The `Write` / `Forget` verbs on `Memory[T]` are first-class on the interface, but they are not promoted to reserved capability names — applications expose them as their own capabilities if they want them callable via the Capability surface.

The reservation itself (the constant `CapabilityNameSelfMemoryRead`, the `Registry.RegisterCapability` rejection of user registrations under `hydaelyn.self.`) is unchanged.

## References

- Spec: `docs/product-spec/v0.8.0/02-capability.md` (full type definitions, all four exports, reserved-namespace details)
- Principle anchor: `docs/product-spec/v0.8.0/09-boundaries.md` Principle 3
- Related: ADR-005 (CapabilityInvoker enforcement), ADR-008 (framework vs business), ADR-013 (Memory as Optional Plugin — binding-activated `hydaelyn.self.memory.read`), ADR-014 (Agent ontology stance — explains why `hydaelyn.self.*` is read-only)
