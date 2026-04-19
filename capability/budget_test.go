package capability

import (
	"context"
	"errors"
	"testing"
)

func TestBudgetAttack(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		budget         Budget
		usage          Usage
		wantBudgetKind string
	}{
		{name: "token budget", budget: Budget{Tokens: 5}, usage: Usage{TotalTokens: 5}, wantBudgetKind: "tokens"},
		{name: "tool call budget", budget: Budget{ToolCalls: 1}, usage: Usage{TotalTokens: 1}, wantBudgetKind: "tool_calls"},
		{name: "cost budget", budget: Budget{Cost: 0.25}, usage: Usage{TotalTokens: 1, Cost: 0.25}, wantBudgetKind: "cost"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			invoker := NewInvoker()
			invoker.Use(BudgetEnforcer())
			handlerCalls := 0
			invoker.Register(TypeTool, "expensive", func(context.Context, Call) (Result, error) {
				handlerCalls++
				return Result{Output: "ok", Usage: tc.usage}, nil
			})

			call := Call{Type: TypeTool, Name: "expensive", Budget: tc.budget}
			if _, err := invoker.Invoke(context.Background(), call); err != nil {
				t.Fatalf("first invoke error = %v", err)
			}
			recorder := &recordingPolicyOutcomeRecorder{}
			_, err := invoker.Invoke(WithPolicyOutcomeRecorder(context.Background(), recorder), call)
			if err == nil {
				t.Fatal("expected budget denial")
			}
			var capErr *Error
			if !errors.As(err, &capErr) || capErr.Kind != ErrorKindRateLimit {
				t.Fatalf("expected rate limit error, got %v", err)
			}
			if handlerCalls != 1 {
				t.Fatalf("expected denied budget call to skip handler, got %d", handlerCalls)
			}
			if len(recorder.events) != 1 {
				t.Fatalf("expected 1 policy outcome, got %#v", recorder.events)
			}
			if recorder.events[0].Evidence == nil || recorder.events[0].Evidence.Metadata["budget"] != tc.wantBudgetKind {
				t.Fatalf("expected budget evidence %q, got %#v", tc.wantBudgetKind, recorder.events[0].Evidence)
			}
		})
	}
}
