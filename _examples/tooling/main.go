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

	"github.com/Viking602/go-hydaelyn/orchestrator"
)

func main() {
	ctx := context.Background()
	rt := orchestrator.NewRuntime(orchestrator.Config{})
	rt.RegisterAgent(orchestrator.AgentProfile{ID: "researcher"})
	rt.RegisterTool(orchestrator.Tool{
		Name:       "web.search",
		EffectType: orchestrator.ToolEffectReadOnly,
		RiskLevel:  "low",
	})

	run, _, err := rt.StartRun(ctx, orchestrator.StartRunCommand{Request: "search the web"})
	must(err)
	task, err := rt.CreateTask(ctx, orchestrator.CreateTaskCommand{
		RunID: run.ID, TaskID: "lookup", OwnerAgentID: "researcher",
	})
	must(err)
	env, err := rt.DispatchTask(ctx, orchestrator.DispatchTaskCommand{
		RunID: run.ID, TaskID: task.ID, TargetAgentID: "researcher",
	})
	must(err)
	lease, _, err := rt.AcquireTaskExecution(ctx, orchestrator.AcquireTaskExecutionCommand{
		RunID: run.ID, TaskID: task.ID, EnvelopeID: env.ID,
		HolderType: orchestrator.HolderAgent, HolderID: "researcher", TTL: time.Minute,
	})
	must(err)

	result, err := rt.InvokeTool(ctx, orchestrator.ToolInvocation{
		RunID: run.ID, TaskID: task.ID, LeaseID: lease.ID,
		HolderType: orchestrator.HolderAgent, HolderID: "researcher",
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
