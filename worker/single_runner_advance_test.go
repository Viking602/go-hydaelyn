package worker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Viking602/venat"
	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/internal/memory"
)

var errCompetingRunWriter = errors.New("test: run was written by another holder")

// competingWriterProvider simulates another process that keeps moving a run's
// status: once armed, every write to that run is rejected and the next read
// reports a different status, which is exactly the shape that used to keep
// advanceSingleRunToRunning retrying forever.
type competingWriterProvider struct {
	inner    *memory.Provider
	statuses []api.RunStatus

	mu     sync.Mutex
	runID  string
	index  int
	writes int
}

func (p *competingWriterProvider) arm(runID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.runID = runID
}

func (p *competingWriterProvider) writeCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.writes
}

// observed is the status a competing writer left behind for runID, if the
// provider is armed for it.
func (p *competingWriterProvider) observed(runID string) (api.RunStatus, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.runID == "" || p.runID != runID {
		return "", false
	}
	return p.statuses[p.index%len(p.statuses)], true
}

// reject records an intercepted write and moves the run on, so the reload that
// follows a failed transition sees a status that changed underneath us.
func (p *competingWriterProvider) reject(runID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.runID == "" || p.runID != runID {
		return false
	}
	p.writes++
	p.index++
	return true
}

func (p *competingWriterProvider) Begin(ctx context.Context) (api.UnitOfWork, error) {
	uow, err := p.inner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return competingWriterUnitOfWork{UnitOfWork: uow, provider: p}, nil
}

func (p *competingWriterProvider) Capabilities(ctx context.Context) (api.StoreCapabilities, error) {
	return p.inner.Capabilities(ctx)
}

type competingWriterUnitOfWork struct {
	api.UnitOfWork
	provider *competingWriterProvider
}

func (u competingWriterUnitOfWork) Runs() api.RunStore {
	return competingWriterRuns{RunStore: u.UnitOfWork.Runs(), provider: u.provider}
}

type competingWriterRuns struct {
	api.RunStore
	provider *competingWriterProvider
}

func (r competingWriterRuns) LoadRun(ctx context.Context, runID string) (api.Run, error) {
	run, err := r.RunStore.LoadRun(ctx, runID)
	if err != nil {
		return run, err
	}
	if status, ok := r.provider.observed(runID); ok {
		run.Status = status
	}
	return run, nil
}

func (r competingWriterRuns) SaveRun(ctx context.Context, run api.Run) error {
	if r.provider.reject(run.ID) {
		return errCompetingRunWriter
	}
	return r.RunStore.SaveRun(ctx, run)
}

func TestAdvanceSingleRunToRunningBoundsStatusConflicts(t *testing.T) {
	ctx := context.Background()
	const runID = "single-advance-conflict"
	provider := &competingWriterProvider{
		inner: memory.NewProvider(),
		// Both are legal starting points for the advance path, so every
		// iteration picks a valid next status and only ever loses the race.
		statuses: []api.RunStatus{api.RunStatusCreated, api.RunStatusPlanning},
	}
	runner := venat.NewDevelopment(api.Config{StoreProvider: provider})
	if _, err := runner.StartRunWithResult(ctx, api.StartRunCommand{
		RunID: runID, RootTaskID: runID + "-root", Request: "advance me",
	}); err != nil {
		t.Fatalf("StartRunWithResult() error = %v", err)
	}
	provider.arm(runID)

	coordinator := &SingleRunner{
		Runner: runner,
		Worker: AgentWorker{Runner: runner, AgentID: "single-agent"},
	}
	advanced := make(chan error, 1)
	go func() {
		advanced <- coordinator.advanceSingleRunToRunning(ctx, runID)
	}()

	select {
	case err := <-advanced:
		if !errors.Is(err, ErrSingleRunAlreadyExecuting) {
			t.Fatalf("advanceSingleRunToRunning() error = %v, want %v", err, ErrSingleRunAlreadyExecuting)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("advanceSingleRunToRunning() livelocked against a competing writer")
	}
	if writes := provider.writeCount(); writes == 0 || writes > 12 {
		t.Fatalf("intercepted transitions = %d, want a bounded number of retries", writes)
	}
}
