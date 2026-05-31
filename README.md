# Hydaelyn

[![Go Reference](https://pkg.go.dev/badge/github.com/Viking602/go-hydaelyn.svg)](https://pkg.go.dev/github.com/Viking602/go-hydaelyn)
[![Go Report Card](https://goreportcard.com/badge/github.com/Viking602/go-hydaelyn)](https://goreportcard.com/report/github.com/Viking602/go-hydaelyn)
[![Go Version](https://img.shields.io/github/go-mod/go-version/Viking602/go-hydaelyn)](https://github.com/Viking602/go-hydaelyn/blob/main/go.mod)
[![CI](https://github.com/Viking602/go-hydaelyn/actions/workflows/ci.yml/badge.svg)](https://github.com/Viking602/go-hydaelyn/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/Viking602/go-hydaelyn?sort=semver)](https://github.com/Viking602/go-hydaelyn/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://github.com/Viking602/go-hydaelyn/blob/main/LICENSE)

**Hydaelyn is a durable typed multi-agent framework for Go.**

It ships a strong but bounded single-agent loop, an explicit role/class
based multi-agent scheduler, and durable execution primitives for
approvals, audit, resume, and idempotent side effects. Embed the root
`Runner` into your Go application and drive work through typed
`Run + Task` commands.

> **Pre-1.0 status.** The public surface is still evolving toward v1.0.

## Install

```bash
go get github.com/Viking602/go-hydaelyn@latest
```

## Quickstart

Queue a run on the default in-memory runner and inspect the append-only
event stream:

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

Override the defaults via `api.Config`:

```go
runner := hydaelyn.New(api.Config{
	PolicyEngine: policy.DenySideEffectsByDefault(),
})
```

## Architecture

Hydaelyn is organized as four layers. Each layer depends only on the
one below it — no upward imports.

```
┌─────────────────────────────────────────────┐
│ Packs / Examples                            │
│ research, customer-support, devops, aiops   │
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

- **Strong bounded Agent Loop** (`agent/`) — one agent does one task
  well. Step trace, schema repair, tool safety, context management,
  typed failure, budget enforcement.
- **Explicit Multi-Agent Scheduler** (`multiagent/`) — named first-class
  primitives for team coordination instead of ad-hoc Pack helpers.
- **Durable Runner** (root + `internal/`) — persists, leases, audits,
  and resumes every multi-agent decision. Storage is a contract; the
  framework ships no built-in backend.
- **Product Packs** (`packs/`, `_examples/`) — vertical scenarios. Free
  to encode domain vocabulary; the kernel never does.
- **Workflow Modeling** (`workflow/`) — a user-facing modeling layer that
  sits with Packs, not below `multiagent/`. A `workflow.Definition` compiles
  to `multiagent.Graph` and runs through `multiagent.Scheduler` decisions; it
  adds no second durable runtime and depends on existing Runner durability.
  `flow` / `api.Flow` remains preset adapter metadata, distinct from
  `workflow/`.

## Packages

| Path | Purpose |
|------|---------|
| `hydaelyn` (root) | `Runner` façade — construction, run/task commands, event reads |
| `api/` | Public contracts: `Config`, commands, `Task`, `Run`, interfaces |
| `agent/` | Strong bounded agent loop: `Engine`, `Step`, `OutputPolicy`, `ToolSafety`, `ContextManager`, `AgentFailure`, `LoopPolicy` |
| `multiagent/` | Multi-agent primitives: `AgentClass`, `AgentInstance`, `Team`, `Scheduler`, `Dispatch`, `Handoff`, `BlackboardEntry`, `Voting`, `Supervisor` |
| `workflow/` | User-facing workflow definitions that compile to `multiagent.Graph` / `multiagent.Scheduler`; no second durable runtime |
| `memory/` | Optional-plugin `Memory[T]` interface (no backend ships) |
| `worker/` | Optional glue from `TaskEnvelope` execution to `agent.Engine` |
| `packs/` | Skeleton vertical packs: `research`, `customersupport`, `devops`, `aiops` |
| `provider/` | LLM provider adapters: Anthropic, OpenAI, scripted (testing) |
| `tool/`, `hook/`, `policy/`, `message/` | Tool bus, hook chain, policy engine, message types |
| `eval/`, `contract/` | Evaluation framework and storage contract suite |

## Examples

Examples live under `_examples/` (leading underscore skips them from
`go build ./...`). Run one with `go run ./_examples/<name>`.

- [`research`](_examples/research/main.go) — fan-out by group; work-stealing semantics
- [`incident_response`](_examples/incident_response/main.go) — full multi-agent reference (fan-out → blackboard → review → approval → action)
- [`panel`](_examples/panel/main.go) — panel task-board collaboration
- [`collab`](_examples/collab/main.go) — collaboration pattern
- [`orchestrator`](_examples/orchestrator/main.go) — orchestrator runtime
- [`tooling`](_examples/tooling/main.go) — tool integration
- [`approval`](_examples/approval/main.go) — approval flows
- [`durable`](_examples/durable/main.go) — durable execution + replay
- [`governed_tool`](_examples/governed_tool/main.go) — governed tool bus
- [`mailbox_pingpong`](_examples/mailbox_pingpong/main.go) — mailbox primitives
- [`dataflow`](_examples/dataflow/main.go) — task dataflow
- [`evaluation`](_examples/evaluation/main.go) — evaluation harness
- [`recipes/`](_examples/recipes/) — recipe / YAML configuration walkthroughs (`collab`, `deepsearch`, `panel`, `research`)
- [`worker`](_examples/worker/main.go) — worker bridging to `agent.Engine`

## Documentation

- [Quickstart](docs/quickstart.md) — deep-dive tutorial
- [Public API](docs/public-api.md) — surface reference
- [Task Dataflow](docs/task-dataflow.md)
- [Durable Execution](docs/durable-execution.md) — replay and resume
- [Evaluation](docs/evaluation.md)
- [Recipe Compiler](docs/recipe.md) — YAML configuration
- [Extensions](docs/extensions.md) — Stage / Capability / Output / Hook
- [Plugin Development](docs/plugin-development.md)

## What's out of scope

By design the framework ships no:

- Built-in storage backend
- Built-in memory backend
- Domain vocabulary in the kernel
- UI / Console
- Hosted observability backends
- Provider-specific deep integrations

These belong in application code or optional plugins.

## Development

See [CONTRIBUTING.md](CONTRIBUTING.md) for coding standards, the
architectural gates that CI enforces (`sentrux`, `check-public-any.sh`,
`check-business-words.sh`), and contribution guidelines.
