package capability

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestInvokerAppliesRetryMiddleware(t *testing.T) {
	invoker := NewInvoker()
	attempts := 0
	invoker.Register(TypeTool, "search", func(context.Context, Call) (Result, error) {
		attempts++
		if attempts == 1 {
			return Result{}, errors.New("transient")
		}
		return Result{Output: "ok"}, nil
	})
	invoker.Use(Retry(2))
	result, err := invoker.Invoke(context.Background(), Call{
		Type:       TypeTool,
		Name:       "search",
		MaxRetries: 1,
	})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if result.Output != "ok" {
		t.Fatalf("unexpected output %#v", result)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
}

func TestInvokerAppliesPermissionMiddleware(t *testing.T) {
	invoker := NewInvoker()
	invoker.Register(TypeTool, "search", func(context.Context, Call) (Result, error) {
		return Result{Output: "ok"}, nil
	})
	invoker.Use(RequirePermissions())
	_, err := invoker.Invoke(context.Background(), Call{
		Type:        TypeTool,
		Name:        "search",
		Permissions: []Permission{{Name: "tool:search"}},
	})
	if err == nil {
		t.Fatalf("expected permission error")
	}
	var capErr *Error
	if !errors.As(err, &capErr) || capErr.Kind != ErrorKindPermission {
		t.Fatalf("expected permission error kind, got %v", err)
	}
}

func TestInvokerReturnsTypedTimeoutError(t *testing.T) {
	invoker := NewInvoker()
	invoker.Register(TypeLLM, "fake", func(ctx context.Context, call Call) (Result, error) {
		<-ctx.Done()
		return Result{}, ctx.Err()
	})
	_, err := invoker.Invoke(context.Background(), Call{
		Type:    TypeLLM,
		Name:    "fake",
		Timeout: 10 * time.Millisecond,
	})
	if err == nil {
		t.Fatalf("expected timeout error")
	}
	var capErr *Error
	if !errors.As(err, &capErr) || capErr.Kind != ErrorKindTimeout {
		t.Fatalf("expected timeout error kind, got %v", err)
	}
}

func TestInvokerAppliesApprovalMiddleware(t *testing.T) {
	invoker := NewInvoker()
	invoker.Register(TypeTool, "deploy", func(context.Context, Call) (Result, error) {
		return Result{Output: "ok"}, nil
	})
	invoker.Use(RequireApproval())
	_, err := invoker.Invoke(context.Background(), Call{
		Type: TypeTool,
		Name: "deploy",
	})
	if err == nil {
		t.Fatalf("expected approval error")
	}
	var capErr *Error
	if !errors.As(err, &capErr) || capErr.Kind != ErrorKindApproval {
		t.Fatalf("expected approval error kind, got %v", err)
	}

	result, err := invoker.Invoke(context.Background(), Call{
		Type: TypeTool,
		Name: "deploy",
		Metadata: map[string]string{
			"approved": "true",
		},
	})
	if err != nil {
		t.Fatalf("expected approved call to pass, got %v", err)
	}
	if result.Output != "ok" {
		t.Fatalf("unexpected output %#v", result)
	}
}

func TestInvokerAppliesRateLimitMiddleware(t *testing.T) {
	invoker := NewInvoker()
	invoker.Register(TypeTool, "search", func(context.Context, Call) (Result, error) {
		return Result{Output: "ok"}, nil
	})
	invoker.Use(RateLimit(1))
	if _, err := invoker.Invoke(context.Background(), Call{Type: TypeTool, Name: "search"}); err != nil {
		t.Fatalf("first call should pass, got %v", err)
	}
	_, err := invoker.Invoke(context.Background(), Call{Type: TypeTool, Name: "search"})
	if err == nil {
		t.Fatalf("expected rate limit error")
	}
	var capErr *Error
	if !errors.As(err, &capErr) || capErr.Kind != ErrorKindRateLimit {
		t.Fatalf("expected rate limit error kind, got %v", err)
	}
}

func TestRateLimitPerWindowResetsAfterWindow(t *testing.T) {
	invoker := NewInvoker()
	invoker.Register(TypeTool, "search", func(context.Context, Call) (Result, error) {
		return Result{Output: "ok"}, nil
	})
	invoker.Use(RateLimitPerWindow(1, 50*time.Millisecond))
	if _, err := invoker.Invoke(context.Background(), Call{Type: TypeTool, Name: "search"}); err != nil {
		t.Fatalf("first call should pass, got %v", err)
	}
	if _, err := invoker.Invoke(context.Background(), Call{Type: TypeTool, Name: "search"}); err == nil {
		t.Fatalf("expected rate limit error within window")
	}
	time.Sleep(60 * time.Millisecond)
	if _, err := invoker.Invoke(context.Background(), Call{Type: TypeTool, Name: "search"}); err != nil {
		t.Fatalf("call after window should pass, got %v", err)
	}
}

func TestBudgetEnforcerBlocksWhenTokenBudgetExhausted(t *testing.T) {
	invoker := NewInvoker()
	invoker.Register(TypeLLM, "fake", func(context.Context, Call) (Result, error) {
		return Result{Output: "ok", Usage: Usage{TotalTokens: 500, Cost: 0.01}}, nil
	})
	invoker.Use(BudgetEnforcer())
	call := Call{Type: TypeLLM, Name: "fake", Budget: Budget{Tokens: 500}}
	if _, err := invoker.Invoke(context.Background(), call); err != nil {
		t.Fatalf("first call should pass, got %v", err)
	}
	_, err := invoker.Invoke(context.Background(), call)
	if err == nil {
		t.Fatalf("expected budget exhausted error")
	}
	var capErr *Error
	if !errors.As(err, &capErr) {
		t.Fatalf("expected capability error, got %v", err)
	}
	if capErr.Kind != ErrorKindRateLimit {
		t.Fatalf("expected rate_limit error kind for budget, got %s", capErr.Kind)
	}
}

func TestBudgetEnforcerBlocksWhenCostBudgetExhausted(t *testing.T) {
	invoker := NewInvoker()
	invoker.Register(TypeLLM, "fake", func(context.Context, Call) (Result, error) {
		return Result{Output: "ok", Usage: Usage{TotalTokens: 100, Cost: 0.05}}, nil
	})
	invoker.Use(BudgetEnforcer())
	call := Call{Type: TypeLLM, Name: "fake", Budget: Budget{Cost: 0.05}}
	if _, err := invoker.Invoke(context.Background(), call); err != nil {
		t.Fatalf("first call should pass, got %v", err)
	}
	_, err := invoker.Invoke(context.Background(), call)
	if err == nil {
		t.Fatalf("expected cost budget exhausted error")
	}
}

func TestRetrySkipsNonTemporaryErrors(t *testing.T) {
	invoker := NewInvoker()
	attempts := 0
	invoker.Register(TypeTool, "deploy", func(context.Context, Call) (Result, error) {
		attempts++
		return Result{}, &Error{Kind: ErrorKindPermission, Message: "denied", Temporary: false}
	})
	invoker.Use(Retry(3))
	_, err := invoker.Invoke(context.Background(), Call{Type: TypeTool, Name: "deploy"})
	if err == nil {
		t.Fatalf("expected error")
	}
	if attempts != 1 {
		t.Fatalf("expected 1 attempt for non-temporary error, got %d", attempts)
	}
}
