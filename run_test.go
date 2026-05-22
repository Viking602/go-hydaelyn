package hydaelyn

import (
	"context"
	"testing"

	"github.com/Viking602/go-hydaelyn/api"
)

func newTestRunner(t *testing.T) *Runner {
	t.Helper()
	return New()
}

func TestQueueRun_ReturnsRun(t *testing.T) {
	r := newTestRunner(t)
	ctx := context.Background()
	run, err := r.QueueRun(ctx, api.StartRunCommand{Request: "hello"})
	if err != nil {
		t.Fatalf("QueueRun: %v", err)
	}
	if run.ID == "" {
		t.Error("expected non-empty run ID")
	}
}

func TestStartRun_ReturnsRunAndTask(t *testing.T) {
	r := newTestRunner(t)
	ctx := context.Background()
	run, task, err := r.StartRun(ctx, api.StartRunCommand{Request: "hello"})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if run.ID == "" {
		t.Error("expected non-empty run ID")
	}
	if task.ID == "" {
		t.Error("expected non-empty task ID")
	}
	if task.RunID != run.ID {
		t.Errorf("task.RunID %q != run.ID %q", task.RunID, run.ID)
	}
}

func TestRun_LoadsExistingRun(t *testing.T) {
	r := newTestRunner(t)
	ctx := context.Background()
	run, _, _ := r.StartRun(ctx, api.StartRunCommand{Request: "test"})
	got, err := r.Run(ctx, run.ID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got.ID != run.ID {
		t.Errorf("run ID mismatch: got %q want %q", got.ID, run.ID)
	}
}

func TestRun_NotFound(t *testing.T) {
	r := newTestRunner(t)
	_, err := r.Run(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent run")
	}
}

func TestEvents_ReturnsEventsForRun(t *testing.T) {
	r := newTestRunner(t)
	ctx := context.Background()
	run, _, _ := r.StartRun(ctx, api.StartRunCommand{Request: "test"})
	events := r.Events(run.ID)
	if len(events) == 0 {
		t.Error("expected at least one event after StartRun")
	}
}

func TestRunEvents_SameAsEvents(t *testing.T) {
	r := newTestRunner(t)
	ctx := context.Background()
	run, _, _ := r.StartRun(ctx, api.StartRunCommand{Request: "test"})
	events1 := r.Events(run.ID)
	events2, err := r.RunEvents(ctx, run.ID)
	if err != nil {
		t.Fatalf("RunEvents: %v", err)
	}
	if len(events1) != len(events2) {
		t.Errorf("Events and RunEvents returned different counts: %d vs %d", len(events1), len(events2))
	}
}

func TestReplayRunState_ReturnsSomeProjection(t *testing.T) {
	r := newTestRunner(t)
	ctx := context.Background()
	run, _, _ := r.StartRun(ctx, api.StartRunCommand{Request: "test"})
	projection, err := r.ReplayRunState(run.ID)
	if err != nil {
		t.Fatalf("ReplayRunState: %v", err)
	}
	if projection.Run.ID == "" {
		t.Error("expected non-empty Run.ID in projection")
	}
}

func TestRunTimeline_DoesNotError(t *testing.T) {
	r := newTestRunner(t)
	ctx := context.Background()
	run, _, _ := r.StartRun(ctx, api.StartRunCommand{Request: "test"})
	_, err := r.RunTimeline(ctx, run.ID)
	if err != nil {
		t.Fatalf("RunTimeline: %v", err)
	}
}
