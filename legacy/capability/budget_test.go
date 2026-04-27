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

func TestBudgetScopePrincipalIsolatedBetweenPrincipals(t *testing.T) {
	t.Parallel()

	invoker := NewInvoker()
	invoker.Use(BudgetEnforcer())
	invoker.Register(TypeLLM, "expensive", func(context.Context, Call) (Result, error) {
		return Result{Output: "ok", Usage: Usage{TotalTokens: 5}}, nil
	})
	call := Call{
		Type:   TypeLLM,
		Name:   "expensive",
		Budget: Budget{Tokens: 5, Scope: BudgetScopePrincipal},
	}
	aliceCtx := WithSecurityContext(context.Background(), SecurityContext{Principal: "alice"})
	bobCtx := WithSecurityContext(context.Background(), SecurityContext{Principal: "bob"})

	if _, err := invoker.Invoke(aliceCtx, call); err != nil {
		t.Fatalf("alice first invoke error = %v", err)
	}
	if _, err := invoker.Invoke(bobCtx, call); err != nil {
		t.Fatalf("bob first invoke error = %v", err)
	}
	if _, err := invoker.Invoke(aliceCtx, call); err == nil {
		t.Fatal("expected alice to exhaust her own principal-scoped budget")
	}
	if _, err := invoker.Invoke(bobCtx, call); err == nil {
		t.Fatal("expected bob to exhaust his own principal-scoped budget independently")
	}
}

func TestBudgetScopeDefaultsToRunWhenRunIDPresent(t *testing.T) {
	t.Parallel()

	invoker := NewInvoker()
	invoker.Use(BudgetEnforcer())
	invoker.Register(TypeLLM, "expensive", func(context.Context, Call) (Result, error) {
		return Result{Output: "ok", Usage: Usage{TotalTokens: 5}}, nil
	})
	call := Call{
		Type:   TypeLLM,
		Name:   "expensive",
		Budget: Budget{Tokens: 5},
	}

	firstRun := context.Background()
	secondRun := context.Background()

	if _, err := invoker.Invoke(firstRun, withMetadata(call, map[string]string{"runId": "run-1"})); err != nil {
		t.Fatalf("run-1 first invoke error = %v", err)
	}
	if _, err := invoker.Invoke(secondRun, withMetadata(call, map[string]string{"runId": "run-2"})); err != nil {
		t.Fatalf("run-2 first invoke error = %v", err)
	}
	if _, err := invoker.Invoke(firstRun, withMetadata(call, map[string]string{"runId": "run-1"})); err == nil {
		t.Fatal("expected run-1 budget exhaustion on second invoke")
	}
	if _, err := invoker.Invoke(secondRun, withMetadata(call, map[string]string{"runId": "run-2"})); err == nil {
		t.Fatal("expected run-2 budget exhaustion on second invoke")
	}
}

func withMetadata(call Call, metadata map[string]string) Call {
	call.Metadata = metadata
	return call
}
