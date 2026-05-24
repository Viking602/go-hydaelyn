package worker_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/worker"
)

func TestRuntime_RequiresPollerAndExecutor(t *testing.T) {
	rt := worker.NewRuntime(nil, nil, worker.RuntimeOptions{})
	if err := rt.Run(context.Background()); !errors.Is(err, worker.ErrRuntimeMisconfigured) {
		t.Fatalf("expected ErrRuntimeMisconfigured, got %v", err)
	}
}

func TestRuntime_ExecutesEnvelopesFromChannel(t *testing.T) {
	p := worker.NewChannelPoller(4)
	var seen []string
	var mu sync.Mutex
	exec := worker.ExecutorFunc(func(ctx context.Context, req worker.ExecuteEnvelopeRequest) error {
		mu.Lock()
		seen = append(seen, req.Envelope.ID)
		mu.Unlock()
		return nil
	})
	rt := worker.NewRuntime(p, exec, worker.RuntimeOptions{
		Concurrency:          2,
		PollInterval:         10 * time.Millisecond,
		ShutdownDrainTimeout: 2 * time.Second,
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- rt.Run(ctx) }()

	for _, id := range []string{"e1", "e2", "e3"} {
		if err := p.Submit(ctx, api.TaskEnvelope{ID: id, RunID: "r", TaskID: "t"}); err != nil {
			t.Fatalf("submit: %v", err)
		}
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(seen)
		mu.Unlock()
		if n == 3 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run returned: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 3 {
		t.Fatalf("expected 3 envelopes processed, got %d: %v", len(seen), seen)
	}
}

func TestRuntime_OnErrorReceivesExecutorErrors(t *testing.T) {
	p := worker.NewChannelPoller(2)
	var errCount int32
	rt := worker.NewRuntime(p,
		worker.ExecutorFunc(func(ctx context.Context, req worker.ExecuteEnvelopeRequest) error {
			return errors.New("boom")
		}),
		worker.RuntimeOptions{
			Concurrency:          1,
			PollInterval:         10 * time.Millisecond,
			ShutdownDrainTimeout: time.Second,
			OnError: func(ctx context.Context, env api.TaskEnvelope, err error) {
				atomic.AddInt32(&errCount, 1)
			},
		})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- rt.Run(ctx) }()

	if err := p.Submit(ctx, api.TaskEnvelope{ID: "fails"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && atomic.LoadInt32(&errCount) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	if atomic.LoadInt32(&errCount) != 1 {
		t.Fatalf("expected 1 error reported, got %d", atomic.LoadInt32(&errCount))
	}
}

func TestRuntime_StopDrainsInFlight(t *testing.T) {
	p := worker.NewChannelPoller(2)
	release := make(chan struct{})
	var finished int32
	rt := worker.NewRuntime(p,
		worker.ExecutorFunc(func(ctx context.Context, req worker.ExecuteEnvelopeRequest) error {
			<-release
			atomic.AddInt32(&finished, 1)
			return nil
		}),
		worker.RuntimeOptions{
			Concurrency:          1,
			PollInterval:         10 * time.Millisecond,
			ShutdownDrainTimeout: 2 * time.Second,
		})

	ctx := context.Background()
	go rt.Run(ctx)
	_ = p.Submit(ctx, api.TaskEnvelope{ID: "slow"})

	// Let the worker start.
	time.Sleep(100 * time.Millisecond)

	// Concurrently call Stop and release the executor; Stop must wait for drain.
	go func() {
		time.Sleep(50 * time.Millisecond)
		close(release)
	}()
	if err := rt.Stop(); err != nil {
		t.Fatalf("Stop returned: %v", err)
	}
	if atomic.LoadInt32(&finished) != 1 {
		t.Fatalf("expected drained executor to finish, got %d", atomic.LoadInt32(&finished))
	}
}
