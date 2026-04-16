package middleware

import (
	"context"
	"testing"

	"github.com/Viking602/go-hydaelyn/provider"
)

func TestChainRunsInOrder(t *testing.T) {
	trace := make([]string, 0, 5)
	chain := NewChain(
		Func(func(ctx context.Context, envelope *Envelope, next Next) error {
			trace = append(trace, "first:before:"+string(envelope.Stage))
			if err := next(ctx, envelope); err != nil {
				return err
			}
			trace = append(trace, "first:after:"+string(envelope.Stage))
			return nil
		}),
		Func(func(ctx context.Context, envelope *Envelope, next Next) error {
			trace = append(trace, "second:before:"+string(envelope.Stage))
			if err := next(ctx, envelope); err != nil {
				return err
			}
			trace = append(trace, "second:after:"+string(envelope.Stage))
			return nil
		}),
	)
	err := chain.Handle(context.Background(), &Envelope{
		Stage:     StageTeam,
		Operation: "start",
	}, func(_ context.Context, envelope *Envelope) error {
		trace = append(trace, "terminal:"+envelope.Operation)
		return nil
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	want := []string{
		"first:before:team",
		"second:before:team",
		"terminal:start",
		"second:after:team",
		"first:after:team",
	}
	if len(trace) != len(want) {
		t.Fatalf("unexpected trace: %#v", trace)
	}
	for idx := range want {
		if trace[idx] != want[idx] {
			t.Fatalf("trace[%d] = %q, want %q", idx, trace[idx], want[idx])
		}
	}
}

func TestHookAdapterMapsLLMAndToolStages(t *testing.T) {
	trace := make([]string, 0, 4)
	chain := NewChain(Func(func(ctx context.Context, envelope *Envelope, next Next) error {
		trace = append(trace, string(envelope.Stage)+":"+envelope.Operation)
		return next(ctx, envelope)
	}))
	adapter := chain.HookAdapter()
	if err := adapter.BeforeModelCall(context.Background(), nil); err != nil {
		t.Fatalf("BeforeModelCall() error = %v", err)
	}
	if err := adapter.BeforeToolCall(context.Background(), nil); err != nil {
		t.Fatalf("BeforeToolCall() error = %v", err)
	}
	if err := adapter.AfterToolCall(context.Background(), nil); err != nil {
		t.Fatalf("AfterToolCall() error = %v", err)
	}
	if err := adapter.OnEvent(context.Background(), provider.Event{}); err != nil {
		t.Fatalf("OnEvent() error = %v", err)
	}
	want := []string{
		"llm:before",
		"tool:before",
		"tool:after",
		"llm:event",
	}
	for idx := range want {
		if trace[idx] != want[idx] {
			t.Fatalf("trace[%d] = %q, want %q", idx, trace[idx], want[idx])
		}
	}
}
