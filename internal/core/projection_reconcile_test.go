package core

import (
	"context"
	"errors"
	"testing"

	"github.com/Viking602/venat/api"
)

// Replay derives task state only from events that carry a task payload, so
// an event that moves a task must carry the moved task. ActionReconcileRequired
// moves the task to reconcile_required; without the payload the projection
// left it Running while the store said otherwise.
func TestReplayProjectsReconcileRequiredFromTypedReport(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run, task := mustStartWorker(ctx, t, rt, "run-replay-reconcile", "worker")
	lease := leaseTask(ctx, t, rt, run.ID, task.ID, HolderAgent, "agent-a")

	err := rt.SubmitTypedReport(ctx, SubmitTypedReportCommand{
		RunID: run.ID, TaskID: task.ID, LeaseID: lease.ID,
		HolderType: HolderAgent, HolderID: "agent-a",
		TaskVersion: task.Version,
		Report: api.TypedReport{
			Status:        api.ReportStatusSuccess,
			Summary:       "outcome unknown",
			ActionOutcome: &api.ActionOutcome{AttemptID: "attempt-1", Status: ActionAttemptUnknown},
		},
	})
	if !errors.Is(err, api.ErrActionReconcileRequired) {
		t.Fatalf("SubmitTypedReport(unknown outcome) error = %v, want ErrActionReconcileRequired", err)
	}

	stored := mustLoadTask(ctx, t, rt, run.ID, task.ID)
	if stored.Status != TaskStatusReconcileRequired {
		t.Fatalf("stored task status = %q, want %q", stored.Status, TaskStatusReconcileRequired)
	}

	projection, err := rt.Replay(ctx, run.ID, ReplayModeAudit)
	if err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
	if got := projection.Tasks[task.ID].Status; got != stored.Status {
		t.Fatalf("replayed task status = %q, want the stored %q", got, stored.Status)
	}
}
