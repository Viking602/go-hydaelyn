package core

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Viking602/venat/internal/core/model"
)

func TestRecoverRequeuesExpiredExecutionOnce(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run := mustStartRun(ctx, t, rt, "run-recovery")
	task := mustCreateTask(ctx, t, rt, CreateTaskCommand{
		RunID:        run.ID,
		TaskID:       "worker",
		OwnerAgentID: "agent-a",
	})
	envelope := mustDispatchTask(ctx, t, rt, DispatchTaskCommand{
		RunID:         run.ID,
		TaskID:        task.ID,
		TargetAgentID: "agent-a",
	})
	lease, acquired, err := rt.AcquireTaskExecution(ctx, AcquireTaskExecutionCommand{
		RunID:      run.ID,
		TaskID:     task.ID,
		EnvelopeID: envelope.ID,
		HolderType: HolderAgent,
		HolderID:   "agent-a",
		TTL:        time.Nanosecond,
	})
	if err != nil || !acquired {
		t.Fatalf("AcquireTaskExecution() lease=%#v acquired=%v err=%v", lease, acquired, err)
	}
	time.Sleep(time.Millisecond)
	if _, err := rt.Replay(ctx, run.ID, ReplayModeAudit); err != nil {
		t.Fatalf("Replay(audit) error = %v", err)
	}
	if got := mustLoadTask(ctx, t, rt, run.ID, task.ID).Status; got != TaskStatusRunning {
		t.Fatalf("audit replay mutated task status to %q", got)
	}

	if _, err := rt.Recover(ctx, run.ID); err != nil {
		t.Fatalf("Recover() error = %v", err)
	}
	recovered := mustLoadTask(ctx, t, rt, run.ID, task.ID)
	if recovered.Status != TaskStatusDispatched {
		t.Fatalf("recovered task status = %q, want dispatched", recovered.Status)
	}
	recoveredEnvelope := mustLoadEnvelope(ctx, t, rt, envelope.ID)
	if recoveredEnvelope.Status != "pending" || recoveredEnvelope.TaskVersion != recovered.Version {
		t.Fatalf("recovered envelope = %#v, task version = %d", recoveredEnvelope, recovered.Version)
	}
	if active := rt.ActiveLeaseCount(ctx, run.ID, task.ID); active != 0 {
		t.Fatalf("recovery left %d active leases", active)
	}

	eventCount := len(rt.Events(ctx, run.ID))
	version := recovered.Version
	if _, err := rt.Recover(ctx, run.ID); err != nil {
		t.Fatalf("Recover(second) error = %v", err)
	}
	if got := mustLoadTask(ctx, t, rt, run.ID, task.ID); got.Version != version {
		t.Fatalf("second recovery changed task version from %d to %d", version, got.Version)
	}
	if got := len(rt.Events(ctx, run.ID)); got != eventCount {
		t.Fatalf("second recovery appended events: before=%d after=%d", eventCount, got)
	}
}

func TestReplayRejectsUnknownMode(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run := mustStartRun(ctx, t, rt, "run-replay-mode")
	if _, err := rt.Replay(ctx, run.ID, ReplayMode("unknown")); !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("Replay(unknown) error = %v, want ErrInvalidCommand", err)
	}
}

func TestRecoverQuarantinesUnresolvedActionAttempt(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run := mustStartRun(ctx, t, rt, "run-action-recovery")
	task := mustCreateTask(ctx, t, rt, CreateTaskCommand{
		RunID:        run.ID,
		TaskID:       "action",
		OwnerAgentID: "agent-a",
		AllowsAction: true,
	})
	envelope := mustDispatchTask(ctx, t, rt, DispatchTaskCommand{
		RunID:         run.ID,
		TaskID:        task.ID,
		TargetAgentID: "agent-a",
	})
	lease, acquired, err := rt.AcquireTaskExecution(ctx, AcquireTaskExecutionCommand{
		RunID:      run.ID,
		TaskID:     task.ID,
		EnvelopeID: envelope.ID,
		HolderType: HolderAgent,
		HolderID:   "agent-a",
		TTL:        10 * time.Millisecond,
	})
	if err != nil || !acquired {
		t.Fatalf("AcquireTaskExecution() lease=%#v acquired=%v err=%v", lease, acquired, err)
	}
	first, err := rt.StartActionAttempt(ctx, StartActionAttemptCommand{
		RunID:       run.ID,
		TaskID:      task.ID,
		LeaseID:     lease.ID,
		HolderType:  HolderAgent,
		HolderID:    "agent-a",
		TaskVersion: task.Version,
		ToolName:    "deploy",
	})
	if err != nil {
		t.Fatalf("StartActionAttempt(first) error = %v", err)
	}
	second, err := rt.StartActionAttempt(ctx, StartActionAttemptCommand{
		RunID:       run.ID,
		TaskID:      task.ID,
		LeaseID:     lease.ID,
		HolderType:  HolderAgent,
		HolderID:    "agent-a",
		TaskVersion: task.Version,
		ToolName:    "notify",
	})
	if err != nil {
		t.Fatalf("StartActionAttempt(second) error = %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	projection, err := rt.Recover(ctx, run.ID)
	if err != nil {
		t.Fatalf("Recover() error = %v", err)
	}
	if got := mustLoadTask(ctx, t, rt, run.ID, task.ID); got.Status != TaskStatusReconcileRequired {
		t.Fatalf("recovered task status = %q, want reconcile_required", got.Status)
	}
	if got, err := rt.Run(ctx, run.ID); err != nil || got.Status != RunStatusReconcileRequired {
		t.Fatalf("recovered run = %#v err=%v, want reconcile_required", got, err)
	}
	if got := projection.Tasks[task.ID].Status; got != TaskStatusReconcileRequired {
		t.Fatalf("replayed task status = %q, want reconcile_required", got)
	}
	if got := mustLoadEnvelope(ctx, t, rt, envelope.ID); got.Status == "pending" {
		t.Fatalf("recovery redispatched unresolved action envelope: %#v", got)
	}
	attempts, err := rt.ListActionAttempts(ctx, model.ActionAttemptSelector{RunID: run.ID, TaskID: task.ID})
	if err != nil || len(attempts) != 2 {
		t.Fatalf("ListActionAttempts() attempts=%#v error=%v", attempts, err)
	}
	for _, attempt := range attempts {
		if attempt.Status != ActionAttemptUnknown || !attempt.RequiresReconcile {
			t.Fatalf("recovered attempt = %#v, want unknown reconciliation barrier", attempt)
		}
	}
	if _, err := rt.ResolveActionAttempt(ctx, ResolveActionAttemptCommand{
		AttemptID: first.AttemptID,
		Status:    ActionAttemptSucceeded,
	}); err != nil {
		t.Fatalf("ResolveActionAttempt(first) error = %v", err)
	}
	if got := mustLoadTask(ctx, t, rt, run.ID, task.ID); got.Status != TaskStatusReconcileRequired {
		t.Fatalf("task after first resolution = %q, want reconcile_required", got.Status)
	}
	if _, err := rt.ResolveActionAttempt(ctx, ResolveActionAttemptCommand{
		AttemptID: second.AttemptID,
		Status:    ActionAttemptTimeout,
	}); err != nil {
		t.Fatalf("ResolveActionAttempt(second) error = %v", err)
	}
	if got := mustLoadTask(ctx, t, rt, run.ID, task.ID); got.Status != TaskStatusDispatched {
		t.Fatalf("task after final resolution = %q, want dispatched", got.Status)
	}
}
