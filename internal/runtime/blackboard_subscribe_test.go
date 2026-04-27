package runtime

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSubscribeStreamsMatchingItems(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run := mustStartRun(t, ctx, rt, "run-sub")

	ch, cancel, err := rt.Subscribe(ctx, run.ID, BlackboardFilter{ItemTypes: []BlackboardItemType{BlackboardItemEvidence}})
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	defer func() { _ = cancel() }()

	// non-matching write — must not arrive
	if err := rt.WriteItem(ctx, BlackboardItem{RunID: run.ID, TaskID: "t", Type: BlackboardItemClaim, Source: SourceIdentity{Type: SourceAgent, ID: "a"}, Visibility: BlackboardVisibilityAgentVisible, Payload: "skip"}); err != nil {
		t.Fatalf("WriteItem(claim) error = %v", err)
	}
	if err := rt.WriteItem(ctx, BlackboardItem{RunID: run.ID, TaskID: "t", Type: BlackboardItemEvidence, Source: SourceIdentity{Type: SourceAgent, ID: "a"}, Visibility: BlackboardVisibilityAgentVisible, Payload: "match"}); err != nil {
		t.Fatalf("WriteItem(evidence) error = %v", err)
	}

	select {
	case got := <-ch:
		if got.Type != BlackboardItemEvidence || got.Payload != "match" {
			t.Fatalf("subscriber got wrong item: %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatalf("subscriber did not receive matching item")
	}

	// drain — should be empty since claim was filtered out
	select {
	case got := <-ch:
		t.Fatalf("subscriber received non-matching item: %#v", got)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestSubscribeCancelClosesChannel(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run := mustStartRun(t, ctx, rt, "run-sub-cancel")
	ch, cancel, err := rt.Subscribe(ctx, run.ID, BlackboardFilter{})
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	if err := cancel(); err != nil {
		t.Fatalf("cancel() error = %v", err)
	}
	if _, ok := <-ch; ok {
		t.Fatalf("expected channel closed after cancel")
	}
	if err := cancel(); !errors.Is(err, ErrSubscriptionClosed) {
		t.Fatalf("double cancel should return ErrSubscriptionClosed, got %v", err)
	}
}

func TestWaitForBlackboardReplaysAndStreams(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run := mustStartRun(t, ctx, rt, "run-wait")

	// Existing item before Wait — should be replayed
	if err := rt.WriteItem(ctx, BlackboardItem{RunID: run.ID, TaskID: "t1", Type: BlackboardItemEvidence, Source: SourceIdentity{Type: SourceAgent, ID: "a"}, Visibility: BlackboardVisibilityAgentVisible, Payload: "p1"}); err != nil {
		t.Fatalf("WriteItem(p1) error = %v", err)
	}

	// Concurrent writer adds a second item shortly after Wait starts
	go func() {
		time.Sleep(10 * time.Millisecond)
		_ = rt.WriteItem(ctx, BlackboardItem{RunID: run.ID, TaskID: "t2", Type: BlackboardItemEvidence, Source: SourceIdentity{Type: SourceAgent, ID: "b"}, Visibility: BlackboardVisibilityAgentVisible, Payload: "p2"})
	}()

	got, err := rt.WaitForBlackboard(ctx, run.ID,
		BlackboardFilter{ItemTypes: []BlackboardItemType{BlackboardItemEvidence}},
		func(items []BlackboardItem) bool { return len(items) >= 2 },
		time.Second,
	)
	if err != nil {
		t.Fatalf("WaitForBlackboard() error = %v", err)
	}
	if len(got) != 2 || got[0].Payload != "p1" || got[1].Payload != "p2" {
		t.Fatalf("unexpected items: %#v", got)
	}
}

func TestWaitForBlackboardTimeout(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run := mustStartRun(t, ctx, rt, "run-wait-timeout")
	_, err := rt.WaitForBlackboard(ctx, run.ID, BlackboardFilter{}, func([]BlackboardItem) bool { return false }, 30*time.Millisecond)
	if !errors.Is(err, ErrWaitTimeout) {
		t.Fatalf("expected ErrWaitTimeout, got %v", err)
	}
}

func TestWaitForBlackboardSatisfiedByExisting(t *testing.T) {
	ctx := context.Background()
	rt := NewMemoryRuntime()
	run := mustStartRun(t, ctx, rt, "run-wait-existing")
	if err := rt.WriteItem(ctx, BlackboardItem{RunID: run.ID, TaskID: "t", Type: BlackboardItemClaim, Source: SourceIdentity{Type: SourceAgent, ID: "a"}, Visibility: BlackboardVisibilityAgentVisible, Payload: "ready"}); err != nil {
		t.Fatalf("WriteItem() error = %v", err)
	}
	got, err := rt.WaitForBlackboard(ctx, run.ID, BlackboardFilter{}, func(items []BlackboardItem) bool { return len(items) > 0 }, time.Second)
	if err != nil {
		t.Fatalf("WaitForBlackboard() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 existing item, got %d", len(got))
	}
}
