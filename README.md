# Hydaelyn

Hydaelyn is a multi-agent parallel runtime for Go.

It embeds into your application to run supervisor-controlled teams and parallel research flows. MCP tools can be imported as external integrations, but they are not Hydaelyn's core runtime model.

## Install

```bash
go get github.com/Viking602/go-hydaelyn@latest
```

## Quickstart

Run a multi-agent team without external API keys using the fake provider:

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

type fakeProvider struct{}

func (fakeProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "fake"}
}

func (fakeProvider) Stream(_ context.Context, request provider.Request) (provider.Stream, error) {
	last := request.Messages[len(request.Messages)-1]
	return provider.NewSliceStream([]provider.Event{
		{Kind: provider.EventTextDelta, Text: last.Text},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	}), nil
}

func main() {
	runtime := host.New(host.Config{})
	runtime.RegisterProvider("fake", fakeProvider{})
	runtime.RegisterPattern(deepsearch.New())
	runtime.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "fake", Model: "test"})
	runtime.RegisterProfile(team.Profile{Name: "researcher", Role: team.RoleResearcher, Provider: "fake", Model: "test"})
	state, err := runtime.StartTeam(context.Background(), host.StartTeamRequest{
		Pattern:           "deepsearch",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"researcher", "researcher"},
		Input: map[string]any{
			"query":      "parallel research",
			"subqueries": []string{"architecture", "tooling"},
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

## Development

Run the same validation commands used in CI:

```bash
go test ./...                  # Run tests
go vet ./...                   # Static analysis
go test -race -timeout=10m ./... # Race detection
staticcheck ./...              # Deep static analysis
```

## Examples + Read Next

### Examples

- [examples/research](examples/research/main.go) - Fake provider quickstart
- [examples/collab](examples/collab/main.go) - Collaboration pattern
- [examples/tooling](examples/tooling/main.go) - Tool integration
- [examples/approval](examples/approval/main.go) - Approval flows
- [examples/durable](examples/durable/main.go) - Durable execution

### Read Next

- [Quickstart](docs/quickstart.md) - Deep-dive tutorial
- [Task Dataflow](docs/task-dataflow.md) - Dataflow documentation
- [Plugin Development](docs/plugin-development.md) - Extending the runtime
- [Architecture](docs/architecture/north-star-runtime.md) - Design philosophy

## Boundaries

Hydaelyn is intentionally scoped:

- **Not a CLI-first tool** - The CLI exists but is secondary and incomplete
- **Not an MCP server** - MCP tools are imported as external integrations, not the core model
- **Not a distributed system** - V1 is single-process
- **Not a framework to subclass** - It is an embeddable runtime you compose into your application
