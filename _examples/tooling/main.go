// tooling demonstrates the tool registration + invocation primitive: a
// read-effect tool runs through the policy gate without approval, and the
// runtime records a ToolInvocationResult on the run timeline.
//
//	go run ./_examples/tooling
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/Viking602/go-hydaelyn"
	"github.com/Viking602/go-hydaelyn/api"
)

func main() {
	ctx := context.Background()
	runner := hydaelyn.New()
	runner.RegisterAgent(api.AgentProfile{ID: "researcher"})
	runner.RegisterTool(api.Tool{
		Name:       "web.search",
		EffectType: api.ToolEffectReadOnly,
		RiskLevel:  "low",
	})

	run, _, err := runner.StartRun(ctx, api.StartRunCommand{Request: "search the web"})
	must(err)
	task, err := runner.CreateTask(ctx, api.CreateTaskCommand{
		RunID: run.ID, TaskID: "lookup", OwnerAgentID: "researcher",
	})
	must(err)
	env, err := runner.DispatchTask(ctx, api.DispatchTaskCommand{
		RunID: run.ID, TaskID: task.ID, TargetAgentID: "researcher",
	})
	must(err)
	lease, _, err := runner.AcquireTaskExecution(ctx, api.AcquireTaskExecutionCommand{
		RunID: run.ID, TaskID: task.ID, EnvelopeID: env.ID,
		HolderType: api.HolderAgent, HolderID: "researcher", TTL: time.Minute,
	})
	must(err)

	result, err := runner.InvokeTool(ctx, api.ToolInvocation{
		RunID: run.ID, TaskID: task.ID, LeaseID: lease.ID,
		HolderType: api.HolderAgent, HolderID: "researcher",
		TaskVersion: task.Version, ToolName: "web.search",
		Input: map[string]any{"q": "go-hydaelyn"},
	})
	must(err)
	fmt.Printf("invoked tool=%s output=%v\n", result.ToolName, result.Output)
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
