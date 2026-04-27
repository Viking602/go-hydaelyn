// Reference architecture: 用户 → 主控 → Blackboard → Mailbox → N 专家 →
// 归因 → 风险评审 → 处置.
//
// This example exercises every primitive that v2.0 promises to ship:
//
//   * Address-based fan-out dispatch (DispatchTaskFanOut + AddressKindRole)
//   * Blackboard wait (WaitForBlackboard) for the aggregator
//   * AwaitMode=All gating for the reviewer
//   * RequiresActionTask + RequireApproval for the actuator
//
// All "business" vocabulary (incident, hazard, monitor) lives in this example
// — the framework only knows about agents, tasks, blackboard items and tools.
package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Viking602/go-hydaelyn/orchestrator"
)

const (
	roleSpecialist = "incident.specialist"
	roleAggregator = "incident.aggregator"
	roleReviewer   = "incident.reviewer"
	roleActuator   = "incident.actuator"

	toolRollback = "rollback.deploy"
)

// approvalGate forces every action-eligible tool call to be confirmed by a
// human. The framework owns the approval/resume protocol; we only provide the
// effect.
type approvalGate struct{}

func (approvalGate) Authorize(_ context.Context, req orchestrator.PolicyRequest) (orchestrator.PolicyDecision, error) {
	if req.Operation == orchestrator.PolicyOperationToolCall && req.Tool != nil && req.Tool.RequiresActionTask {
		return orchestrator.PolicyDecision{Effect: orchestrator.PolicyEffectRequireApproval, Reason: "tool requires human approval"}, nil
	}
	return orchestrator.PolicyDecision{Effect: orchestrator.PolicyEffectAllow}, nil
}

func main() {
	ctx := context.Background()
	rt := orchestrator.NewRuntime(orchestrator.Config{PolicyEngine: approvalGate{}})

	// --- topology ----------------------------------------------------------
	specialists := []string{"monitor", "logreader", "changelog"}
	for _, id := range specialists {
		rt.RegisterAgent(orchestrator.AgentProfile{ID: id, Role: roleSpecialist, Groups: []string{"incident"}})
	}
	rt.RegisterAgent(orchestrator.AgentProfile{ID: "summarizer", Role: roleAggregator})
	rt.RegisterAgent(orchestrator.AgentProfile{ID: "reviewer", Role: roleReviewer})
	rt.RegisterAgent(orchestrator.AgentProfile{ID: "actuator", Role: roleActuator})

	rt.RegisterTool(orchestrator.Tool{
		Name:               toolRollback,
		EffectType:         orchestrator.ToolEffectWrite,
		RequiresActionTask: true,
		RiskLevel:          "high",
	})

	run, _, err := rt.StartRun(ctx, orchestrator.StartRunCommand{Request: "deploy regression — investigate"})
	must(err)

	// --- 1. fan-out a heads-up envelope to every specialist by role -------
	notifyTask, err := rt.CreateTask(ctx, orchestrator.CreateTaskCommand{
		RunID: run.ID, TaskID: "notify", OwnerComponent: "orchestrator",
	})
	must(err)
	envelopes, err := rt.DispatchTaskFanOut(ctx, orchestrator.FanOutDispatchTaskCommand{
		RunID:   run.ID,
		TaskID:  notifyTask.ID,
		To:      orchestrator.Address{Kind: orchestrator.AddressKindRole, Role: roleSpecialist},
		Payload: map[string]any{"alert": "5xx spike on checkout"},
	})
	must(err)
	fmt.Printf("fan-out delivered %d envelopes to role=%s\n", len(envelopes), roleSpecialist)

	// --- 2. one task per specialist; they run in parallel ----------------
	specialistTaskIDs := make([]string, 0, len(specialists))
	var wg sync.WaitGroup
	for i, id := range specialists {
		taskID := fmt.Sprintf("evidence-%s", id)
		specialistTaskIDs = append(specialistTaskIDs, taskID)
		_, err := rt.CreateTask(ctx, orchestrator.CreateTaskCommand{
			RunID: run.ID, TaskID: taskID, OwnerAgentID: id,
			Goal: fmt.Sprintf("collect evidence channel #%d", i+1),
		})
		must(err)
		wg.Add(1)
		go func(taskID, agentID string, idx int) {
			defer wg.Done()
			runSpecialist(ctx, rt, run.ID, taskID, agentID, idx)
		}(taskID, id, i)
	}

	// --- 3. aggregator waits for 3 evidence items via blackboard subscribe
	want := len(specialists)
	items, err := rt.WaitForBlackboard(ctx, run.ID,
		orchestrator.BlackboardFilter{ItemTypes: []orchestrator.BlackboardItemType{orchestrator.BlackboardItemEvidence}},
		func(items []orchestrator.BlackboardItem) bool { return len(items) >= want },
		5*time.Second,
	)
	must(err)
	wg.Wait()

	// Aggregator writes a Finding synthesising the evidence.
	finding := fmt.Sprintf("3 specialists corroborate deploy regression (n=%d evidence rows)", len(items))
	must(rt.WriteItem(ctx, orchestrator.BlackboardItem{
		RunID: run.ID, Type: orchestrator.BlackboardItemFinding,
		Source:     orchestrator.SourceIdentity{Type: orchestrator.SourceAgent, ID: "summarizer"},
		Content:    finding,
		Visibility: orchestrator.BlackboardVisibilityAgentVisible,
	}))
	fmt.Println("aggregator wrote finding:", finding)

	// --- 4. reviewer task gates on AwaitMode=All over the 3 specialists --
	reviewTask, err := rt.CreateTask(ctx, orchestrator.CreateTaskCommand{
		RunID: run.ID, TaskID: "review", OwnerAgentID: "reviewer",
		DependsOn: specialistTaskIDs,
		AwaitMode: orchestrator.AwaitModeAll,
	})
	must(err)
	runWorker(ctx, rt, run.ID, reviewTask.ID, "reviewer", orchestrator.TypedReport{
		Status:  orchestrator.ReportStatusSuccess,
		Summary: "rollback recommended",
	})

	// --- 5. action task with AllowsAction=true triggers an approval pause
	actionTask, err := rt.CreateTask(ctx, orchestrator.CreateTaskCommand{
		RunID: run.ID, TaskID: "rollback", OwnerAgentID: "actuator",
		DependsOn: []string{reviewTask.ID}, AllowsAction: true,
	})
	must(err)
	env, err := rt.DispatchTask(ctx, orchestrator.DispatchTaskCommand{
		RunID: run.ID, TaskID: actionTask.ID, TargetAgentID: "actuator",
	})
	must(err)
	lease, _, err := rt.AcquireTaskExecution(ctx, orchestrator.AcquireTaskExecutionCommand{
		RunID: run.ID, TaskID: actionTask.ID, EnvelopeID: env.ID,
		HolderType: orchestrator.HolderAgent, HolderID: "actuator",
		TTL: time.Minute,
	})
	must(err)

	// The first call is rejected by approvalGate → request approval, decide,
	// then retry. This is the canonical "approval round-trip" pattern.
	_, err = rt.InvokeTool(ctx, orchestrator.ToolInvocation{
		RunID: run.ID, TaskID: actionTask.ID, LeaseID: lease.ID,
		HolderType: orchestrator.HolderAgent, HolderID: "actuator",
		TaskVersion: actionTask.Version, ToolName: toolRollback,
	})
	switch {
	case errors.Is(err, orchestrator.ErrPolicyDenied):
		fmt.Println("tool invocation paused — policy demanded approval")
	case err != nil:
		panic(fmt.Errorf("unexpected tool error: %w", err))
	}
	approval, _, err := rt.RequestApproval(ctx, orchestrator.RequestApprovalCommand{
		RunID: run.ID, TaskID: actionTask.ID, RequesterAgentID: "actuator",
		Reason: "rollback to last green deploy",
	})
	must(err)
	must(rt.DecideApproval(ctx, orchestrator.DecideApprovalCommand{
		RunID: run.ID, ApprovalID: approval.ApprovalID,
		DecidedBy: "oncall-human", Decision: "approved",
	}))
	fmt.Println("approval granted:", approval.ApprovalID)

	// --- 6. timeline summary --------------------------------------------
	timeline, err := rt.RunTimeline(ctx, run.ID)
	must(err)
	fmt.Printf("\nfinal timeline: %d items\n", len(timeline))
	for _, it := range timeline {
		fmt.Printf("  [%s] %s\n", it.Kind, it.Title)
	}
}

// runSpecialist plays one specialist: acquire a lease, write Evidence, submit.
func runSpecialist(ctx context.Context, rt *orchestrator.Runtime, runID, taskID, agentID string, idx int) {
	env, err := rt.DispatchTask(ctx, orchestrator.DispatchTaskCommand{
		RunID: runID, TaskID: taskID, TargetAgentID: agentID,
	})
	must(err)
	lease, _, err := rt.AcquireTaskExecution(ctx, orchestrator.AcquireTaskExecutionCommand{
		RunID: runID, TaskID: taskID, EnvelopeID: env.ID,
		HolderType: orchestrator.HolderAgent, HolderID: agentID,
		TTL: time.Minute,
	})
	must(err)
	must(rt.WriteItem(ctx, orchestrator.BlackboardItem{
		RunID: runID, TaskID: taskID, Type: orchestrator.BlackboardItemEvidence,
		Source:     orchestrator.SourceIdentity{Type: orchestrator.SourceAgent, ID: agentID},
		Content:    fmt.Sprintf("signal-%d from %s", idx+1, agentID),
		Visibility: orchestrator.BlackboardVisibilityAgentVisible,
	}))
	must(rt.SubmitTypedReport(ctx, orchestrator.SubmitTypedReportCommand{
		RunID: runID, TaskID: taskID, LeaseID: lease.ID,
		HolderType: orchestrator.HolderAgent, HolderID: agentID,
		TaskVersion: 1, // version after CreateTask
		Report:      orchestrator.TypedReport{Status: orchestrator.ReportStatusSuccess, Summary: "evidence collected"},
	}))
}

// runWorker is a 1-shot dispatch+lease+submit for non-action tasks.
func runWorker(ctx context.Context, rt *orchestrator.Runtime, runID, taskID, agentID string, report orchestrator.TypedReport) {
	env, err := rt.DispatchTask(ctx, orchestrator.DispatchTaskCommand{
		RunID: runID, TaskID: taskID, TargetAgentID: agentID,
	})
	must(err)
	lease, _, err := rt.AcquireTaskExecution(ctx, orchestrator.AcquireTaskExecutionCommand{
		RunID: runID, TaskID: taskID, EnvelopeID: env.ID,
		HolderType: orchestrator.HolderAgent, HolderID: agentID,
		TTL: time.Minute,
	})
	must(err)
	task, err := rt.Task(ctx, runID, taskID)
	must(err)
	must(rt.SubmitTypedReport(ctx, orchestrator.SubmitTypedReportCommand{
		RunID: runID, TaskID: taskID, LeaseID: lease.ID,
		HolderType: orchestrator.HolderAgent, HolderID: agentID,
		TaskVersion: task.Version, Report: report,
	}))
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
