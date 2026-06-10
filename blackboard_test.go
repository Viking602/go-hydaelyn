package hydaelyn

import (
	"context"
	"runtime"
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

func TestSubscribe_CancelStopsForwarderWithoutReader(t *testing.T) {
	r := newTestRunner(t)
	ctx := context.Background()
	run, _, _ := r.StartRun(ctx, api.StartRunCommand{Request: "test"})

	before := runtime.NumGoroutine()

	_, cancel, err := r.Subscribe(ctx, run.ID, api.BlackboardFilter{})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	// Publish an item nobody ever reads, so the forwarder parks on its
	// send into the consumer channel.
	if err := r.WriteItem(ctx, api.BlackboardItem{
		RunID:      run.ID,
		Key:        "unread",
		Visibility: api.BlackboardVisibilityInternal,
		Source:     api.SourceIdentity{Type: api.SourceType("agent"), ID: "a1"},
		Payload:    "ping",
	}); err != nil {
		t.Fatalf("WriteItem: %v", err)
	}
	time.Sleep(50 * time.Millisecond) // let the forwarder pick up the item and park

	if err := cancel(); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	// The forwarder (and the subscription it wraps) must wind down even
	// though the consumer never read the channel.
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > before {
		if time.Now().After(deadline) {
			t.Fatalf("goroutines after cancel = %d, want <= %d (forwarder leaked)", runtime.NumGoroutine(), before)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestSubscribe_CtxCancelReleasesSubscriptionWithoutCancelCall(t *testing.T) {
	r := newTestRunner(t)
	run, _, _ := r.StartRun(context.Background(), api.StartRunCommand{Request: "test"})

	before := runtime.NumGoroutine()

	ctx, cancelCtx := context.WithCancel(context.Background())
	_, cancel, err := r.Subscribe(ctx, run.ID, api.BlackboardFilter{})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	// Abandon the subscription via ctx only — the upstream registration
	// must still be torn down without an explicit cancel() call.
	cancelCtx()

	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > before {
		if time.Now().After(deadline) {
			t.Fatalf("goroutines after ctx cancel = %d, want <= %d (subscription leaked)", runtime.NumGoroutine(), before)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Calling cancel afterwards must be a harmless no-op, not a
	// double-cancel error.
	if err := cancel(); err != nil {
		t.Fatalf("cancel after ctx cancellation = %v, want nil", err)
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
