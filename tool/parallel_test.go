package tool

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Viking602/go-hydaelyn/message"
)

func TestMaxConcurrencyParallelToolsReduceLatency(t *testing.T) {
	driver := &latencyDriver{name: "slow", latency: 20 * time.Millisecond}
	bus := NewBus(driver)
	calls := []Call{
		{Name: "slow", Arguments: message.ToolCall{}.Arguments},
		{Name: "slow", Arguments: message.ToolCall{}.Arguments},
		{Name: "slow", Arguments: message.ToolCall{}.Arguments},
		{Name: "slow", Arguments: message.ToolCall{}.Arguments},
	}

	startedAt := time.Now()
	sequential, err := bus.ExecuteBatch(context.Background(), calls, ModeSequential, nil)
	if err != nil {
		t.Fatalf("ExecuteBatch(sequential) error = %v", err)
	}
	sequentialDuration := time.Since(startedAt)

	driver.Reset()
	startedAt = time.Now()
	parallel, err := bus.ExecuteBatch(context.Background(), calls, ModeParallel, nil)
	if err != nil {
		t.Fatalf("ExecuteBatch(parallel) error = %v", err)
	}
	parallelDuration := time.Since(startedAt)

	if len(sequential) != len(parallel) {
		t.Fatalf("result length mismatch: sequential=%d parallel=%d", len(sequential), len(parallel))
	}
	if parallelDuration >= sequentialDuration {
		t.Fatalf("expected parallel execution to be faster: sequential=%s parallel=%s", sequentialDuration, parallelDuration)
	}
	if driver.Max() < 2 {
		t.Fatalf("expected overlapping execution, max concurrency = %d", driver.Max())
	}
	t.Logf("sequential=%s parallel=%s", sequentialDuration, parallelDuration)
}

type latencyDriver struct {
	name    string
	latency time.Duration
	active  int64
	max     int64
}

func (d *latencyDriver) Definition() Definition {
	return Definition{Name: d.name, InputSchema: Schema{Type: "object"}}
}

func (d *latencyDriver) Execute(ctx context.Context, call Call, sink UpdateSink) (Result, error) {
	current := atomic.AddInt64(&d.active, 1)
	for {
		maxCurrent := atomic.LoadInt64(&d.max)
		if current <= maxCurrent || atomic.CompareAndSwapInt64(&d.max, maxCurrent, current) {
			break
		}
	}
	defer atomic.AddInt64(&d.active, -1)
	select {
	case <-ctx.Done():
		return Result{}, ctx.Err()
	case <-time.After(d.latency):
	}
	return Result{Name: call.Name, Content: "ok"}, nil
}

func (d *latencyDriver) Max() int {
	return int(atomic.LoadInt64(&d.max))
}

func (d *latencyDriver) Reset() {
	atomic.StoreInt64(&d.active, 0)
	atomic.StoreInt64(&d.max, 0)
}
