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

	hydaelyn "github.com/Viking602/go-hydaelyn"
)

type approvalGate struct{}

func (approvalGate) Authorize(_ context.Context, req hydaelyn.PolicyRequest) (hydaelyn.PolicyDecision, error) {
	if req.Operation == hydaelyn.PolicyOperationToolCall && req.Tool != nil && req.Tool.RequiresActionTask {
		return hydaelyn.PolicyDecision{Effect: hydaelyn.PolicyEffectRequireApproval, Reason: "needs human ack"}, nil
	}
	return hydaelyn.PolicyDecision{Effect: hydaelyn.PolicyEffectAllow}, nil
}

func main() {
	ctx := context.Background()
	runner := hydaelyn.New(hydaelyn.Config{PolicyEngine: approvalGate{}})

	runner.RegisterAgent(hydaelyn.AgentProfile{ID: "actuator"})
	runner.RegisterTool(hydaelyn.Tool{
		Name:               "deploy.rollback",
		EffectType:         hydaelyn.ToolEffectWrite,
		RequiresActionTask: true,
	})

	run, _, err := runner.StartRun(ctx, hydaelyn.StartRunCommand{Request: "rollback bad release"})
	must(err)
	task, err := runner.CreateTask(ctx, hydaelyn.CreateTaskCommand{
		RunID: run.ID, TaskID: "rollback", OwnerAgentID: "actuator", AllowsAction: true,
	})
	must(err)
	env, err := runner.DispatchTask(ctx, hydaelyn.DispatchTaskCommand{
		RunID: run.ID, TaskID: task.ID, TargetAgentID: "actuator",
	})
	must(err)
	lease, _, err := runner.AcquireTaskExecution(ctx, hydaelyn.AcquireTaskExecutionCommand{
		RunID: run.ID, TaskID: task.ID, EnvelopeID: env.ID,
		HolderType: hydaelyn.HolderAgent, HolderID: "actuator",
		TTL: time.Minute,
	})
	must(err)

	// First call hits the policy gate and is paused.
	_, err = runner.InvokeTool(ctx, hydaelyn.ToolInvocation{
		RunID: run.ID, TaskID: task.ID, LeaseID: lease.ID,
		HolderType: hydaelyn.HolderAgent, HolderID: "actuator",
		TaskVersion: task.Version, ToolName: "deploy.rollback",
	})
	if !errors.Is(err, hydaelyn.ErrPolicyDenied) {
		panic(fmt.Errorf("expected ErrPolicyDenied, got %v", err))
	}
	fmt.Println("paused: policy demands approval")

	approval, _, err := runner.RequestApproval(ctx, hydaelyn.RequestApprovalCommand{
		RunID: run.ID, TaskID: task.ID, RequesterAgentID: "actuator", Reason: "rollback to last green",
	})
	must(err)
	must(runner.DecideApproval(ctx, hydaelyn.DecideApprovalCommand{
		RunID: run.ID, ApprovalID: approval.ApprovalID, DecidedBy: "oncall", Decision: "approved",
	}))
	fmt.Println("approval granted:", approval.ApprovalID)
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
