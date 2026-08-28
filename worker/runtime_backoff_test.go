package worker

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Viking602/venat/api"
)

// recordSleeps replaces the runtime delay hook with one that records the
// requested durations instead of waiting, and stops the loop once stopAfter
// delays have been recorded.
func recordSleeps(t *testing.T, rt *Runtime, stopAfter int) *[]time.Duration {
	t.Helper()
	recorded := make([]time.Duration, 0, stopAfter)
	rt.sleep = func(_ context.Context, _ <-chan struct{}, d time.Duration) bool {
		recorded = append(recorded, d)
		return len(recorded) < stopAfter
	}
	return &recorded
}

// failingBatchPoller returns the same fixed batch on every poll and counts the
// rounds, so a test can tell a throttled loop from a spinning one.
func failingBatchPoller(size int, polls *atomic.Int64) PollerFunc {
	return func(context.Context, int) ([]api.TaskEnvelope, error) {
		polls.Add(1)
		batch := make([]api.TaskEnvelope, 0, size)
		for i := range size {
			batch = append(batch, api.TaskEnvelope{
				ID: fmt.Sprintf("unleased-%d", i), RunID: "run", TaskID: "task",
			})
		}
		return batch, nil
	}
}

func TestRuntimeBacksOffFullyFailedBatches(t *testing.T) {
	tests := []struct {
		name        string
		concurrency int
		batchSize   int
	}{
		{name: "one envelope per batch", concurrency: 1, batchSize: 1},
		// The batch fills every worker slot, so the whole batch is dispatched
		// without the loop ever blocking on the semaphore.
		{name: "batch size equal to concurrency", concurrency: 4, batchSize: 4},
		{name: "batch larger than concurrency", concurrency: 2, batchSize: 6},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var polls atomic.Int64
			executor := ExecutorFunc(func(context.Context, ExecuteEnvelopeRequest) (ExecutionOutcome, error) {
				return ExecutionOutcome{State: ExecutionFailed}, errors.New("task execution unavailable")
			})
			opts := RuntimeOptions{
				Concurrency:          test.concurrency,
				BatchSize:            test.batchSize,
				PollInterval:         time.Minute,
				FailureBackoff:       10 * time.Millisecond,
				MaxFailureBackoff:    40 * time.Millisecond,
				ShutdownDrainTimeout: time.Second,
			}
			rt := NewRuntime(failingBatchPoller(test.batchSize, &polls), executor, opts)
			want := []time.Duration{
				10 * time.Millisecond,
				20 * time.Millisecond,
				40 * time.Millisecond,
				40 * time.Millisecond,
				40 * time.Millisecond,
			}
			recorded := recordSleeps(t, rt, len(want))

			if err := rt.Run(context.Background()); err != nil {
				t.Fatalf("Run() error = %v", err)
			}

			if !slices.Equal(*recorded, want) {
				t.Fatalf("recorded sleeps = %v, want %v", *recorded, want)
			}
			// One poll per backed-off round: every round of re-polling a
			// batch that keeps failing has to pay a backoff first.
			if polls.Load() != int64(len(want)) {
				t.Fatalf("polls = %d, want %d — one per backoff", polls.Load(), len(want))
			}
		})
	}
}

func TestRuntimeBacksOffFailedOutcomeWithoutError(t *testing.T) {
	var polls atomic.Int64
	var reported atomic.Int64
	rt := NewRuntime(
		failingBatchPoller(1, &polls),
		ExecutorFunc(func(context.Context, ExecuteEnvelopeRequest) (ExecutionOutcome, error) {
			return ExecutionOutcome{State: ExecutionFailed}, nil
		}),
		RuntimeOptions{
			Concurrency:       1,
			BatchSize:         1,
			FailureBackoff:    10 * time.Millisecond,
			MaxFailureBackoff: 10 * time.Millisecond,
			OnError: func(context.Context, api.TaskEnvelope, error) {
				reported.Add(1)
			},
		},
	)
	recorded := recordSleeps(t, rt, 1)

	if err := rt.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if want := []time.Duration{10 * time.Millisecond}; !slices.Equal(*recorded, want) {
		t.Fatalf("recorded sleeps = %v, want %v", *recorded, want)
	}
	if reported.Load() != 1 {
		t.Fatalf("OnError calls = %d, want 1", reported.Load())
	}
}

func TestRuntimeKeepsFullSpeedWhenBatchMakesProgress(t *testing.T) {
	var polls atomic.Int64
	poller := PollerFunc(func(context.Context, int) ([]api.TaskEnvelope, error) {
		if polls.Add(1) > 3 {
			return nil, nil
		}
		return []api.TaskEnvelope{
			{ID: "fails", RunID: "run", TaskID: "task-a"},
			{ID: "succeeds", RunID: "run", TaskID: "task-b"},
		}, nil
	})
	executor := ExecutorFunc(func(_ context.Context, req ExecuteEnvelopeRequest) (ExecutionOutcome, error) {
		if req.Envelope.ID == "fails" {
			return ExecutionOutcome{State: ExecutionFailed}, errors.New("task execution unavailable")
		}
		return ExecutionOutcome{State: ExecutionCompleted}, nil
	})
	pollInterval := time.Minute
	rt := NewRuntime(poller, executor, RuntimeOptions{
		Concurrency:          2,
		BatchSize:            2,
		PollInterval:         pollInterval,
		FailureBackoff:       10 * time.Millisecond,
		MaxFailureBackoff:    40 * time.Millisecond,
		ShutdownDrainTimeout: time.Second,
	})
	// The idle poll sleep doubles as the loop's stop signal: reaching it means
	// the batches with a failing envelope never paid a backoff.
	recorded := recordSleeps(t, rt, 1)

	if err := rt.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if want := []time.Duration{pollInterval}; !slices.Equal(*recorded, want) {
		t.Fatalf("recorded sleeps = %v, want only the idle poll interval %v", *recorded, want)
	}
	if failures := rt.failures.Load(); failures != 0 {
		t.Fatalf("consecutive failed batches = %d, want 0 while envelopes still execute", failures)
	}
}

func TestRuntimeResetsFailureBackoffAfterSuccess(t *testing.T) {
	var succeeded atomic.Bool
	poller := PollerFunc(func(context.Context, int) ([]api.TaskEnvelope, error) {
		if succeeded.Load() {
			return nil, nil
		}
		return []api.TaskEnvelope{{ID: "recovers", RunID: "run", TaskID: "task"}}, nil
	})
	var attempts atomic.Int64
	executor := ExecutorFunc(func(context.Context, ExecuteEnvelopeRequest) (ExecutionOutcome, error) {
		if attempts.Add(1) <= 2 {
			return ExecutionOutcome{State: ExecutionFailed}, errors.New("task execution unavailable")
		}
		succeeded.Store(true)
		return ExecutionOutcome{State: ExecutionCompleted}, nil
	})
	opts := RuntimeOptions{
		Concurrency:          1,
		PollInterval:         time.Minute,
		FailureBackoff:       10 * time.Millisecond,
		MaxFailureBackoff:    40 * time.Millisecond,
		ShutdownDrainTimeout: time.Second,
	}
	rt := NewRuntime(poller, executor, opts)
	// The idle poll sleep is the loop's own stop signal here: it can only be
	// reached once the executor stopped failing.
	recorded := make([]time.Duration, 0, 8)
	rt.sleep = func(_ context.Context, _ <-chan struct{}, d time.Duration) bool {
		recorded = append(recorded, d)
		return d != opts.PollInterval
	}

	if err := rt.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(recorded) == 0 || recorded[len(recorded)-1] != opts.PollInterval {
		t.Fatalf("recorded sleeps = %v, want the idle poll interval last", recorded)
	}
	for i, sleep := range recorded[:len(recorded)-1] {
		if sleep > opts.MaxFailureBackoff {
			t.Fatalf("sleep[%d] = %v, want a failure backoff", i, sleep)
		}
	}
	if failures := rt.failures.Load(); failures != 0 {
		t.Fatalf("consecutive failures = %d after a success, want 0", failures)
	}
}

func TestRuntimeFailureBackoffSchedule(t *testing.T) {
	rt := NewRuntime(nil, nil, RuntimeOptions{
		FailureBackoff:    10 * time.Millisecond,
		MaxFailureBackoff: 40 * time.Millisecond,
	})
	tests := []struct {
		name     string
		failures int64
		want     time.Duration
	}{
		{name: "no failures", failures: 0, want: 0},
		{name: "first failure", failures: 1, want: 10 * time.Millisecond},
		{name: "second failure doubles", failures: 2, want: 20 * time.Millisecond},
		{name: "third failure doubles", failures: 3, want: 40 * time.Millisecond},
		{name: "further failures stay capped", failures: 9, want: 40 * time.Millisecond},
		{name: "overflow stays capped", failures: 1 << 40, want: 40 * time.Millisecond},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := rt.failureBackoff(test.failures); got != test.want {
				t.Fatalf("failureBackoff(%d) = %v, want %v", test.failures, got, test.want)
			}
		})
	}
}
