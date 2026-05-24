# 10 — Package Structure

## Goal

Define the canonical package layout for v0.8.0. Distinguish kernel from extension from pack. Make the boundary mechanical (a name on a path), not aspirational.

## Target layout

```
github.com/Viking602/go-hydaelyn/
│
├── doc.go                          # Package-level doc
├── hydaelyn.go                     # Top-level constructor (hydaelyn.New)
├── runner.go                       # Runner struct + ExecuteCommand
├── admin.go                        # Runner methods: registration / config / store ops
├── blackboard.go                   # Runner methods: blackboard
├── governance.go                   # Runner methods: lease / approval / action / trace
├── mailbox.go                      # Runner methods: dispatch / envelope
├── response.go                     # Runner methods: response + message pipeline
├── run.go                          # Runner methods: run lifecycle
├── task.go                         # Runner methods: task CRUD
├── public_api_test.go
├── errors.go
│
├── api/                            # PUBLIC CONTRACTS — the kernel surface
│   ├── doc.go
│   ├── types.go                    # Run, Task, Event, Lease, Flow, AgentProfile, ...
│   ├── store.go                    # All Store interfaces
│   ├── commands.go                 # All Command structs
│   ├── command_names.go
│   ├── policy.go                   # PolicyEngine, PolicyEnforcer, BudgetPolicy
│   ├── output.go                   # OutputGateway
│   ├── pipeline.go                 # PipelineComponents
│   ├── projector.go
│   ├── status.go
│   ├── config.go
│   ├── errors.go
│   ├── capability.go               # NEW: Capability, CapabilityManifest
│   ├── registry.go                 # NEW: Registry interface, AgentSelector, CapabilitySelector
│   ├── memory.go                   # NEW: Memory[T Identified] optional plugin + MemorySelector
│   ├── artifact.go                 # NEW: ArtifactStore, Artifact
│   ├── context.go                  # NEW: ContextScope, ContextSource
│   ├── usage.go                    # NEW: UsageRecord, UsageStore, UsageSelector
│   ├── budget.go                   # NEW: Budget, Quota
│   └── tool.go                     # Tool, ToolInvocation (existing, may add Tool.AsCapability here)
│
├── agent/                          # Agent Engine: single LLM/tool loop
│   ├── agent.go
│   ├── profile.go                  # Re-export of api.AgentProfile for ergonomic imports
│   ├── output_guardrail.go
│   └── …_test.go
│
├── blackboard/                     # Blackboard public types (re-exports)
│   └── blackboard.go
│
├── tool/                           # Tool registration helpers
│   ├── kit/
│   ├── tooltest/
│   ├── tool.go
│   ├── parallel_test.go
│   └── safety_test.go
│
├── policy/                         # Policy presets + Enforcer
│   ├── engine.go
│   ├── enforcer.go                 # NEW: default PolicyEnforcer
│   └── obligations.go              # NEW: built-in obligation implementations
│
├── provider/                       # Model provider drivers
│   ├── doc.go
│   ├── driver.go
│   ├── tooldef.go                  # NEW: ToolDefinitionFromCapability
│   ├── openai/
│   ├── anthropic/
│   ├── scripted/
│   └── errorprovider/
│
├── artifact/                       # NEW: ArtifactStore implementations
│   ├── filesystem/
│   └── inmem/
│
│   # storage/ DELETED 2026-05-24 (ADR-012 revised, Position D). The framework
│   # ships no StoreProvider implementation. Applications implement
│   # api.StoreProvider against their own data stack; see 05-storage.md
│   # and 12-migration-guide.md for the ent-based template. Framework CI
│   # self-tests the contract suite via the non-exported
│   # contract/internal/inmemfake/ adapter.
│
├── worker/                         # Worker Runtime + AgentWorker
│   ├── worker.go                   # AgentWorker (existing)
│   ├── runtime.go                  # NEW: worker.Runtime
│   ├── tools.go
│   ├── deadletter.go               # NEW: DeadLetterSink
│   ├── backoff.go                  # NEW: BackoffStrategy
│   └── worker_test.go
│
├── transport/                      # External interface adapters
│   ├── mcp/                        # MCP server + capability export
│   ├── openapi/                    # NEW: OpenAPI document export
│   ├── webhook/                    # NEW: Webhook trigger listener
│   ├── scheduler/                  # NEW: Schedule trigger driver
│   └── event/                      # NEW: In-process event bus
│
├── observe/                        # NEW: Observability adapters
│   └── otel/                       # OTEL trace + metrics (skeleton in v0.8.0)
│
├── eval/                           # NEW: Evaluation framework
│   ├── types.go
│   ├── runner.go
│   ├── assertions.go
│   ├── dataset.go
│   ├── testing.go
│   └── schema/dataset.json
│
├── recipe/                         # NEW: Public recipe library
│   ├── README.md                   # Index of available recipes
│   ├── memory-pyramid/             # RESERVED v0.9.0+: L0→L3 extraction pipeline (doc 07)
│   │   ├── README.md               # Stub explaining v0.9.0 plan; no Go code in v0.8.0
│   │   └── doc.go                  # Empty package marker
│   └── context-canvas/             # RESERVED v0.9.0+: Mermaid symbolic short-term memory
│       ├── README.md
│       └── doc.go
│
├── contract/                       # NEW: Contract test suites
│   ├── store_provider.go
│   ├── provider.go
│   ├── tool_driver.go
│   ├── policy_engine.go
│   ├── output_gateway.go
│   └── README.md
│
├── packs/                          # NEW: Vertical scenario packs (skeleton)
│   ├── README.md
│   ├── research/                   # Empty skeleton with doc.go
│   ├── support/                    # Empty skeleton with doc.go
│   ├── devops/                     # Empty skeleton with doc.go
│   └── aiops/                      # Empty skeleton with doc.go
│
├── hook/                           # Engine hooks (existing)
│   └── hook.go
│
├── flow/                           # Flow types (existing)
│   └── flow.go
│
├── message/                        # Message types (existing)
│   └── message.go
│
├── cmd/                            # CLI tools
│   └── (existing + capability-cli generator)
│
├── internal/                       # Implementation, not public
│   ├── core/
│   ├── blackboard/
│   ├── mailbox/
│   ├── policy/
│   ├── registry/                   # NEW: in-process Registry impl
│   ├── capability/                 # CapabilityInvoker (ADR-005), stays internal
│   └── ... (existing internal packages)
│
├── examples/                       # NEW: renamed from _examples
│   └── (existing examples, with new ones for v0.8.0 features)
│
├── docs/                           # Documentation
│   ├── product-spec/               # THIS DIRECTORY (organized by version: v0.8.0/, v0.9.0/)
│   ├── architecture-boundaries.md  # NEW: published from product-spec/v0.8.0/09
│   ├── adr/
│   │   ├── ADR-001..008.md         # Existing
│   │   ├── ADR-009-capability-public-api.md            # NEW
│   │   ├── ADR-010-usage-budget-policy-composition.md  # NEW
│   │   ├── ADR-011-context-four-layer-model.md         # NEW
│   │   ├── ADR-012-storage-contract-stability.md       # NEW
│   │   ├── ADR-013-memory-kernel-vs-pipeline.md        # NEW
│   │   └── ADR-014-agent-ontology-stance.md            # NEW
│   ├── release-notes/
│   │   ├── archive/
│   │   │   └── framework-purification.md   # Renamed from old v2.0.md if archived
│   │   └── v0.8.0.md                       # Rewritten for v0.8.0
│   └── (existing docs)
│
├── scripts/
│   ├── check-business-words.sh     # Existing, extended
│   └── check-public-any.sh         # NEW: forbid []any in api/ returns
│
├── .sentrux/
├── .github/
└── (top-level config files)
```

## Three-tier layering (Kernel / Extension / Pack)

This layout makes the three tiers physical:

| Tier | Directories | Stability promise |
|------|-------------|-------------------|
| **Kernel** | `api/`, root package, `agent/`, `blackboard/`, `tool/`, `policy/`, `worker/`, `flow/`, `message/`, `hook/`, `internal/` | Public types in `api/` follow SemVer once v1.0.0 lands |
| **Extension** | `provider/{openai,anthropic,…}`, `memory/`, `artifact/`, `transport/{mcp,openapi,webhook,scheduler,event}`, `observe/otel/`, `eval/`, `contract/`, `recipe/` | Interface stability tracks `api/` (and `contract/` for storage). Per ADR-012 (revised, Position D) the framework ships no `api.StoreProvider` implementation — `storage/` does not exist in the repository. |
| **Pack** | `packs/{research,support,devops,aiops}`, `examples/` | No stability promise; domain logic is free to break |

## Boundary rules (mechanical)

Enforced by `.sentrux/rules.toml` `[[boundaries]]`:

- `kernel` MUST NOT import from `extension/` or `packs/`
- `extension/` MUST NOT import from `packs/`
- `extension/` MAY import from `api/` and other approved kernel paths
- `packs/` MAY import from anywhere
- `examples/` MAY import from anywhere
- `recipe/` is part of extension; same rules as other extension dirs

## Naming conventions

- Packages: lower case, single word where possible
- Transport adapters: `transport/<protocol>/` (e.g., `transport/openapi/`)
- Provider adapters: `provider/<vendor>/` (e.g., `provider/anthropic/`)
- Observability adapters: `observe/<system>/` (e.g., `observe/otel/`)

## Empty package policy

Packages in `packs/` are created with only a `doc.go` and `README.md` for v0.8.0. Their purpose is to claim a path and document expected scope. Content lands in later releases or community contributions.

`observe/otel/` is similar — interface defined, default no-op + skeleton OTEL exporter, full impl in v0.9.0+.

## Migration of moved packages

- `internal/memory/*` stays under `internal/` — it remains the runtime's internal default and is not part of the public API. Earlier v0.8.0 drafts moved it to `storage/memory/`; per ADR-012 (revised, Position D) that move is withdrawn and `storage/` no longer exists.
- **Examples naming (locked)**: `_examples/` stays as-is for internal end-to-end / integration / sandbox material — the leading underscore deliberately excludes it from `go build ./...` and `go test ./...`, keeping CI clean. A new top-level `examples/` directory hosts the **curated public showcase** that compiles as part of `./...` and ships in release notes. Migration rule: nothing moves automatically; an example is **lifted** into `examples/` only after it is reviewed for runnable correctness, dependency hygiene, and documentation quality. The two directories are not synced — `_examples/` may carry experimental or broken-for-now demos, `examples/` must always be green.

## Verification

- `go build ./...` succeeds across the new tree
- `sentrux` boundary rules pass
- `architecture-gate` CI job passes
- Empty packs/ packages each contain a `doc.go` that compiles
