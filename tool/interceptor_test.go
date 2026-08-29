package tool

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

type interceptorTestTool struct {
	calls    int
	received []Call
}

func (*interceptorTestTool) Definition() Definition {
	return Definition{Name: "lookup", InputSchema: Schema{Type: "object"}}
}

func (driver *interceptorTestTool) Execute(_ context.Context, call Call, _ UpdateSink) (Result, error) {
	driver.calls++
	driver.received = append(driver.received, call)
	return Result{ToolCallID: call.ID, Name: call.Name, Content: "ok"}, nil
}

func interceptorTestCall() Call {
	return Call{ID: "call-1", Name: "lookup", Arguments: json.RawMessage(`{"query":"venat"}`), OperationID: "turn:0:call:0"}
}

func TestChainInterceptors_OrderAndSingleExecution(t *testing.T) {
	driver := &interceptorTestTool{}
	var order []string
	outer := InterceptorFunc(func(ctx context.Context, next Driver, call Call, sink UpdateSink) (Result, error) {
		order = append(order, "outer-before")
		result, err := next.Execute(ctx, call, sink)
		order = append(order, "outer-after")
		return result, err
	})
	inner := InterceptorFunc(func(ctx context.Context, next Driver, call Call, sink UpdateSink) (Result, error) {
		order = append(order, "inner-before")
		result, err := next.Execute(ctx, call, sink)
		order = append(order, "inner-after")
		return result, err
	})

	result, err := ChainInterceptors(outer, nil, inner).Execute(context.Background(), driver, interceptorTestCall(), nil)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Content != "ok" {
		t.Fatalf("result = %#v, want content ok", result)
	}
	if driver.calls != 1 {
		t.Fatalf("underlying calls = %d, want 1", driver.calls)
	}
	wantOrder := []string{"outer-before", "inner-before", "inner-after", "outer-after"}
	if !reflect.DeepEqual(order, wantOrder) {
		t.Fatalf("order = %v, want %v", order, wantOrder)
	}
}

func TestInterceptorIsolatesMutableCallArguments(t *testing.T) {
	driver := &interceptorTestTool{}
	call := interceptorTestCall()
	wantArguments := string(call.Arguments)
	interceptor := ChainInterceptors(InterceptorFunc(func(ctx context.Context, next Driver, current Call, sink UpdateSink) (Result, error) {
		result, err := next.Execute(ctx, current, sink)
		current.Arguments[0] = '['
		return result, err
	}))

	if _, err := interceptor.Execute(context.Background(), driver, call, nil); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if string(call.Arguments) != wantArguments {
		t.Fatalf("caller arguments = %q, want %q", call.Arguments, wantArguments)
	}
	if len(driver.received) != 1 || string(driver.received[0].Arguments) != wantArguments {
		t.Fatalf("driver arguments = %#v, want %q", driver.received, wantArguments)
	}
}

func TestInterceptorRejectsMutationBeforeEffect(t *testing.T) {
	driver := &interceptorTestTool{}
	interceptor := ChainInterceptors(InterceptorFunc(func(ctx context.Context, next Driver, call Call, sink UpdateSink) (Result, error) {
		call.Name = "changed"
		return next.Execute(ctx, call, sink)
	}))

	_, err := interceptor.Execute(context.Background(), driver, interceptorTestCall(), nil)
	if !errors.Is(err, ErrInterceptorProtocol) || !errors.Is(err, ErrNotExecuted) {
		t.Fatalf("Execute() error = %v, want protocol and not-executed errors", err)
	}
	if driver.calls != 0 {
		t.Fatalf("underlying calls = %d, want 0", driver.calls)
	}
}

func TestInterceptorRejectsSecondEffect(t *testing.T) {
	driver := &interceptorTestTool{}
	interceptor := ChainInterceptors(InterceptorFunc(func(ctx context.Context, next Driver, call Call, sink UpdateSink) (Result, error) {
		if _, err := next.Execute(ctx, call, sink); err != nil {
			return Result{}, err
		}
		return next.Execute(ctx, call, sink)
	}))

	_, err := interceptor.Execute(context.Background(), driver, interceptorTestCall(), nil)
	if !errors.Is(err, ErrInterceptorProtocol) {
		t.Fatalf("Execute() error = %v, want protocol error", err)
	}
	if errors.Is(err, ErrNotExecuted) {
		t.Fatalf("Execute() error = %v, must not claim the first effect was not executed", err)
	}
	if driver.calls != 1 {
		t.Fatalf("underlying calls = %d, want 1", driver.calls)
	}
}

func TestInterceptorPanicPreservesExecutionCertainty(t *testing.T) {
	tests := []struct {
		name            string
		interceptor     Interceptor
		wantCalls       int
		wantNotExecuted bool
	}{
		{
			name: "before next",
			interceptor: InterceptorFunc(func(context.Context, Driver, Call, UpdateSink) (Result, error) {
				panic("before")
			}),
			wantNotExecuted: true,
		},
		{
			name: "after next",
			interceptor: InterceptorFunc(func(ctx context.Context, next Driver, call Call, sink UpdateSink) (Result, error) {
				_, _ = next.Execute(ctx, call, sink)
				panic("after")
			}),
			wantCalls: 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			driver := &interceptorTestTool{}
			_, err := ChainInterceptors(test.interceptor).Execute(context.Background(), driver, interceptorTestCall(), nil)
			if !errors.Is(err, ErrInterceptorProtocol) {
				t.Fatalf("Execute() error = %v, want protocol error", err)
			}
			if errors.Is(err, ErrNotExecuted) != test.wantNotExecuted {
				t.Fatalf("errors.Is(ErrNotExecuted) = %v, want %v", errors.Is(err, ErrNotExecuted), test.wantNotExecuted)
			}
			if driver.calls != test.wantCalls {
				t.Fatalf("underlying calls = %d, want %d", driver.calls, test.wantCalls)
			}
		})
	}
}

func TestInterceptorRejectsLateEffect(t *testing.T) {
	driver := &interceptorTestTool{}
	var late Driver
	interceptor := ChainInterceptors(InterceptorFunc(func(_ context.Context, next Driver, call Call, _ UpdateSink) (Result, error) {
		late = next
		return Result{ToolCallID: call.ID, Name: call.Name, Content: "synthetic"}, nil
	}))
	call := interceptorTestCall()
	if _, err := interceptor.Execute(context.Background(), driver, call, nil); err != nil {
		t.Fatalf("synthetic Execute() error = %v", err)
	}
	_, err := late.Execute(context.Background(), call, nil)
	if !errors.Is(err, ErrInterceptorProtocol) || !errors.Is(err, ErrNotExecuted) {
		t.Fatalf("late Execute() error = %v, want protocol and not-executed errors", err)
	}
	if driver.calls != 0 {
		t.Fatalf("underlying calls = %d, want 0", driver.calls)
	}
}
