// research demonstrates fan-out by group. A single dispatch produces one
// envelope per group member; whichever agent acquires the lease first owns
// the task (work-stealing semantics).
//
//	go run ./_examples/research
package main

import (
	"context"
	"fmt"

	hydaelyn "github.com/Viking602/go-hydaelyn"
)

const groupResearchers = "researchers"

func main() {
	ctx := context.Background()
	runner := hydaelyn.New()

	pool := []string{"r1", "r2", "r3"}
	for _, id := range pool {
		runner.RegisterAgent(hydaelyn.AgentProfile{ID: id, Groups: []string{groupResearchers}})
	}

	run, _, err := runner.StartRun(ctx, hydaelyn.StartRunCommand{Request: "compare Go agent runtimes"})
	must(err)
	task, err := runner.CreateTask(ctx, hydaelyn.CreateTaskCommand{
		RunID: run.ID, TaskID: "investigate", OwnerComponent: "orchestrator",
	})
	must(err)

	envelopes, err := runner.DispatchTaskFanOut(ctx, hydaelyn.FanOutDispatchTaskCommand{
		RunID: run.ID, TaskID: task.ID,
		To:      hydaelyn.Address{Kind: hydaelyn.AddressKindGroup, Group: groupResearchers},
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
