// Reference architecture: 用户 → 主控 → Blackboard → Mailbox → N 专家 →
// 归因 → 风险评审 → 处置.
//
// This example exercises every primitive that v2.0 promises to ship:
//
//   - Address-based fan-out dispatch (DispatchTaskFanOut + AddressKindRole)
//   - Blackboard wait (WaitForBlackboard) for the aggregator
//   - AwaitMode=All gating for the reviewer
//   - RequiresActionTask + RequireApproval for the actuator
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

	hydaelyn "github.com/Viking602/go-hydaelyn"
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

func (approvalGate) Authorize(_ context.Context, req hydaelyn.PolicyRequest) (hydaelyn.PolicyDecision, error) {
	if req.Operation == hydaelyn.PolicyOperationToolCall && req.Tool != nil && req.Tool.RequiresActionTask {
		return hydaelyn.PolicyDecision{Effect: hydaelyn.PolicyEffectRequireApproval, Reason: "tool requires human approval"}, nil
	}
	return hydaelyn.PolicyDecision{Effect: hydaelyn.PolicyEffectAllow}, nil
}

func main() {
	ctx := context.Background()
	runner := hydaelyn.New(hydaelyn.Config{PolicyEngine: approvalGate{}})

	// --- topology ----------------------------------------------------------
	specialists := []string{"monitor", "logreader", "changelog"}
	for _, id := range specialists {
		runner.RegisterAgent(hydaelyn.AgentProfile{ID: id, Role: roleSpecialist, Groups: []string{"incident"}})
	}
	runner.RegisterAgent(hydaelyn.AgentProfile{ID: "summarizer", Role: roleAggregator})
	runner.RegisterAgent(hydaelyn.AgentProfile{ID: "reviewer", Role: roleReviewer})
	runner.RegisterAgent(hydaelyn.AgentProfile{ID: "actuator", Role: roleActuator})

	runner.RegisterTool(hydaelyn.Tool{
		Name:               toolRollback,
		EffectType:         hydaelyn.ToolEffectWrite,
		RequiresActionTask: true,
		RiskLevel:          "high",
	})

	run, _, err := runner.StartRun(ctx, hydaelyn.StartRunCommand{Request: "deploy regression — investigate"})
	must(err)

	// --- 1. fan-out a heads-up envelope to every specialist by role -------
	notifyTask, err := runner.CreateTask(ctx, hydaelyn.CreateTaskCommand{
		RunID: run.ID, TaskID: "notify", OwnerComponent: "orchestrator",
	})
	must(err)
	envelopes, err := runner.DispatchTaskFanOut(ctx, hydaelyn.FanOutDispatchTaskCommand{
		RunID:   run.ID,
		TaskID:  notifyTask.ID,
		To:      hydaelyn.Address{Kind: hydaelyn.AddressKindRole, Role: roleSpecialist},
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
		_, err := runner.CreateTask(ctx, hydaelyn.CreateTaskCommand{
			RunID: run.ID, TaskID: taskID, OwnerAgentID: id,
			Goal: fmt.Sprintf("collect evidence channel #%d", i+1),
		})
		must(err)
		wg.Add(1)
		go func(taskID, agentID string, idx int) {
			defer wg.Done()
			runSpecialist(ctx, runner, run.ID, taskID, agentID, idx)
		}(taskID, id, i)
	}

	// --- 3. aggregator waits for 3 evidence items via blackboard subscribe
	want := len(specialists)
	items, err := runner.WaitForBlackboard(ctx, run.ID,
		hydaelyn.BlackboardFilter{ItemTypes: []hydaelyn.BlackboardItemType{hydaelyn.BlackboardItemEvidence}},
		func(items []hydaelyn.BlackboardItem) bool { return len(items) >= want },
		5*time.Second,
	)
	must(err)
	wg.Wait()

	// Aggregator writes a Finding synthesising the evidence.
	finding := fmt.Sprintf("3 specialists corroborate deploy regression (n=%d evidence rows)", len(items))
	must(runner.WriteItem(ctx, hydaelyn.BlackboardItem{
		RunID: run.ID, Type: hydaelyn.BlackboardItemFinding,
		Source:     hydaelyn.SourceIdentity{Type: hydaelyn.SourceAgent, ID: "summarizer"},
		Content:    finding,
		Visibility: hydaelyn.BlackboardVisibilityAgentVisible,
	}))
	fmt.Println("aggregator wrote finding:", finding)

	// --- 4. reviewer task gates on AwaitMode=All over the 3 specialists --
	reviewTask, err := runner.CreateTask(ctx, hydaelyn.CreateTaskCommand{
		RunID: run.ID, TaskID: "review", OwnerAgentID: "reviewer",
		DependsOn: specialistTaskIDs,
		AwaitMode: hydaelyn.AwaitModeAll,
	})
	must(err)
	runWorker(ctx, runner, run.ID, reviewTask.ID, "reviewer", hydaelyn.TypedReport{
		Status:  hydaelyn.ReportStatusSuccess,
		Summary: "rollback recommended",
	})

	// --- 5. action task with AllowsAction=true triggers an approval pause
	actionTask, err := runner.CreateTask(ctx, hydaelyn.CreateTaskCommand{
		RunID: run.ID, TaskID: "rollback", OwnerAgentID: "actuator",
		DependsOn: []string{reviewTask.ID}, AllowsAction: true,
	})
	must(err)
	env, err := runner.DispatchTask(ctx, hydaelyn.DispatchTaskCommand{
		RunID: run.ID, TaskID: actionTask.ID, TargetAgentID: "actuator",
	})
	must(err)
	lease, _, err := runner.AcquireTaskExecution(ctx, hydaelyn.AcquireTaskExecutionCommand{
		RunID: run.ID, TaskID: actionTask.ID, EnvelopeID: env.ID,
		HolderType: hydaelyn.HolderAgent, HolderID: "actuator",
		TTL: time.Minute,
	})
	must(err)

	// The first call is rejected by approvalGate → request approval, decide,
	// then retry. This is the canonical "approval round-trip" pattern.
	_, err = runner.InvokeTool(ctx, hydaelyn.ToolInvocation{
		RunID: run.ID, TaskID: actionTask.ID, LeaseID: lease.ID,
		HolderType: hydaelyn.HolderAgent, HolderID: "actuator",
		TaskVersion: actionTask.Version, ToolName: toolRollback,
	})
	switch {
	case errors.Is(err, hydaelyn.ErrPolicyDenied):
		fmt.Println("tool invocation paused — policy demanded approval")
	case err != nil:
		panic(fmt.Errorf("unexpected tool error: %w", err))
	}
	approval, _, err := runner.RequestApproval(ctx, hydaelyn.RequestApprovalCommand{
		RunID: run.ID, TaskID: actionTask.ID, RequesterAgentID: "actuator",
		Reason: "rollback to last green deploy",
	})
	must(err)
	must(runner.DecideApproval(ctx, hydaelyn.DecideApprovalCommand{
		RunID: run.ID, ApprovalID: approval.ApprovalID,
		DecidedBy: "oncall-human", Decision: "approved",
	}))
	fmt.Println("approval granted:", approval.ApprovalID)

	// --- 6. timeline summary --------------------------------------------
	timeline, err := runner.RunTimeline(ctx, run.ID)
	must(err)
	fmt.Printf("\nfinal timeline: %d items\n", len(timeline))
	for _, it := range timeline {
		fmt.Printf("  [%s] %s\n", it.Kind, it.Title)
	}
}

// runSpecialist plays one specialist: acquire a lease, write Evidence, submit.
func runSpecialist(ctx context.Context, runner *hydaelyn.Runner, runID, taskID, agentID string, idx int) {
	env, err := runner.DispatchTask(ctx, hydaelyn.DispatchTaskCommand{
		RunID: runID, TaskID: taskID, TargetAgentID: agentID,
	})
	must(err)
	lease, _, err := runner.AcquireTaskExecution(ctx, hydaelyn.AcquireTaskExecutionCommand{
		RunID: runID, TaskID: taskID, EnvelopeID: env.ID,
		HolderType: hydaelyn.HolderAgent, HolderID: agentID,
		TTL: time.Minute,
	})
	must(err)
	must(runner.WriteItem(ctx, hydaelyn.BlackboardItem{
		RunID: runID, TaskID: taskID, Type: hydaelyn.BlackboardItemEvidence,
		Source:     hydaelyn.SourceIdentity{Type: hydaelyn.SourceAgent, ID: agentID},
		Content:    fmt.Sprintf("signal-%d from %s", idx+1, agentID),
		Visibility: hydaelyn.BlackboardVisibilityAgentVisible,
	}))
	must(runner.SubmitTypedReport(ctx, hydaelyn.SubmitTypedReportCommand{
		RunID: runID, TaskID: taskID, LeaseID: lease.ID,
		HolderType: hydaelyn.HolderAgent, HolderID: agentID,
		TaskVersion: 1, // version after CreateTask
		Report:      hydaelyn.TypedReport{Status: hydaelyn.ReportStatusSuccess, Summary: "evidence collected"},
	}))
}

// runWorker is a 1-shot dispatch+lease+submit for non-action tasks.
func runWorker(ctx context.Context, runner *hydaelyn.Runner, runID, taskID, agentID string, report hydaelyn.TypedReport) {
	env, err := runner.DispatchTask(ctx, hydaelyn.DispatchTaskCommand{
		RunID: runID, TaskID: taskID, TargetAgentID: agentID,
	})
	must(err)
	lease, _, err := runner.AcquireTaskExecution(ctx, hydaelyn.AcquireTaskExecutionCommand{
		RunID: runID, TaskID: taskID, EnvelopeID: env.ID,
		HolderType: hydaelyn.HolderAgent, HolderID: agentID,
		TTL: time.Minute,
	})
	must(err)
	task, err := runner.Task(ctx, runID, taskID)
	must(err)
	must(runner.SubmitTypedReport(ctx, hydaelyn.SubmitTypedReportCommand{
		RunID: runID, TaskID: taskID, LeaseID: lease.ID,
		HolderType: hydaelyn.HolderAgent, HolderID: agentID,
		TaskVersion: task.Version, Report: report,
	}))
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
