// approval demonstrates the human-in-the-loop tool gate: a policy engine
// requires approval for any tool flagged RequiresActionTask, the runtime
// pauses, a human decides, and the call resumes.
//
//	go run ./_examples/approval
package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Viking602/venat"
	"github.com/Viking602/venat/api"
)

type approvalGate struct{}

func (approvalGate) Authorize(_ context.Context, req api.PolicyRequest) (api.PolicyDecision, error) {
	if req.Operation == api.PolicyOperationToolCall && req.Tool != nil && req.Tool.RequiresActionTask {
		return api.PolicyDecision{Effect: api.PolicyEffectRequireApproval, Reason: "needs human ack"}, nil
	}
	return api.PolicyDecision{Effect: api.PolicyEffectAllow}, nil
}

func main() {
	ctx := context.Background()
	runner := venat.NewDevelopment(api.Config{PolicyEngine: approvalGate{}})

	runner.RegisterAgent(api.AgentProfile{ID: "actuator"})
	runner.RegisterTool(api.Tool{
		Name:               "deploy.rollback",
		EffectType:         api.ToolEffectWrite,
		RequiresActionTask: true,
	})

	run, _, err := runner.StartRun(ctx, api.StartRunCommand{Request: "rollback bad release"})
	must(err)
	task, err := runner.CreateTask(ctx, api.CreateTaskCommand{
		RunID: run.ID, TaskID: "rollback", OwnerAgentID: "actuator", AllowsAction: true,
	})
	must(err)
	env, err := runner.DispatchTask(ctx, api.DispatchTaskCommand{
		RunID: run.ID, TaskID: task.ID, TargetAgentID: "actuator",
	})
	must(err)
	lease, _, err := runner.AcquireTaskExecution(ctx, api.AcquireTaskExecutionCommand{
		RunID: run.ID, TaskID: task.ID, EnvelopeID: env.ID,
		HolderType: api.HolderAgent, HolderID: "actuator",
		TTL: time.Minute,
	})
	must(err)

	// First call hits the policy gate and is paused.
	_, err = runner.InvokeTool(ctx, api.ToolInvocation{
		RunID: run.ID, TaskID: task.ID, LeaseID: lease.ID,
		HolderType: api.HolderAgent, HolderID: "actuator",
		TaskVersion: task.Version, ToolName: "deploy.rollback",
	})
	if !errors.Is(err, venat.ErrPolicyDenied) {
		panic(fmt.Errorf("expected ErrPolicyDenied, got %v", err))
	}
	fmt.Println("paused: policy demands approval")

	approval, _, err := runner.RequestApproval(ctx, api.RequestApprovalCommand{
		RunID: run.ID, TaskID: task.ID, RequesterAgentID: "actuator", Reason: "rollback to last green",
	})
	must(err)
	must(runner.DecideApproval(ctx, api.DecideApprovalCommand{
		RunID: run.ID, ApprovalID: approval.ApprovalID, DecidedBy: "oncall", Decision: "approved",
	}))
	fmt.Println("approval granted:", approval.ApprovalID)
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
