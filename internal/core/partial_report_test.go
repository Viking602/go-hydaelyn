package core

import (
	"context"
	"testing"

	"github.com/Viking602/venat/api"
)

// A partial-success report used to save task.Result and emit nothing, so the
// interim result existed in the store but not in the event log and could
// never be replayed. It must stay a progress report: the task keeps running
// and the holder keeps its lease for the terminal report that follows.
func TestPartialReportIsReplayableAndKeepsTheLease(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run, task := mustStartWorker(ctx, t, rt, "run-partial-report", "worker")
	lease := leaseTask(ctx, t, rt, run.ID, task.ID, HolderAgent, "agent-a")

	if err := rt.SubmitTypedReport(ctx, SubmitTypedReportCommand{
		RunID: run.ID, TaskID: task.ID, LeaseID: lease.ID,
		HolderType: HolderAgent, HolderID: "agent-a", TaskVersion: task.Version,
		Report: api.TypedReport{Status: api.ReportStatusPartialSuccess, Summary: "halfway"},
	}); err != nil {
		t.Fatalf("SubmitTypedReport(partial) error = %v", err)
	}

	if !collectEventTypes(rt.Events(ctx, run.ID)).Contains(EventTaskPartiallyCompleted) {
		t.Fatalf("partial report emitted no event: %#v", rt.Events(ctx, run.ID))
	}

	stored := mustLoadTask(ctx, t, rt, run.ID, task.ID)
	if stored.Status != TaskStatusRunning {
		t.Fatalf("partial report changed task status to %q, want it to keep running", stored.Status)
	}
	if stored.Result == nil || stored.Result.Summary != "halfway" {
		t.Fatalf("stored task result = %#v, want the partial summary", stored.Result)
	}

	projection, err := rt.Replay(ctx, run.ID, ReplayModeAudit)
	if err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
	replayed := projection.Tasks[task.ID]
	if replayed.Status != stored.Status {
		t.Fatalf("replayed status = %q, want the stored %q", replayed.Status, stored.Status)
	}
	if replayed.Result == nil || replayed.Result.Summary != "halfway" {
		t.Fatalf("replayed task result = %#v, want the partial summary to survive replay", replayed.Result)
	}

	// The holder keeps working under the same lease and finishes the task.
	if active := mustActiveLeaseCount(ctx, t, rt, run.ID, task.ID); active != 1 {
		t.Fatalf("partial report left %d active leases, want the holder's 1", active)
	}
	if err := rt.SubmitTypedReport(ctx, SubmitTypedReportCommand{
		RunID: run.ID, TaskID: task.ID, LeaseID: lease.ID,
		HolderType: HolderAgent, HolderID: "agent-a", TaskVersion: stored.Version,
		Report: api.TypedReport{Status: api.ReportStatusSuccess, Summary: "done"},
	}); err != nil {
		t.Fatalf("SubmitTypedReport(success under the same lease) error = %v", err)
	}
	if got := mustLoadTask(ctx, t, rt, run.ID, task.ID).Status; got != TaskStatusCompleted {
		t.Fatalf("final task status = %q, want completed", got)
	}
}
