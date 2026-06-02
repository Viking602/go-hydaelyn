# Hydaelyn

[![Go Reference](https://pkg.go.dev/badge/github.com/Viking602/go-hydaelyn.svg)](https://pkg.go.dev/github.com/Viking602/go-hydaelyn)
[![Go Report Card](https://goreportcard.com/badge/github.com/Viking602/go-hydaelyn)](https://goreportcard.com/report/github.com/Viking602/go-hydaelyn)
[![Go Version](https://img.shields.io/github/go-mod/go-version/Viking602/go-hydaelyn)](https://github.com/Viking602/go-hydaelyn/blob/main/go.mod)
[![CI](https://github.com/Viking602/go-hydaelyn/actions/workflows/ci.yml/badge.svg)](https://github.com/Viking602/go-hydaelyn/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/Viking602/go-hydaelyn?sort=semver)](https://github.com/Viking602/go-hydaelyn/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://github.com/Viking602/go-hydaelyn/blob/main/LICENSE)

**A durable, typed, multi-agent framework for Go.** Embed the `Runner` into your
application, drive agents through typed `Run + Task` commands, and get approvals,
audit, crash-resume, and idempotent side effects as first-class primitives —
not glue code you write yourself.

> **Pre-1.0.** The public surface is still moving toward v1.0.

## Why Hydaelyn

Multi-agent systems are easy to prototype and hard to *operate*. The moment an
agent calls a tool with a real side effect, waits on a human approval, or has to
recover after a crash, the orchestration code becomes the liability: lost state,
double-fired actions, no audit trail, and untyped payloads flowing between
agents. Most frameworks are Python-first and dynamically typed, and they bake in
a storage backend and a domain vocabulary you then have to fight.

Hydaelyn takes the opposite stance:

- **Durability is the substrate, not an add-on.** Every multi-agent decision is
  a command that is persisted, leased, audited, and resumable. A crashed process
  picks up where it left off; a side effect fires at most once.
- **Typed all the way to the edges.** The public API forbids `[]any` returns and
  loose `any` fields (enforced in CI), so the compiler — not a runtime panic —
  catches contract drift between agents.
- **Bring your own storage.** Storage is a contract you implement; the kernel
  ships no backend and no domain words. Your infrastructure choices stay yours.
- **A library, not a platform.** No UI, no hosted services, no vendor lock-in.
  You embed the `Runner` in a normal Go program.

## At a glance

- **Strong bounded agent loop** (`agent/`) — one agent does one task well: step
  trace, schema repair, tool safety, context management, typed failure, and
  budget enforcement.
- **Explicit multi-agent scheduler** (`multiagent/`) — first-class
  `AgentClass`, `Team`, `Scheduler`, `Dispatch`, typed `Handoff`, `Blackboard`,
  `Voting`, and `Supervisor`, instead of ad-hoc helpers.
- **Durable runner** (root + `internal/`) — runs, tasks, events, leases,
  approvals, an outbox / action-attempt ledger for idempotent side effects, and
  handoffs, all persisted and replayable.
- **Workflow modeling** (`workflow/`) — a declarative `Definition` compiles to a
  `multiagent` graph; it adds no second runtime.
- **Durable triggers** (`transport/cron`, `transport/webhook`, …) — schedule and
  event entry points, with per-trigger timezone support on the cron driver.
- **Product packs** (`packs/`) — vertical skeletons free to encode domain
  vocabulary the kernel never touches.

## Install

```bash
go get github.com/Viking602/go-hydaelyn@latest
```

Requires Go 1.25+.

## Quickstart

Queue a run on the default in-memory runner and read the append-only event
stream:

```go
package main

import (
	"context"
	"fmt"

	"github.com/Viking602/go-hydaelyn"
	"github.com/Viking602/go-hydaelyn/api"
)

func main() {
	runner := hydaelyn.New()

	run, err := runner.QueueRun(context.Background(), api.StartRunCommand{
		Request: "compare options for a Go research assistant",
	})
	if err != nil {
		panic(err)
	}

	events, err := runner.RunEvents(context.Background(), run.ID)
	if err != nil {
		panic(err)
	}
	fmt.Println(run.ID, len(events))
}
```

Override defaults via `api.Config`:

```go
runner := hydaelyn.New(api.Config{
	PolicyEngine: policy.DenySideEffectsByDefault(),
})
```

Model a multi-step flow declaratively and run it through the multi-agent
scheduler:

```go
def := workflow.New("support-triage").
	Step("intake", multiagent.AgentClass{Name: "intake", Instructions: "classify the request"}).
	Step("reply", multiagent.AgentClass{Name: "reply", Instructions: "draft the response"}).
	Then("intake", "reply").
	Definition()

compiled, err := workflow.Compile(def)
if err != nil {
	panic(err)
}

engine := workflow.Engine{Executor: myExecutor} // a multiagent.Executor you supply
run, err := engine.Start(context.Background(), workflow.StartRequest{
	RunID:    "triage-1",
	Workflow: compiled,
})
```

See [`_examples/workflow`](_examples/workflow/main.go) for the complete,
runnable version.

## How it works

Hydaelyn is four layers. Each depends only on the one below it — there are no
upward imports, and an architecture gate fails CI on a violation.

```
┌─────────────────────────────────────────────┐
│ Packs / Workflow / Examples                 │
│ research, customer-support, devops, aiops,  │
│ workflow modeling                           │
└─────────────────────────────────────────────┘
                    ↓
┌─────────────────────────────────────────────┐
│ Multi-Agent Layer  (multiagent/)            │
│ AgentClass, AgentInstance, Team, Scheduler, │
│ Dispatch, typed Handoff, Blackboard, Voting │
└─────────────────────────────────────────────┘
                    ↓
┌─────────────────────────────────────────────┐
│ Agent Loop Layer  (agent/)                  │
│ Step, OutputPolicy, ToolSafety,             │
│ ContextManager, AgentFailure, LoopPolicy    │
└─────────────────────────────────────────────┘
                    ↓
┌─────────────────────────────────────────────┐
│ Durable Runner  (root + internal/)          │
│ Run / Task / Event / Lease / Approval /     │
│ Outbox / ActionAttempt / Handoff stores     │
└─────────────────────────────────────────────┘
```

The **durable runner** is what makes the upper layers safe to operate. The root
`Runner` façade exposes typed commands for the full lifecycle — `QueueRun` /
`RunEvents`, task leasing (`AcquireTaskExecution`), human approvals
(`RequestApproval` / `DecideApproval` with resume tokens), idempotent side
effects (`StartActionAttempt` / `CompleteActionAttempt`), trace spans, the
blackboard, and the task-envelope mailbox. Storage sits behind a contract
([ADR-012](docs/adr/ADR-012-storage-contract-position-c.md)); the default runner
keeps everything in memory so you can
start without provisioning anything, and you swap in your own backend for
production by implementing the store interfaces.

## Workflow layer

`workflow/` is a **modeling** layer, not a runtime. A `workflow.Definition`
(built fluently with `New().Step().Then().Branch().Map()`) compiles to a
`multiagent.CompiledGraph`:

```
Definition ──Compile──▶ Compiled ──Scheduler()──▶ multiagent.Scheduler
                                └──Graph()──────▶ multiagent.CompiledGraph
```

Because it lowers to the existing graph, workflow execution still flows through
`multiagent.Scheduler` decisions and `multiagent.Dispatch` values. It does **not**
create a second durable runtime and does **not** bypass Runner-owned `Run`,
`Task`, `Event`, `Lease`, `Policy`, or `Outbox` behavior. `Engine` is an
in-process convenience over `multiagent.Drive`; for durable execution, supply a
`multiagent.Executor` that persists each dispatch through the root `Runner`
before running agent work.

Two constraints worth knowing:

- **`Branch` conditions must be pure** functions of `api.TypedReport`. The
  compiled graph may evaluate them during replay or recovery, so they must not
  read clocks, mutate state, call providers, or depend on process-local counters.
- `flow` / `api.Flow` (preset adapter metadata) is a separate concept from
  `workflow/` — same English word, different layer.

## Packages

| Path | Purpose |
|------|---------|
| `hydaelyn` (root) | `Runner` façade — construction, run/task commands, approvals, leases, action attempts, event reads |
| `api/` | Public contracts: `Config`, commands, `Run`, `Task`, store and policy interfaces |
| `agent/` | Strong bounded agent loop: `Engine`, `Step`, `OutputPolicy`, `ToolSafety`, `ContextManager`, `AgentFailure`, `LoopPolicy` |
| `multiagent/` | Multi-agent primitives: `AgentClass`, `Team`, `Scheduler`, `Dispatch`, `Handoff`, `BlackboardEntry`, `Voting`, `Supervisor`, `CompiledGraph` |
| `workflow/` | Declarative `Definition` / `Builder` that compiles to a `multiagent` graph; in-process `Engine`; no second durable runtime |
| `transport/` | Integration transports: `cron` and `webhook` triggers, MCP, SSE, events |
| `provider/` | LLM provider drivers: Anthropic, OpenAI, scripted (testing) |
| `tool/`, `hook/`, `policy/`, `message/` | Tool bus, hook chain, policy engine, message types |
| `memory/` | Optional-plugin `Memory[T]` interface (no backend ships) |
| `worker/` | Optional glue from `TaskEnvelope` execution to `agent.Engine` |
| `packs/` | Skeleton vertical packs: `research`, `customersupport`, `devops`, `aiops` |
| `eval/` | Evaluation framework — `EvalCase` / `Harness` / `Run` / `RunSuite`, typed assertions, matchers, reporters ([docs](docs/evaluation.md)) |
| `contract/` | Storage-contract conformance suite |

## Examples

Examples live under `_examples/` (the leading underscore keeps them out of
`go build ./...`). Run one with `go run ./_examples/<name>`.

| Example | Shows |
|---------|-------|
| [`research`](_examples/research/main.go) | Fan-out by group with work-stealing semantics |
| [`incident_response`](_examples/incident_response/main.go) | Full reference: fan-out → blackboard → review → approval → action |
| [`subagent`](_examples/subagent/main.go) | Agent-as-tool delegation across models/vendors via one `provider.Registry` (no multiagent layer) |
| [`workflow`](_examples/workflow/main.go) | Declarative workflow compiled to a multiagent graph |
| [`panel`](_examples/panel/main.go) | Panel task-board collaboration |
| [`collab`](_examples/collab/main.go) | Collaboration pattern |
| [`orchestrator`](_examples/orchestrator/main.go) | Orchestrator runtime |
| [`tooling`](_examples/tooling/main.go) | Tool integration |
| [`governed_tool`](_examples/governed_tool/main.go) | Governed tool bus |
| [`approval`](_examples/approval/main.go) | Human approval flows |
| [`durable`](_examples/durable/main.go) | Durable execution and replay |
| [`mailbox_pingpong`](_examples/mailbox_pingpong/main.go) | Mailbox primitives |
| [`dataflow`](_examples/dataflow/main.go) | Task dataflow |
| [`evaluation`](_examples/evaluation/main.go) | Evaluation harness |
| [`worker`](_examples/worker/main.go) | Worker bridging to `agent.Engine` |
| [`recipes/`](_examples/recipes/) | YAML recipe walkthroughs (`collab`, `deepsearch`, `panel`, `research`) |

## Documentation

- [Quickstart](docs/quickstart.md) — deep-dive tutorial
- [Public API](docs/public-api.md) — surface reference
- [Workflow](docs/workflow.md) — modeling layer and boundaries
- [Task Dataflow](docs/task-dataflow.md)
- [Durable Execution](docs/durable-execution.md) — replay and resume
- [Orchestrator Runtime](docs/orchestrator-runtime.md)
- [Recipe Compiler](docs/recipe.md) — YAML configuration
- [Extensions](docs/extensions.md) — Stage / Capability / Output / Hook
- [Plugin Development](docs/plugin-development.md)
- [Evaluation](docs/evaluation.md)
- [Migration](docs/migration.md) · [SemVer policy](docs/semver.md)

## Scope & status

By design the kernel ships **no**:

- Built-in storage backend
- Built-in memory backend
- Domain vocabulary
- UI / console
- Hosted observability backends
- Provider-specific deep integrations

These belong in application code or optional plugins. The framework is pre-1.0;
the public surface is still evolving and may change before v1.0.

## Development

See [CONTRIBUTING.md](CONTRIBUTING.md) for coding standards and the
architectural gates CI enforces (`sentrux`, `check-public-any.sh`,
`check-business-words.sh`). The fast local gate is `make verify`; run
`make ci-local` before substantial changes.
