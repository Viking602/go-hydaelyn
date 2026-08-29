package tool

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/Viking602/venat/message"
)

type updateTestDriver struct {
	name    string
	execute func(context.Context, Call, UpdateSink) (Result, error)
}

func (driver updateTestDriver) Definition() Definition {
	return Definition{Name: driver.name}
}

func (driver updateTestDriver) Execute(ctx context.Context, call Call, sink UpdateSink) (Result, error) {
	return driver.execute(ctx, call, sink)
}

func TestBusExecute_OrdersEnrichesAndAccumulatesUpdates(t *testing.T) {
	data := map[string]string{"phase": "start"}
	parts := []message.ContentPart{message.TextPart("first")}
	driver := updateTestDriver{name: "stream", execute: func(_ context.Context, _ Call, sink UpdateSink) (Result, error) {
		if err := sink(Update{
			Kind:        UpdateProgress,
			ToolCallID:  "untrusted",
			OperationID: "untrusted",
			Sequence:    99,
			Message:     "starting",
			Data:        data,
		}); err != nil {
			return Result{}, err
		}
		data["phase"] = "mutated"
		if err := sink(Update{Kind: UpdateOutput, Parts: parts}); err != nil {
			return Result{}, err
		}
		parts[0].Text = "mutated"
		if err := sink(Update{Kind: UpdateOutput, Parts: []message.ContentPart{message.TextPart(" second")}}); err != nil {
			return Result{}, err
		}
		return Result{
			ToolCallID: "wrong",
			Name:       "wrong",
			Structured: json.RawMessage(`{"status":"ok"}`),
		}, nil
	}}

	var updates []Update
	result, err := NewBus(driver).Execute(context.Background(), Call{
		ID:          "call-1",
		Name:        "stream",
		OperationID: "turn:0:call:0",
	}, ExecuteOptions{Sink: func(update Update) error {
		updates = append(updates, CloneUpdate(update))
		return nil
	}})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(updates) != 3 {
		t.Fatalf("len(updates) = %d, want 3", len(updates))
	}
	for index, update := range updates {
		if update.ToolCallID != "call-1" || update.OperationID != "turn:0:call:0" || update.Sequence != uint64(index+1) {
			t.Fatalf("update[%d] identity = %#v", index, update)
		}
	}
	if updates[0].Kind != UpdateProgress || updates[0].Data["phase"] != "start" {
		t.Fatalf("progress update = %#v", updates[0])
	}
	if updates[1].Kind != UpdateOutput || updates[1].Parts[0].Text != "first" || updates[2].Parts[0].Text != " second" {
		t.Fatalf("output updates = %#v", updates[1:])
	}
	if result.ToolCallID != "call-1" || result.Name != "stream" || result.Content != "first second" {
		t.Fatalf("normalized result = %#v", result)
	}
	if len(result.Parts) != 2 || result.Parts[0].Text != "first" || result.Parts[1].Text != " second" {
		t.Fatalf("result parts = %#v", result.Parts)
	}
	if string(result.Structured) != `{"status":"ok"}` {
		t.Fatalf("result structured = %s", result.Structured)
	}
}

func TestBusExecute_ValidatesTerminalResultAndBusinessError(t *testing.T) {
	t.Run("matching business error", func(t *testing.T) {
		part := message.TextPart("rejected")
		driver := updateTestDriver{name: "action", execute: func(_ context.Context, _ Call, sink UpdateSink) (Result, error) {
			if err := sink(Update{Kind: UpdateOutput, Parts: []message.ContentPart{part}}); err != nil {
				return Result{}, err
			}
			return Result{Parts: []message.ContentPart{part}, IsError: true}, nil
		}}
		result, err := NewBus(driver).Execute(context.Background(), Call{ID: "call-1", Name: "action"}, ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !result.IsError || result.Content != "rejected" {
			t.Fatalf("result = %#v", result)
		}
	})

	t.Run("mismatched final output", func(t *testing.T) {
		driver := updateTestDriver{name: "action", execute: func(_ context.Context, _ Call, sink UpdateSink) (Result, error) {
			if err := sink(Update{Kind: UpdateOutput, Parts: []message.ContentPart{message.TextPart("streamed")}}); err != nil {
				return Result{}, err
			}
			return Result{Parts: []message.ContentPart{message.TextPart("different")}}, nil
		}}
		_, err := NewBus(driver).Execute(context.Background(), Call{ID: "call-1", Name: "action"}, ExecuteOptions{})
		if !errors.Is(err, ErrToolUpdateProtocol) {
			t.Fatalf("Execute() error = %v, want ErrToolUpdateProtocol", err)
		}
	})

	t.Run("update contradicts not executed", func(t *testing.T) {
		driver := updateTestDriver{name: "action", execute: func(_ context.Context, _ Call, sink UpdateSink) (Result, error) {
			if err := sink(Update{Kind: UpdateProgress, Message: "started"}); err != nil {
				return Result{}, err
			}
			return Result{}, ErrNotExecuted
		}}
		_, err := NewBus(driver).Execute(context.Background(), Call{ID: "call-1", Name: "action"}, ExecuteOptions{})
		if !errors.Is(err, ErrToolUpdateProtocol) || errors.Is(err, ErrNotExecuted) {
			t.Fatalf("Execute() error = %v, want protocol error without ErrNotExecuted", err)
		}
	})
}

func TestBusExecute_RejectsInvalidAndExcessiveUpdates(t *testing.T) {
	for name, update := range map[string]Update{
		"unknown kind":         {Kind: UpdateKind("unknown")},
		"progress with parts":  {Kind: UpdateProgress, Parts: []message.ContentPart{message.TextPart("invalid")}},
		"output without parts": {Kind: UpdateOutput},
	} {
		t.Run(name, func(t *testing.T) {
			driver := updateTestDriver{name: "invalid", execute: func(_ context.Context, _ Call, sink UpdateSink) (Result, error) {
				_ = sink(update)
				return Result{}, nil
			}}
			_, err := NewBus(driver).Execute(context.Background(), Call{ID: "call-1", Name: "invalid"}, ExecuteOptions{})
			if !errors.Is(err, ErrToolUpdateProtocol) || errors.Is(err, ErrNotExecuted) {
				t.Fatalf("Execute() error = %v, want protocol error after execution", err)
			}
		})
	}

	t.Run("count", func(t *testing.T) {
		driver := updateTestDriver{name: "many", execute: func(_ context.Context, _ Call, sink UpdateSink) (Result, error) {
			for range maxToolUpdatesPerCall + 1 {
				if err := sink(Update{Kind: UpdateProgress}); err != nil {
					break
				}
			}
			return Result{}, nil
		}}
		_, err := NewBus(driver).Execute(context.Background(), Call{ID: "call-1", Name: "many"}, ExecuteOptions{})
		if !errors.Is(err, ErrToolUpdateLimit) || errors.Is(err, ErrNotExecuted) {
			t.Fatalf("Execute() error = %v, want ErrToolUpdateLimit", err)
		}
	})

	t.Run("decoded bytes", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		state := &toolUpdateState{
			call:   Call{ID: "call-1", Name: "large", OperationID: "turn:0:call:0"},
			cancel: cancel,
			bytes:  maxToolUpdateBytesPerCall - 1,
		}
		err := state.emit(Update{Kind: UpdateProgress, Message: "x"})
		if !errors.Is(err, ErrToolUpdateLimit) || ctx.Err() != context.Canceled {
			t.Fatalf("emit() error = %v, context = %v", err, ctx.Err())
		}
	})
}

func TestBusExecute_AppliesSynchronousBackpressureAndSinkFailure(t *testing.T) {
	t.Run("backpressure", func(t *testing.T) {
		entered := make(chan struct{})
		release := make(chan struct{})
		driver := updateTestDriver{name: "wait", execute: func(_ context.Context, _ Call, sink UpdateSink) (Result, error) {
			if err := sink(Update{Kind: UpdateProgress, Message: "waiting"}); err != nil {
				return Result{}, err
			}
			return Result{Content: "done"}, nil
		}}
		done := make(chan error, 1)
		go func() {
			_, err := NewBus(driver).Execute(context.Background(), Call{ID: "call-1", Name: "wait"}, ExecuteOptions{Sink: func(Update) error {
				close(entered)
				<-release
				return nil
			}})
			done <- err
		}()
		<-entered
		select {
		case err := <-done:
			t.Fatalf("Execute() returned before sink released: %v", err)
		default:
		}
		close(release)
		if err := <-done; err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
	})

	t.Run("error cancels child even when ignored", func(t *testing.T) {
		sinkErr := errors.New("sink failed")
		canceled := make(chan struct{})
		driver := updateTestDriver{name: "ignore", execute: func(ctx context.Context, _ Call, sink UpdateSink) (Result, error) {
			_ = sink(Update{Kind: UpdateProgress, Message: "started"})
			<-ctx.Done()
			close(canceled)
			return Result{Content: "ignored"}, nil
		}}
		_, err := NewBus(driver).Execute(context.Background(), Call{ID: "call-1", Name: "ignore"}, ExecuteOptions{Sink: func(Update) error {
			return sinkErr
		}})
		if !errors.Is(err, sinkErr) || errors.Is(err, ErrNotExecuted) {
			t.Fatalf("Execute() error = %v, want sink error without ErrNotExecuted", err)
		}
		select {
		case <-canceled:
		default:
			t.Fatal("driver child context was not canceled")
		}
	})
}

func TestBusExecuteBatch_SerializesSharedUpdateSink(t *testing.T) {
	attempted := make(chan struct{}, 2)
	release := make(chan struct{})
	newDriver := func(name string) Driver {
		return updateTestDriver{name: name, execute: func(_ context.Context, call Call, sink UpdateSink) (Result, error) {
			attempted <- struct{}{}
			if err := sink(Update{Kind: UpdateProgress, Message: call.Name}); err != nil {
				return Result{}, err
			}
			return Result{Content: call.Name}, nil
		}}
	}
	bus := NewBus(newDriver("first"), newDriver("second"))
	var active atomic.Int32
	var maximum atomic.Int32
	var updates []Update
	done := make(chan error, 1)
	go func() {
		_, err := bus.ExecuteBatch(context.Background(), []Call{
			{ID: "call-1", Name: "first", OperationID: "op-1"},
			{ID: "call-2", Name: "second", OperationID: "op-2"},
		}, ModeParallel, ExecuteOptions{Sink: func(update Update) error {
			current := active.Add(1)
			for observed := maximum.Load(); current > observed; observed = maximum.Load() {
				if maximum.CompareAndSwap(observed, current) {
					break
				}
			}
			updates = append(updates, CloneUpdate(update))
			<-release
			active.Add(-1)
			return nil
		}})
		done <- err
	}()
	<-attempted
	<-attempted
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("ExecuteBatch() error = %v", err)
	}
	if maximum.Load() != 1 {
		t.Fatalf("maximum concurrent sink calls = %d, want 1", maximum.Load())
	}
	if len(updates) != 2 {
		t.Fatalf("len(updates) = %d, want 2", len(updates))
	}
	for _, update := range updates {
		if update.Sequence != 1 || update.OperationID == "" || update.ToolCallID == "" {
			t.Fatalf("parallel update identity = %#v", update)
		}
	}
}

func TestBusExecute_ClosesUpdatesAndNormalizesBeforeInterceptorReturns(t *testing.T) {
	var late UpdateSink
	driver := updateTestDriver{name: "lifecycle", execute: func(_ context.Context, _ Call, sink UpdateSink) (Result, error) {
		late = sink
		if err := sink(Update{Kind: UpdateOutput, Parts: []message.ContentPart{message.TextPart("complete")}}); err != nil {
			return Result{}, err
		}
		return Result{}, nil
	}}
	var intercepted Result
	interceptor := InterceptorFunc(func(ctx context.Context, next Driver, call Call, sink UpdateSink) (Result, error) {
		result, err := next.Execute(ctx, call, sink)
		intercepted = message.CloneToolResult(result)
		return result, err
	})
	var delivered int
	result, err := NewBus(driver).Execute(context.Background(), Call{ID: "call-1", Name: "lifecycle"}, ExecuteOptions{
		Interceptor: interceptor,
		Sink: func(Update) error {
			delivered++
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if intercepted.Content != "complete" || len(intercepted.Parts) != 1 || result.Content != "complete" {
		t.Fatalf("intercepted = %#v, result = %#v", intercepted, result)
	}
	if err := late(Update{Kind: UpdateProgress, Message: "late"}); !errors.Is(err, ErrToolUpdateProtocol) {
		t.Fatalf("late update error = %v, want ErrToolUpdateProtocol", err)
	}
	if delivered != 1 {
		t.Fatalf("delivered updates = %d, want 1", delivered)
	}
}
