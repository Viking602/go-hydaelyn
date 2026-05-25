package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/tool"
)

func TestEngineRunMaxWallClockPrecedenceReturnsBudgetFailure(t *testing.T) {
	tests := []struct {
		name       string
		taskBudget time.Duration
		loopBudget time.Duration
		loopMax    time.Duration
		want       time.Duration
	}{
		{
			name:       "task budget overrides loop budgets",
			taskBudget: 50 * time.Millisecond,
			loopBudget: 10 * time.Millisecond,
			loopMax:    5 * time.Millisecond,
			want:       50 * time.Millisecond,
		},
		{
			name:       "loop budget overrides loop max",
			loopBudget: 40 * time.Millisecond,
			loopMax:    5 * time.Millisecond,
			want:       40 * time.Millisecond,
		},
		{
			name:    "loop max used when no budgets",
			loopMax: 30 * time.Millisecond,
			want:    30 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := deadlineRecordingProvider{observed: make(chan time.Duration, 1)}
			engine := Engine{
				Provider: provider,
				LoopPolicy: LoopPolicy{
					MaxWallClock: tt.loopMax,
				},
			}
			if tt.loopBudget > 0 {
				engine.LoopPolicy.Budget = &api.TaskBudget{MaxWallClock: tt.loopBudget}
			}
			task := api.Task{Goal: "wait for deadline"}
			if tt.taskBudget > 0 {
				task.Budget = &api.TaskBudget{MaxWallClock: tt.taskBudget}
			}

			ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
			defer cancel()

			started := time.Now()
			result := engine.Run(ctx, task, OutputPolicy{})
			elapsed := time.Since(started)

			if result.Failure == nil {
				t.Fatal("expected budget failure, got nil")
			}
			if result.Failure.Kind != FailureKindBudgetExhausted {
				t.Fatalf("failure kind = %s, want %s", result.Failure.Kind, FailureKindBudgetExhausted)
			}
			if !errors.Is(result.Failure, context.DeadlineExceeded) {
				t.Fatalf("failure cause = %v, want context deadline exceeded", result.Failure)
			}
			observed := <-provider.observed
			assertDurationNear(t, observed, tt.want)
			if elapsed > tt.want+100*time.Millisecond {
				t.Fatalf("Engine.Run elapsed = %s, want deadline near %s", elapsed, tt.want)
			}
		})
	}
}

func TestEngineRunContextBuildHonorsMaxWallClock(t *testing.T) {
	tests := []struct {
		name       string
		taskBudget time.Duration
		loopMax    time.Duration
		want       time.Duration
	}{
		{
			name:    "loop max wall clock bounds context build",
			loopMax: 20 * time.Millisecond,
			want:    20 * time.Millisecond,
		},
		{
			name:       "task budget max wall clock bounds context build",
			taskBudget: 30 * time.Millisecond,
			loopMax:    100 * time.Millisecond,
			want:       30 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := &blockingContextBuilder{observed: make(chan time.Duration, 1)}
			engine := Engine{
				ContextBuilder: builder,
				LoopPolicy: LoopPolicy{
					MaxWallClock: tt.loopMax,
				},
			}
			task := api.Task{Goal: "build context"}
			if tt.taskBudget > 0 {
				task.Budget = &api.TaskBudget{MaxWallClock: tt.taskBudget}
			}

			ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
			defer cancel()

			started := time.Now()
			result := engine.Run(ctx, task, OutputPolicy{})
			elapsed := time.Since(started)

			if result.Failure == nil {
				t.Fatal("expected budget failure, got nil")
			}
			if result.Failure.Kind != FailureKindBudgetExhausted {
				t.Fatalf("failure kind = %s, want %s", result.Failure.Kind, FailureKindBudgetExhausted)
			}
			if !errors.Is(result.Failure, context.DeadlineExceeded) {
				t.Fatalf("failure cause = %v, want context deadline exceeded", result.Failure)
			}
			observed := <-builder.observed
			assertDurationNear(t, observed, tt.want)
			if elapsed > tt.want+100*time.Millisecond {
				t.Fatalf("Engine.Run elapsed = %s, want context build deadline near %s", elapsed, tt.want)
			}
		})
	}
}

func TestEngineRunMessagesHonorsToolDefinitionTimeout(t *testing.T) {
	driver := &blockingTimeoutTool{
		name:     "slow",
		timeout:  10 * time.Millisecond,
		observed: make(chan time.Duration, 1),
	}
	engine := Engine{
		Provider: &scriptedProvider{
			turns: [][]provider.Event{{
				{
					Kind: provider.EventToolCall,
					ToolCall: &message.ToolCall{
						ID:   "call-1",
						Name: "slow",
					},
				},
				{Kind: provider.EventDone, StopReason: provider.StopReasonToolUse},
			}},
		},
		Tools: tool.NewBus(driver),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	started := time.Now()
	_, err := engine.RunMessages(ctx, LoopInput{
		Model:         "test-model",
		Messages:      []message.Message{message.NewText(message.RoleUser, "call slow")},
		MaxIterations: 1,
	})
	elapsed := time.Since(started)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("RunMessages() error = %v, want context deadline exceeded", err)
	}
	observed := <-driver.observed
	assertDurationNear(t, observed, driver.timeout)
	if elapsed > 100*time.Millisecond {
		t.Fatalf("RunMessages() elapsed = %s, want tool timeout near %s", elapsed, driver.timeout)
	}
}

func TestEngineRunToolDefinitionTimeoutWithActiveRunBudgetReturnsEngineError(t *testing.T) {
	driver := &blockingTimeoutTool{
		name:     "slow",
		timeout:  10 * time.Millisecond,
		observed: make(chan time.Duration, 1),
	}
	engine := Engine{
		Provider: &scriptedProvider{
			turns: [][]provider.Event{{
				{
					Kind: provider.EventToolCall,
					ToolCall: &message.ToolCall{
						ID:   "call-1",
						Name: "slow",
					},
				},
				{Kind: provider.EventDone, StopReason: provider.StopReasonToolUse},
			}},
		},
		Tools: tool.NewBus(driver),
		LoopPolicy: LoopPolicy{
			Budget: &api.TaskBudget{MaxWallClock: 250 * time.Millisecond},
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	started := time.Now()
	result := engine.Run(ctx, api.Task{Goal: "call slow"}, OutputPolicy{})
	elapsed := time.Since(started)

	if result.Failure == nil {
		t.Fatal("expected tool timeout failure, got nil")
	}
	if result.Failure.Kind != FailureKindEngineError {
		t.Fatalf("failure kind = %s, want %s", result.Failure.Kind, FailureKindEngineError)
	}
	if !errors.Is(result.Failure, context.DeadlineExceeded) {
		t.Fatalf("failure cause = %v, want context deadline exceeded", result.Failure)
	}
	if ctx.Err() != nil {
		t.Fatalf("parent context error = %v, want nil", ctx.Err())
	}
	observed := <-driver.observed
	assertDurationNear(t, observed, driver.timeout)
	if elapsed > 100*time.Millisecond {
		t.Fatalf("Engine.Run elapsed = %s, want tool timeout near %s", elapsed, driver.timeout)
	}
}

type deadlineRecordingProvider struct {
	observed chan time.Duration
}

type blockingContextBuilder struct {
	observed chan time.Duration
}

func (b *blockingContextBuilder) Build(ctx context.Context, _ api.Task) ([]message.Message, error) {
	recordObservedDeadline(ctx, b.observed)
	<-ctx.Done()
	return nil, ctx.Err()
}

func (*blockingContextBuilder) Compact(_ context.Context, history []message.Message) ([]message.Message, error) {
	return history, nil
}

func (deadlineRecordingProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "deadline-recording"}
}

func (p deadlineRecordingProvider) Stream(ctx context.Context, _ provider.Request) (provider.Stream, error) {
	recordObservedDeadline(ctx, p.observed)
	<-ctx.Done()
	return nil, ctx.Err()
}

type blockingTimeoutTool struct {
	name     string
	timeout  time.Duration
	observed chan time.Duration
}

func (t *blockingTimeoutTool) Definition() tool.Definition {
	return tool.Definition{
		Name:        t.name,
		InputSchema: tool.Schema{Type: "object"},
		Timeout:     t.timeout,
	}
}

func (t *blockingTimeoutTool) Execute(ctx context.Context, _ tool.Call, _ tool.UpdateSink) (tool.Result, error) {
	recordObservedDeadline(ctx, t.observed)
	<-ctx.Done()
	return tool.Result{}, ctx.Err()
}

func recordObservedDeadline(ctx context.Context, observed chan<- time.Duration) {
	deadline, ok := ctx.Deadline()
	if !ok {
		observed <- 0
		return
	}
	observed <- time.Until(deadline)
}

func assertDurationNear(t *testing.T, got, want time.Duration) {
	t.Helper()
	if got < want/2 || got > want+75*time.Millisecond {
		t.Fatalf("deadline = %s, want near %s", got, want)
	}
}
