// governed_tool demonstrates policy-gated tool execution.
//
//	go run ./_examples/governed_tool
package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/Viking602/go-hydaelyn"
	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/policy"
	"github.com/Viking602/go-hydaelyn/tool"
	"github.com/Viking602/go-hydaelyn/worker"
)

func main() {
	ctx := context.Background()
	runner := hydaelyn.New(api.Config{PolicyEngine: policy.DenySideEffectsByDefault()})
	runner.RegisterAgent(api.AgentProfile{ID: "agent-a"})

	run, _, err := runner.StartRun(ctx, api.StartRunCommand{Request: "try a write tool"})
	must(err)
	task, err := runner.CreateTask(ctx, api.CreateTaskCommand{
		RunID: run.ID, TaskID: "write-task", OwnerAgentID: "agent-a", AllowsAction: true,
	})
	must(err)
	env, err := runner.DispatchTask(ctx, api.DispatchTaskCommand{RunID: run.ID, TaskID: task.ID, TargetAgentID: "agent-a"})
	must(err)
	lease, _, err := runner.AcquireTaskExecution(ctx, api.AcquireTaskExecutionCommand{
		RunID: run.ID, TaskID: task.ID, EnvelopeID: env.ID, HolderType: api.HolderAgent, HolderID: "agent-a",
	})
	must(err)

	bus := worker.GovernedToolBus{
		Runner: runner, Bus: tool.NewBus(writeTool{}), RunID: run.ID, TaskID: task.ID,
		LeaseID: lease.ID, HolderType: api.HolderAgent, HolderID: "agent-a", TaskVersion: task.Version,
	}
	_, err = bus.Execute(ctx, tool.Call{Name: "write_file"}, nil)
	if errors.Is(err, hydaelyn.ErrPolicyDenied) {
		fmt.Println("write tool denied by policy")
		return
	}
	must(err)
}

type writeTool struct{}

func (writeTool) Definition() tool.Definition {
	return tool.Definition{Name: "write_file", EffectType: tool.EffectWrite, RequiresActionTask: true}
}

func (writeTool) Execute(context.Context, tool.Call, tool.UpdateSink) (tool.Result, error) {
	return tool.Result{Name: "write_file", Content: "wrote file"}, nil
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
