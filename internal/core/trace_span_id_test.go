package core

import (
	"context"
	"fmt"
	"testing"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/internal/core/ports"
	"github.com/Viking602/venat/internal/memory"
)

// A third-party TraceStore is entitled to reject a span with no ID rather
// than mint one, so every runtime call site must generate the ID itself.
// strictSpanIDStoreProvider stands in for such a store: the in-memory
// provider's own fallback would otherwise hide a missing ID.
func TestRuntimeGeneratesTraceSpanIDsAtEveryCallSite(t *testing.T) {
	ctx := context.Background()

	t.Run("blackboard write", func(t *testing.T) {
		rt := newStrictSpanIDRuntime()
		run := mustStartRun(ctx, t, rt, "run-span-blackboard")
		if err := rt.WriteItem(ctx, api.BlackboardItem{
			RunID:      run.ID,
			TaskID:     run.RootTaskID,
			Source:     api.SourceIdentity{Type: api.SourceAgent, ID: "agent-a"},
			Visibility: api.BlackboardVisibilityAgentVisible,
			Key:        "note",
			Payload:    "hello",
		}); err != nil {
			t.Fatalf("WriteItem() error = %v", err)
		}
		assertTraceSpanIDs(ctx, t, rt, run.ID)
	})

	t.Run("advance run", func(t *testing.T) {
		rt := newStrictSpanIDRuntime()
		run := mustStartRun(ctx, t, rt, "run-span-advance")
		mustAdvanceRun(ctx, t, rt, run.ID)
		assertTraceSpanIDs(ctx, t, rt, run.ID)
	})

	t.Run("typed report", func(t *testing.T) {
		rt := newStrictSpanIDRuntime()
		run, task := mustStartWorker(ctx, t, rt, "run-span-report", "worker")
		lease := leaseTask(ctx, t, rt, run.ID, task.ID, api.HolderAgent, "agent-a")
		if err := rt.SubmitTypedReport(ctx, SubmitTypedReportCommand{
			RunID: run.ID, TaskID: task.ID, LeaseID: lease.ID,
			HolderType: api.HolderAgent, HolderID: "agent-a",
			TaskVersion: task.Version,
			Report: api.TypedReport{
				Status:        api.ReportStatusSuccess,
				Summary:       "done",
				ActionOutcome: &api.ActionOutcome{AttemptID: "attempt-1", Status: api.ActionAttemptSucceeded, Output: "ok"},
			},
		}); err != nil {
			t.Fatalf("SubmitTypedReport() error = %v", err)
		}
		assertTraceSpanIDs(ctx, t, rt, run.ID)
	})
}

func newStrictSpanIDRuntime() *Runtime {
	return NewRuntime(Config{StoreProvider: strictSpanIDStoreProvider{inner: memory.NewProvider()}})
}

func assertTraceSpanIDs(ctx context.Context, t *testing.T, rt *Runtime, runID string) {
	t.Helper()
	spans, err := rt.ListTraceSpans(ctx, runID)
	if err != nil {
		t.Fatalf("ListTraceSpans() error = %v", err)
	}
	if len(spans) == 0 {
		t.Fatal("ListTraceSpans() returned no spans; the flow recorded none")
	}
	for _, span := range spans {
		if span.ID == "" {
			t.Fatalf("trace span %q has an empty ID: %#v", span.Name, span)
		}
	}
}

var errEmptyTraceSpanID = fmt.Errorf("trace span requires a caller-supplied ID: %w", api.ErrInvalidCommand)

type strictSpanIDStoreProvider struct{ inner *memory.Provider }

func (p strictSpanIDStoreProvider) Begin(ctx context.Context) (ports.UnitOfWork, error) {
	uow, err := p.inner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return strictSpanIDUnitOfWork{UnitOfWork: uow}, nil
}

func (p strictSpanIDStoreProvider) BeginRead(ctx context.Context) (ports.UnitOfWork, error) {
	uow, err := p.inner.BeginRead(ctx)
	if err != nil {
		return nil, err
	}
	return strictSpanIDUnitOfWork{UnitOfWork: uow}, nil
}

func (p strictSpanIDStoreProvider) Capabilities(ctx context.Context) (ports.StoreCapabilities, error) {
	return p.inner.Capabilities(ctx)
}

type strictSpanIDUnitOfWork struct{ ports.UnitOfWork }

func (u strictSpanIDUnitOfWork) Trace() ports.TraceStore {
	return strictSpanIDTraceStore{TraceStore: u.UnitOfWork.Trace()}
}

type strictSpanIDTraceStore struct{ ports.TraceStore }

func (s strictSpanIDTraceStore) SaveTraceSpan(ctx context.Context, span api.TraceSpan) error {
	if span.ID == "" {
		return errEmptyTraceSpanID
	}
	return s.TraceStore.SaveTraceSpan(ctx, span)
}
