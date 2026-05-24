package hydaelyn

import (
	"context"
	"testing"
	"time"

	"github.com/Viking602/go-hydaelyn/api"
)

func TestWriteAndSelectItems(t *testing.T) {
	r := newTestRunner(t)
	ctx := context.Background()
	run, _, _ := r.StartRun(ctx, api.StartRunCommand{Request: "test"})

	item := api.BlackboardItem{
		RunID:      run.ID,
		Key:        "result",
		Visibility: api.BlackboardVisibilityInternal,
		Source:     api.SourceIdentity{Type: api.SourceType("agent"), ID: "agent-1"},
		Payload:    "some data",
	}
	if err := r.WriteItem(ctx, item); err != nil {
		t.Fatalf("WriteItem: %v", err)
	}

	items, err := r.SelectItems(ctx, run.ID, api.BlackboardSelector{})
	if err != nil {
		t.Fatalf("SelectItems: %v", err)
	}
	if len(items) == 0 {
		t.Error("expected at least one blackboard item")
	}
}

func TestSubscribe_ReceivesWrittenItem(t *testing.T) {
	r := newTestRunner(t)
	ctx := context.Background()
	run, _, _ := r.StartRun(ctx, api.StartRunCommand{Request: "test"})

	ch, cancel, err := r.Subscribe(ctx, run.ID, api.BlackboardFilter{})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel() //nolint:errcheck

	if err := r.WriteItem(ctx, api.BlackboardItem{
		RunID:      run.ID,
		Key:        "signal",
		Visibility: api.BlackboardVisibilityInternal,
		Source:     api.SourceIdentity{Type: api.SourceType("agent"), ID: "a1"},
		Payload:    "ping",
	}); err != nil {
		t.Fatalf("WriteItem: %v", err)
	}

	select {
	case got := <-ch:
		if got.Key != "signal" {
			t.Errorf("unexpected key %q", got.Key)
		}
	case <-time.After(2 * time.Second):
		t.Error("timed out waiting for blackboard item")
	}
}

func TestWaitForBlackboard_PredicateSatisfied(t *testing.T) {
	r := newTestRunner(t)
	ctx := context.Background()
	run, _, _ := r.StartRun(ctx, api.StartRunCommand{Request: "test"})

	if err := r.WriteItem(ctx, api.BlackboardItem{
		RunID:      run.ID,
		Key:        "done",
		Visibility: api.BlackboardVisibilityInternal,
		Source:     api.SourceIdentity{Type: api.SourceType("agent"), ID: "a1"},
		Payload:    "ok",
	}); err != nil {
		t.Fatalf("WriteItem: %v", err)
	}

	items, err := r.WaitForBlackboard(ctx, run.ID, api.BlackboardFilter{},
		func(items []api.BlackboardItem) bool { return len(items) >= 1 },
		2*time.Second)
	if err != nil {
		t.Fatalf("WaitForBlackboard: %v", err)
	}
	if len(items) == 0 {
		t.Error("expected items from WaitForBlackboard")
	}
}
