# Hydaelyn

[![Go Reference](https://pkg.go.dev/badge/github.com/Viking602/go-hydaelyn.svg)](https://pkg.go.dev/github.com/Viking602/go-hydaelyn)
[![CI](https://github.com/Viking602/go-hydaelyn/actions/workflows/ci.yml/badge.svg)](https://github.com/Viking602/go-hydaelyn/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/Viking602/go-hydaelyn?sort=semver)](https://github.com/Viking602/go-hydaelyn/releases)

Hydaelyn is a Go library for multi-agent workloads that need crash recovery,
human approvals, and idempotent side effects. Its `Runner` records typed `Run`,
`Task`, `Event`, `Lease`, `Approval`, and `ActionAttempt` state through
configurable stores. Applications embed the runner and provide the production
storage implementation.

> **Status:** The latest release is [v0.10.0](https://github.com/Viking602/go-hydaelyn/releases/tag/v0.10.0).
> The public API may still change before v1.0.

## Why Hydaelyn

Agent workloads must preserve state across crashes, prevent duplicate side
effects, and record decisions that cross process or human boundaries. Hydaelyn
handles those concerns in four concrete ways:

- **Recovery:** commands are persisted, leased, audited, and resumable; guarded
  side effects use an action-attempt ledger.
- **Type safety:** CI rejects exported `[]any` results and loose `any` fields in
  the public API.
- **Storage ownership:** applications implement the storage contract; the
  kernel ships no production backend or domain schema.
- **Embedding:** Hydaelyn runs as a library in a normal Go program. It ships no
  UI or hosted service.

## At a glance

- **Strong bounded agent loop** (`agent/`): one agent does one task well: step
  trace, schema repair, tool safety, context management, typed failure, and
  budget enforcement.
  Reusable `skill/` instruction bundles support explicit and model-driven
  activation there; they do not grant host tools or create a second runtime.
- **Explicit multi-agent scheduler** (`multiagent/`): first-class
  `AgentClass`, `Team`, `Scheduler`, `Dispatch`, typed `Handoff`, `Blackboard`,
  `Voting`, and `Supervisor`, instead of ad-hoc helpers.
- **Durable runner** (root + `internal/`): runs, tasks, events, leases,
  approvals, an outbox / action-attempt ledger for idempotent side effects, and
  handoffs, all persisted and replayable.
- **Workflow modeling** (`workflow/`): a declarative `Definition` compiles to a
  `multiagent` graph; it adds no second runtime.
- **Durable triggers** (`transport/cron`, `transport/webhook`, and others): schedule and
  event entry points, with per-trigger timezone support on the cron driver.
- **Product packs** (`packs/`): vertical skeletons free to encode domain
  vocabulary the kernel never touches.

## Install

```bash
go get github.com/Viking602/go-hydaelyn@latest
```

Requires Go 1.25+.
Repository development and release gates pin Go 1.25.12.

## Agent Skills

Hydaelyn supports the complete local Agent Skills lifecycle: trusted directory
discovery, standards-compatible `SKILL.md` parsing, explicit activation,
metadata-only catalog disclosure, model-driven activation, and bounded on-demand
reads from `scripts/`, `references/`, and `assets/`.

```go
found, err := skill.Discover(skill.DiscoveryOptions{
	ProjectDir:   projectRoot,
	TrustProject: true,
})
if err != nil {
	return err
}
skills := skill.NewRegistry()
for _, current := range found.Skills {
	if err := skill.Register(skills, current); err != nil {
		return err
	}
}
engine, err := agent.Build(agent.Spec{
	Model:           "gpt-5",
	Skills:          []string{"always-on-review"},
	AvailableSkills: []string{"pdf-processing"},
}, agent.BuildDeps{Providers: providers, Skills: skills})
```

`Skills` are injected immediately. `AvailableSkills` disclose only names and
descriptions until the model calls the framework-owned activation tool. Project
skills are ignored unless `TrustProject` is true; additional discovery roots are
explicit trust grants. Bundled files are listed without being loaded, and reads
are limited to the captured manifest. Skills never grant host tools and scripts
are never executed automatically: `allowed-tools` remains advisory while
`Spec.Tools`, the tool bus, hooks, and policy remain authoritative.

Run the complete local example with `go run ./_examples/skills`.

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
	runner := hydaelyn.NewDevelopment()

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
runner := hydaelyn.NewDevelopment(api.Config{
	PolicyEngine: policy.DenySideEffectsByDefault(),
})
```

Continue with the [full quickstart](docs/quickstart.md), or run the
[`workflow` example](_examples/workflow/main.go) for a declarative multi-step
flow.

## How it works

Hydaelyn is four layers. Their critical import seams are explicit and checked
in CI: `api/` imports no Hydaelyn package, `agent/` never imports
`multiagent/`, `multiagent/` never imports the root Runner, `worker/`, or
`internal/`, and the root facade never imports `multiagent/`. The shared
`stream/` package is intentionally available to `multiagent/`; it is a
runtime-neutral collaboration primitive, not an upward dependency on Runner.

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
`Runner` façade exposes typed commands for the full lifecycle: `QueueRun` /
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
  `workflow/`, despite sharing the same English word.

## Examples

Examples live under `_examples/` (the leading underscore keeps them out of
`go build ./...`). Run one with `go run ./_examples/<name>`.

- [`incident_response`](_examples/incident_response/main.go): fan-out,
  blackboard, review, approval, and action in one flow.
- [`workflow`](_examples/workflow/main.go): a declarative workflow compiled to
  a multi-agent graph.
- [`subagent`](_examples/subagent/main.go): agent-as-tool delegation across
  models and providers.
- [`evaluation`](_examples/evaluation/main.go): the evaluation harness.

Browse the [_examples directory](_examples/) for the complete set.

## Documentation

- [Quickstart](docs/quickstart.md): install and run the first task.
- [Public API](docs/public-api.md): exported contracts and stability.
- [Documentation index](docs/index.md): runtime concepts, extensions,
  compatibility, architecture, and the package map.
- [Contributing](CONTRIBUTING.md): development workflow and verification gates.

## Scope & status

By design the kernel ships **no**:

- Built-in storage backend
- Built-in memory backend
- Domain vocabulary
- UI / console
- Hosted observability backends
- Provider-specific deep integrations

These belong in application code or optional plugins.

## Development

See [CONTRIBUTING.md](CONTRIBUTING.md) for coding standards and the
architectural gates CI enforces (`sentrux`, `check-public-any.sh`,
`check-business-words.sh`, `check-import-boundaries.sh`). The fast local gate is
`make verify`; run `make ci-local` before substantial changes.
