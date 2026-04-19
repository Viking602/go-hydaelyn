# Hydaelyn

Hydaelyn is a multi-agent parallel runtime for Go.

Embed it into your application with `host` to run supervisor-controlled teams, deepsearch-style research flows, and other parallel agent workflows inside a normal Go program.

## Install

```bash
go get github.com/Viking602/go-hydaelyn@latest
```

## Quickstart

Run a multi-agent team without external API keys using a tiny local echo provider:

```go
package main

import (
	"context"
	"fmt"

	"github.com/Viking602/go-hydaelyn/host"
	"github.com/Viking602/go-hydaelyn/patterns/deepsearch"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/team"
)

type echoProvider struct{}

func (echoProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "echo"}
}

func (echoProvider) Stream(_ context.Context, request provider.Request) (provider.Stream, error) {
	last := request.Messages[len(request.Messages)-1]
	return provider.NewSliceStream([]provider.Event{
		{Kind: provider.EventTextDelta, Text: last.Text},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	}), nil
}

func main() {
	runner := host.New(host.Config{})
	runner.RegisterProvider("echo", echoProvider{})
	runner.RegisterPattern(deepsearch.New())
	runner.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "echo", Model: "test"})
	runner.RegisterProfile(team.Profile{Name: "researcher", Role: team.RoleResearcher, Provider: "echo", Model: "test"})
	state, err := runner.StartTeam(context.Background(), host.StartTeamRequest{
		Pattern:           "deepsearch",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"researcher", "researcher"},
		Input: map[string]any{
			"query":      "compare options for a Go research assistant",
			"subqueries": []string{"runtime design", "tool integration"},
		},
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(state.Result.Summary)
}
```

## Core Concepts

Hydaelyn centers on the `deepsearch` pattern: parallel research tasks run simultaneously, optional verification checks their outputs, and a final synthesize task produces the result. The `host` runtime embeds into your application and coordinates supervisor and worker profiles. Supervisors orchestrate the workflow while workers execute tasks. All task outputs publish to a shared blackboard that downstream tasks read explicitly.

## Examples + Read Next

### Examples

- [examples/research](examples/research/main.go) - Local quickstart
- [examples/collab](examples/collab/main.go) - Collaboration pattern
- [examples/tooling](examples/tooling/main.go) - Tool integration
- [examples/approval](examples/approval/main.go) - Approval flows
- [examples/durable](examples/durable/main.go) - Durable execution

### Read Next

- [Quickstart](docs/quickstart.md) - Deep-dive tutorial
- [Extensions](docs/extensions.md) - Stage / Capability / Output / Hook guide
- [Task Dataflow](docs/task-dataflow.md) - Dataflow documentation
- [Recipe Compiler](docs/recipe.md) - Recipe/YAML configuration
- [Evaluation](docs/evaluation.md) - Performance evaluation
- [Durable Execution](docs/durable-execution.md) - Replay and durability

## Where Hydaelyn Fits

Hydaelyn is designed to live inside your Go application. You compose a `host` runtime, register providers, tools, patterns, and profiles, and then run supervisor-led teams in the same process as the rest of your system.

The CLI is useful for inspection and workflow support, but the library is the primary surface. MCP can be plugged in as one integration path, not as the core execution model. V1 stays single-process, and the intended extension model is composition around the runtime rather than subclassing a framework.

## Development

See [CONTRIBUTING.md](CONTRIBUTING.md) for coding standards and contribution guidelines.
