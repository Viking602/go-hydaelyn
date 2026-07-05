# 12 — Package Structure

> Renumbered from `10-package-structure.md`. v0.8.0 adds the
> `multiagent/` top-level package and expands `agent/` to carry the
> Strong Bounded Loop primitives (Step, OutputPolicy, Result,
> ToolSafety, ContextManager, AgentFailure, LoopPolicy, StepPolicy).

## Goal

A package layout that makes the four-layer architecture visible at
`tree -L 2` and prevents the kernel from accumulating domain code.

## Target tree

```
github.com/Viking602/go-hydaelyn/
├── api/                          # Public types only — no logic
│   ├── types.go
│   ├── capability.go             # NEW (02-capability.md)
│   ├── errors.go
│   ├── commands.go
│   └── selectors.go
│
├── agent/                        # Strong Bounded Agent Loop (03-agent-loop.md)
│   ├── engine.go                 # agent.Engine
│   ├── step.go                   # Step, StepDecision, StepPolicy
│   ├── output_policy.go          # OutputPolicy + repair loop
│   ├── result.go                 # agent.Result
│   ├── tool_safety.go            # ToolSafety, ToolPolicy
│   ├── context_manager.go        # ContextManager interface
│   ├── failure.go                # AgentFailure, FailureKind
│   ├── loop_policy.go            # LoopPolicy
│   └── registry.go               # AgentProfile registry surface (04-agent-class.md)
│
├── skill/                        # Agent Skills parser/registry; no runtime
│   └── skill.go                  # SKILL.md parsing, registry, system rendering
│
├── multiagent/                   # NEW — Multi-Agent Layer (05-multi-agent-layer.md)
│   ├── class.go                  # AgentClass
│   ├── instance.go               # AgentInstance + ComputeInstanceID
│   ├── team.go                   # Team, NewTeam, AddRole, UseScheduler, Start, Resume
│   ├── scheduler.go              # Scheduler interface
│   ├── sequential.go             # SequentialScheduler (reference impl)
│   ├── router.go                 # RouterScheduler (reference impl)
│   ├── supervisor.go             # SupervisorScheduler (reference impl) + SupervisorDecision
│   ├── dispatch.go               # Dispatch
│   ├── handoff.go                # Handoff (typed, schema-validated)
│   ├── blackboard.go             # Multi-agent BlackboardEntry helpers
│   ├── voting.go                 # VotingResult, MajorityVote, QuorumVote
│   ├── team_state.go             # TeamState
│   └── events.go                 # multi-agent event kinds
│
├── memory/                       # Memory interface only (15-memory-optional-plugin.md)
│   └── memory.go                 # Memory[T Identified], Query
│
├── artifact/                     # Artifact type + Store + filesystem ref impl
│   ├── artifact.go
│   ├── store.go
│   └── filesystem/
│
├── eval/                         # Evaluation framework (10-evaluation.md)
│   ├── case.go
│   ├── runner.go
│   ├── result.go
│   ├── assertions/
│   ├── matcher/
│   └── reporter/
│
├── recipe/                       # Recipe / pattern surface (compiles to Run/Task)
│   └── ...
│
├── flow/                         # Thin alias for api.Flow preset metadata
│   └── flow.go
│
├── workflow/                     # User-facing workflow definitions compiled to multiagent.Graph
│   ├── definition.go
│   ├── compiler.go
│   ├── engine.go
│   └── doc.go
│
├── contract/                     # Storage conformance suite (07-storage.md)
│   ├── README.md
│   ├── suite.go
│   ├── runs/
│   ├── tasks/
│   ├── events/
│   ├── leases/
│   ├── outbox/
│   ├── idempotency/
│   ├── resumetokens/
│   ├── approvals/
│   ├── blackboard/
│   ├── mailboxes/
│   ├── traces/
│   ├── usage/
│   ├── deadletters/
│   ├── schedules/
│   ├── webhooks/
│   ├── handoffs/                 # NEW
│   ├── teamstates/               # NEW
│   ├── agentinstances/           # NEW
│   ├── integration/              # NEW (three-surface resume tests)
│   └── internal/inmemfake/       # Framework-internal — not importable
│
├── packs/                        # Vertical scenarios — domain vocabulary allowed
│   ├── research/                 # doc.go + README
│   ├── support/                  # doc.go + README
│   ├── devops/                   # doc.go + README
│   └── aiops/
│       ├── doc.go
│       ├── README.md
│       └── incident-triage/      # Full demo (16-multi-agent-demo.md)
│
├── transport/                    # Outbound surface adapters
│   ├── mcp/
│   ├── openapi/                  # NEW (02-capability.md export)
│   ├── webhook/                  # NEW (Outbox FIFO consumer)
│   ├── cron/                     # NEW (cron triggers for api.TriggerSchedule)
│   ├── scheduler/                # Deprecated compatibility shim for transport/cron
│   └── event/                    # NEW (event bus consumers)
│
├── observe/                      # Observability adapters
│   └── otel/                     # OpenTelemetry skeleton — v0.9.0 fleshes out
│
├── provider/                     # LLM provider adapters
│   ├── provider.go
│   ├── scripted/                 # For tests; deterministic
│   ├── openai/                   # Optional
│   ├── anthropic/                # Optional
│   └── google/                   # Optional
│
├── internal/                     # Implementation — no external imports
│   ├── core/
│   ├── run/
│   ├── task/
│   ├── event/
│   ├── lease/
│   ├── approval/
│   ├── outbox/
│   ├── idempotency/
│   ├── blackboard/
│   ├── mailbox/
│   ├── policy/
│   ├── tool/
│   ├── capability/               # Execution-time governance (ADR-005)
│   ├── governance/
│   ├── handoff/
│   ├── provider/
│   ├── memory/                   # Stays internal; framework provides no backend
│   ├── multiagent/               # Internal helpers backing multiagent/
│   ├── trace/
│   └── command/
│
├── runner.go                     # Public Runner facade
├── errors.go
├── doc.go
│
├── _examples/                    # Runnable demos
│   ├── single-agent-research/
│   ├── multi-agent-incident-triage/  # links to packs/aiops/incident-triage
│   └── ...
│
├── docs/
│   ├── product-spec/v0.8.0/      # This release's spec
│   ├── adr/                      # ADR-001..017
│   ├── superpowers/specs/        # Master spec(s)
│   ├── architecture-boundaries.md
│   └── ...
│
├── scripts/
│   ├── check-business-words.sh
│   ├── check-public-any.sh       # NEW
│   ├── check-import-boundaries.sh
│   └── ...
│
├── .sentrux.toml                 # Architectural boundaries
├── .golangci.yml
├── CONTRIBUTING.md
├── README.md
└── go.mod
```

## Dependency rules

```
packs/         → multiagent/, agent/, api/, memory/, artifact/, eval/, recipe/
multiagent/    → agent/, api/                                             (one-way)
agent/         → api/, skill/, memory/, artifact/                         (never multiagent/)
skill/         → stdlib + YAML parser only                                (no Hydaelyn deps)
eval/          → api/, agent/, multiagent/                                (test consumer)
recipe/        → api/                                                     (compiles to commands)
contract/      → api/                                                     (no impl deps)
transport/*    → api/                                                     (publishing only)
provider/*     → api/                                                     (LLM only)
observe/*      → api/                                                     (read-only)

internal/      → api/, internal/*                                         (no external)
api/           → (nothing — pure types)
memory/        → api/                                                     (interface only)
artifact/      → api/                                                     (interface + ref impl)

runner.go      → api/, internal/core, internal/run, internal/task, ...
```

Enforced by `.sentrux.toml`. The critical rules:

- `agent/` MUST NOT import `multiagent/`
- `multiagent/` MUST NOT import any `internal/` package
- runner facade (`runner.go`) MUST NOT import `multiagent/` (one-way: multi-agent calls down to runner via api/, not up)
- `api/` MUST NOT import anything from this module

## Package-level invariants

| Package | Invariant |
|---------|-----------|
| `api/` | Only types, errors, command definitions, selectors. No logic, no imports of this module. |
| `agent/` | Strong bounded loop. Owns Step trace, OutputPolicy, ToolSafety, ContextManager, AgentFailure, LoopPolicy. NEVER imports `multiagent/`. |
| `skill/` | Parses and renders reusable instruction bundles. No filesystem auto-discovery inside runner; no tool authorization. |
| `multiagent/` | Multi-agent primitives. Imports `agent/` + `api/` only. NEVER imports any `internal/`. |
| `memory/` | Verbs only. No backend. |
| `artifact/` | Interface + filesystem ref impl. Production storage in app code. |
| `eval/` | Test harness. Imports `api/`, `agent/`, `multiagent/`. |
| `contract/` | Storage conformance suite. Imports `api/` only. `internal/inmemfake/` is the self-check fake; not exported. |
| `packs/*` | Domain code allowed. Calls down through `multiagent/` / `agent/` / `api/`. |
| `transport/*`, `provider/*`, `observe/*` | Adapters. Each imports `api/` only. |
| `internal/*` | All implementation. No external imports except `api/` and other internals. |

## Files / packages NOT shipping in v0.8.0

- `storage/` — by Position D, no top-level storage package
- Any built-in Memory backend
- Any LLM provider beyond `provider/scripted/` (the others may exist as separate go-modules)
- `observe/otel/` exporter — only the package skeleton + a no-op
- v1.0.0 SemVer commitment

## Verification

- `sentrux check` passes against `.sentrux.toml`
- `scripts/check-import-boundaries.sh` passes
- `scripts/check-business-words.sh` passes (with multi-agent exception list)
- `scripts/check-public-any.sh` passes
- `go build ./...` succeeds with the new top-level packages
- `go vet ./...` clean
- `go doc ./...` returns sensible package-level docs for every new top-level package
