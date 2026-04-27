// research demonstrates fan-out by group. A single dispatch produces one
// envelope per group member; whichever agent acquires the lease first owns
// the task (work-stealing semantics).
//
//	go run ./_examples/research
package main

import (
	"context"
	"fmt"

	"github.com/Viking602/go-hydaelyn/orchestrator"
)

const groupResearchers = "researchers"

func main() {
	ctx := context.Background()
	rt := orchestrator.NewRuntime(orchestrator.Config{})

	pool := []string{"r1", "r2", "r3"}
	for _, id := range pool {
		rt.RegisterAgent(orchestrator.AgentProfile{ID: id, Groups: []string{groupResearchers}})
	}

	run, _, err := rt.StartRun(ctx, orchestrator.StartRunCommand{Request: "compare Go agent runtimes"})
	must(err)
	task, err := rt.CreateTask(ctx, orchestrator.CreateTaskCommand{
		RunID: run.ID, TaskID: "investigate", OwnerComponent: "orchestrator",
	})
	must(err)

	envelopes, err := rt.DispatchTaskFanOut(ctx, orchestrator.FanOutDispatchTaskCommand{
		RunID: run.ID, TaskID: task.ID,
		To:      orchestrator.Address{Kind: orchestrator.AddressKindGroup, Group: groupResearchers},
		Payload: map[string]any{"query": "compare Go agent runtimes"},
	})
	must(err)
	fmt.Printf("fan-out: %d envelope(s) for group %q\n", len(envelopes), groupResearchers)
	for _, env := range envelopes {
		fmt.Printf("  envelope %s → agent %s\n", env.ID, env.TargetAgentID)
	}
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
