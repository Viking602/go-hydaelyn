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

	"github.com/Viking602/go-hydaelyn/orchestrator"
)

type approvalGate struct{}

func (approvalGate) Authorize(_ context.Context, req orchestrator.PolicyRequest) (orchestrator.PolicyDecision, error) {
	if req.Operation == orchestrator.PolicyOperationToolCall && req.Tool != nil && req.Tool.RequiresActionTask {
		return orchestrator.PolicyDecision{Effect: orchestrator.PolicyEffectRequireApproval, Reason: "needs human ack"}, nil
	}
	return orchestrator.PolicyDecision{Effect: orchestrator.PolicyEffectAllow}, nil
}

func main() {
	ctx := context.Background()
	rt := orchestrator.NewRuntime(orchestrator.Config{PolicyEngine: approvalGate{}})

	rt.RegisterAgent(orchestrator.AgentProfile{ID: "actuator"})
	rt.RegisterTool(orchestrator.Tool{
		Name:               "deploy.rollback",
		EffectType:         orchestrator.ToolEffectWrite,
		RequiresActionTask: true,
	})

	run, _, err := rt.StartRun(ctx, orchestrator.StartRunCommand{Request: "rollback bad release"})
	must(err)
	task, err := rt.CreateTask(ctx, orchestrator.CreateTaskCommand{
		RunID: run.ID, TaskID: "rollback", OwnerAgentID: "actuator", AllowsAction: true,
	})
	must(err)
	env, err := rt.DispatchTask(ctx, orchestrator.DispatchTaskCommand{
		RunID: run.ID, TaskID: task.ID, TargetAgentID: "actuator",
	})
	must(err)
	lease, _, err := rt.AcquireTaskExecution(ctx, orchestrator.AcquireTaskExecutionCommand{
		RunID: run.ID, TaskID: task.ID, EnvelopeID: env.ID,
		HolderType: orchestrator.HolderAgent, HolderID: "actuator",
		TTL: time.Minute,
	})
	must(err)

	// First call hits the policy gate and is paused.
	_, err = rt.InvokeTool(ctx, orchestrator.ToolInvocation{
		RunID: run.ID, TaskID: task.ID, LeaseID: lease.ID,
		HolderType: orchestrator.HolderAgent, HolderID: "actuator",
		TaskVersion: task.Version, ToolName: "deploy.rollback",
	})
	if !errors.Is(err, orchestrator.ErrPolicyDenied) {
		panic(fmt.Errorf("expected ErrPolicyDenied, got %v", err))
	}
	fmt.Println("paused: policy demands approval")

	approval, _, err := rt.RequestApproval(ctx, orchestrator.RequestApprovalCommand{
		RunID: run.ID, TaskID: task.ID, RequesterAgentID: "actuator", Reason: "rollback to last green",
	})
	must(err)
	must(rt.DecideApproval(ctx, orchestrator.DecideApprovalCommand{
		RunID: run.ID, ApprovalID: approval.ApprovalID, DecidedBy: "oncall", Decision: "approved",
	}))
	fmt.Println("approval granted:", approval.ApprovalID)
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
