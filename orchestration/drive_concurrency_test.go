package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Viking602/venat/agent"
)

func TestDriveBoundsConcurrency(t *testing.T) {
	entered := make(chan string, 4)
	release := make(chan struct{})
	var active atomic.Int32
	var maximum atomic.Int32
	executor := ExecutorFunc(func(_ context.Context, dispatch Dispatch, _ agent.Sink) (agent.Result, error) {
		current := active.Add(1)
		for {
			prior := maximum.Load()
			if current <= prior || maximum.CompareAndSwap(prior, current) {
				break
			}
		}
		entered <- dispatch.ID
		<-release
		active.Add(-1)
		return agent.Result{Text: dispatch.ID}, nil
	})
	done := make(chan error, 1)
	go func() {
		_, err := Drive(context.Background(), oneTickScheduler("a", "b", "c", "d"), executor, DriveOptions{MaxConcurrency: 2})
		done <- err
	}()
	<-entered
	<-entered
	select {
	case third := <-entered:
		t.Fatalf("dispatch %q started above MaxConcurrency", third)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("Drive() error = %v", err)
	}
	if maximum.Load() != 2 {
		t.Fatalf("maximum concurrency = %d, want 2", maximum.Load())
	}
}

func TestDriveStateIsByteEquivalentAcrossCompletionOrders(t *testing.T) {
	run := func(delays map[string]time.Duration) State {
		t.Helper()
		state, err := Drive(context.Background(), oneTickScheduler("c", "a", "b"), ExecutorFunc(func(_ context.Context, dispatch Dispatch, _ agent.Sink) (agent.Result, error) {
			time.Sleep(delays[dispatch.ID])
			return agent.Result{Text: dispatch.ID, Valid: true}, nil
		}), DriveOptions{UnlimitedConcurrency: true})
		if err != nil {
			t.Fatalf("Drive() error = %v", err)
		}
		return state
	}
	first := run(map[string]time.Duration{"a": 3 * time.Millisecond, "b": 2 * time.Millisecond, "c": time.Millisecond})
	second := run(map[string]time.Duration{"a": time.Millisecond, "b": 2 * time.Millisecond, "c": 3 * time.Millisecond})
	firstJSON, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("json.Marshal(first) error = %v", err)
	}
	secondJSON, err := json.Marshal(second)
	if err != nil {
		t.Fatalf("json.Marshal(second) error = %v", err)
	}
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("states differ by completion order\nfirst:  %s\nsecond: %s", firstJSON, secondJSON)
	}
	if got := []string{first.Outcomes[0].Dispatch.ID, first.Outcomes[1].Dispatch.ID, first.Outcomes[2].Dispatch.ID}; !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatalf("outcome order = %v, want [a b c]", got)
	}
}

func TestDriveSerializesSharedSinkAndSetsDispatchSource(t *testing.T) {
	var active atomic.Int32
	var maximum atomic.Int32
	var mu sync.Mutex
	var sources []string
	sink := agent.SinkFunc(func(_ context.Context, frame agent.Frame) error {
		current := active.Add(1)
		for {
			prior := maximum.Load()
			if current <= prior || maximum.CompareAndSwap(prior, current) {
				break
			}
		}
		time.Sleep(time.Millisecond)
		mu.Lock()
		sources = append(sources, frame.Source)
		mu.Unlock()
		active.Add(-1)
		return nil
	})
	executor := ExecutorFunc(func(ctx context.Context, dispatch Dispatch, sink agent.Sink) (agent.Result, error) {
		if err := sink.Emit(ctx, agent.Frame{Source: "wrong", Kind: agent.FrameText, Text: dispatch.ID}); err != nil {
			return agent.Result{}, err
		}
		return agent.Result{Text: dispatch.ID}, nil
	})
	_, err := Drive(context.Background(), oneTickScheduler("c", "a", "b"), executor, DriveOptions{UnlimitedConcurrency: true, Sink: sink})
	if err != nil {
		t.Fatalf("Drive() error = %v", err)
	}
	if maximum.Load() != 1 {
		t.Fatalf("maximum concurrent sink calls = %d, want 1", maximum.Load())
	}
	mu.Lock()
	defer mu.Unlock()
	seen := make(map[string]bool, len(sources))
	for _, source := range sources {
		seen[source] = true
	}
	if len(sources) != 3 || !seen["a"] || !seen["b"] || !seen["c"] || seen["wrong"] {
		t.Fatalf("frame sources = %v, want dispatch IDs", sources)
	}
}

func TestDriveCancellationReturnsDispatchPartialResult(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	executor := ExecutorFunc(func(ctx context.Context, _ Dispatch, _ agent.Sink) (agent.Result, error) {
		close(started)
		<-ctx.Done()
		return agent.Result{Text: "partial"}, ctx.Err()
	})
	done := make(chan struct{})
	var state State
	var driveErr error
	go func() {
		state, driveErr = Drive(ctx, oneTickScheduler("cancel"), executor, DriveOptions{})
		close(done)
	}()
	<-started
	cancel()
	<-done
	if !errors.Is(driveErr, context.Canceled) {
		t.Fatalf("Drive() error = %v, want context.Canceled", driveErr)
	}
	var dispatchErr *DispatchError
	if !errors.As(driveErr, &dispatchErr) || dispatchErr.Result.Text != "partial" {
		t.Fatalf("DispatchError = %#v, want partial result", dispatchErr)
	}
	if state.Tick != 0 || len(state.Outcomes) != 0 {
		t.Fatalf("state = %#v, want no successful outcomes", state)
	}
}

func oneTickScheduler(ids ...string) Scheduler {
	return SchedulerFunc(func(_ context.Context, state State) ([]Dispatch, error) {
		if state.Tick > 0 {
			return nil, nil
		}
		dispatches := make([]Dispatch, len(ids))
		for index, id := range ids {
			dispatches[index] = validDispatch(id)
		}
		return dispatches, nil
	})
}
