# Hydaelyn

[![Go Reference](https://pkg.go.dev/badge/github.com/Viking602/go-hydaelyn.svg)](https://pkg.go.dev/github.com/Viking602/go-hydaelyn)
[![Go Report Card](https://goreportcard.com/badge/github.com/Viking602/go-hydaelyn)](https://goreportcard.com/report/github.com/Viking602/go-hydaelyn)
[![Go Version](https://img.shields.io/github/go-mod/go-version/Viking602/go-hydaelyn)](https://github.com/Viking602/go-hydaelyn/blob/main/go.mod)
[![CI](https://github.com/Viking602/go-hydaelyn/actions/workflows/ci.yml/badge.svg)](https://github.com/Viking602/go-hydaelyn/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/Viking602/go-hydaelyn?sort=semver)](https://github.com/Viking602/go-hydaelyn/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://github.com/Viking602/go-hydaelyn/blob/main/LICENSE)
[![Module](https://img.shields.io/badge/module-github.com%2FViking602%2Fgo--hydaelyn-007d9c?logo=go)](https://pkg.go.dev/github.com/Viking602/go-hydaelyn)

Hydaelyn is a Run/Task orchestrator for Go.

Embed it into your application with `hydaelyn` to run durable orchestrator
workflows where every state change goes through Run/Task commands, policy,
leases, typed reports, handoff, response outbox, and replay.

## Install

```bash
go get github.com/Viking602/go-hydaelyn@latest
```

## Quickstart

Start a run, let the orchestrator create the root task, and inspect the
append-only event stream:

```go
package main

import (
	"context"
	"fmt"

	"github.com/Viking602/go-hydaelyn"
)

func main() {
	runner := hydaelyn.New(hydaelyn.Config{})
	run, err := runner.QueueRun(context.Background(), hydaelyn.StartRunCommand{
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

## Core Concepts

Hydaelyn centers on embeddable orchestrator primitives. New work should model
execution as `Run + Task + TaskExecutionLease`. Flow and pattern adapters are
presets only; they must not bypass `TaskStore`, `PolicyEngine`,
`TaskExecutionLease`, handoff, `ResponseLayer`, or `OutputGateway`.

Minimal run-level orchestration:

```go
rt := hydaelyn.New(hydaelyn.Config{})
run, err := rt.QueueRun(context.Background(), hydaelyn.StartRunCommand{
	Request: "coordinate a multi-agent run",
})
if err != nil {
	panic(err)
}
events, _ := rt.RunEvents(context.Background(), run.ID)
fmt.Println(len(events))
```

Legacy Team + Pattern code remains available during the migration window:

```go
teamRunner := hydaelyn.NewTeamRuntime(hydaelyn.TeamConfig{})
```

Direct imports for the old runtime now live under `legacy/`, for example
`github.com/Viking602/go-hydaelyn/legacy/host` and
`github.com/Viking602/go-hydaelyn/legacy/pattern/deepsearch`.

## Examples + Read Next

### Examples

- [_examples/research](_examples/research/main.go) - Local quickstart
- [_examples/panel](_examples/panel/main.go) - Panel task-board collaboration
- [_examples/collab](_examples/collab/main.go) - Collaboration pattern
- [_examples/tooling](_examples/tooling/main.go) - Tool integration
- [_examples/approval](_examples/approval/main.go) - Approval flows
- [_examples/durable](_examples/durable/main.go) - Durable execution

Examples live under `_examples/` (leading underscore) so they are skipped
by `go build ./...`. Build or run one explicitly with `go run ./_examples/research/`.

### Read Next

- [Quickstart](docs/quickstart.md) - Deep-dive tutorial
- [Extensions](docs/extensions.md) - Stage / Capability / Output / Hook guide
- [Task Dataflow](docs/task-dataflow.md) - Dataflow documentation
- [Recipe Compiler](docs/recipe.md) - Recipe/YAML configuration
- [Evaluation](docs/evaluation.md) - Performance evaluation
- [Durable Execution](docs/durable-execution.md) - Replay and durability

## Where Hydaelyn Fits

Hydaelyn is designed to live inside your Go application. Compose the
Orchestrator runtime, register stable provider/tool/policy/flow contracts, and
drive work through Run/Task commands. Legacy host/team/pattern code remains
available only through `legacy/` compatibility packages.

The CLI is useful for inspection and workflow support, but the library is the primary surface. MCP can be plugged in as one integration path, not as the core execution model. V1 stays single-process, and the intended extension model is composition around the runtime rather than subclassing a framework.

## Development

See [CONTRIBUTING.md](CONTRIBUTING.md) for coding standards and contribution guidelines.
