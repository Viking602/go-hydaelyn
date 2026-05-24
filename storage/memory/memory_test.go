package memory_test

import (
	"context"
	"testing"
	"time"

	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/storage/memory"
)

// Compile-time interface satisfaction checks.
var (
	_ api.StoreProvider        = (*memory.Provider)(nil)
	_ api.BlackboardSubscriber = (*memory.Provider)(nil)
	_ api.CapabilityReporter   = (*memory.Provider)(nil)
	_ api.ProviderCloser       = (*memory.Provider)(nil)
)

func TestProvider_BeginCommit(t *testing.T) {
	p := memory.NewProvider()
	ctx := context.Background()

	uow, err := p.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := uow.Runs().SaveRun(ctx, api.Run{ID: "run-1", Status: api.RunStatusCreated, CreatedAt: time.Now()}); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	if err := uow.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	uow2, err := p.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin 2: %v", err)
	}
	defer func() { _ = uow2.Rollback(ctx) }()
	run, err := uow2.Runs().LoadRun(ctx, "run-1")
	if err != nil {
		t.Fatalf("LoadRun: %v", err)
	}
	if run.ID != "run-1" {
		t.Fatalf("unexpected run id %q", run.ID)
	}
}

func TestProvider_Rollback(t *testing.T) {
	p := memory.NewProvider()
	ctx := context.Background()

	uow, err := p.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := uow.Runs().SaveRun(ctx, api.Run{ID: "run-rollback", Status: api.RunStatusCreated}); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	if err := uow.Rollback(ctx); err != nil {
		t.Fatalf("Rollback: %v", err)
	}

	uow2, err := p.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin 2: %v", err)
	}
	defer func() { _ = uow2.Rollback(ctx) }()
	if _, err := uow2.Runs().LoadRun(ctx, "run-rollback"); err == nil {
		t.Fatal("expected NotFound after rollback")
	}
}

func TestProvider_Capabilities(t *testing.T) {
	p := memory.NewProvider()
	caps, err := p.Capabilities(context.Background())
	if err != nil {
		t.Fatalf("Capabilities: %v", err)
	}
	if !caps.SupportsTransactions {
		t.Error("memory provider should support transactions")
	}
	if !caps.SupportsBlackboardSubscribe {
		t.Error("memory provider should support blackboard subscribe")
	}
	if !caps.SupportsListPending {
		t.Error("memory provider should support list-pending")
	}
	if caps.SupportsConcurrentWriters {
		t.Error("memory provider should NOT support concurrent writers")
	}
	if caps.SupportsDeadLetterRequeue {
		t.Error("memory provider should NOT support dead-letter requeue")
	}
}

func TestProvider_Close(t *testing.T) {
	p := memory.NewProvider()
	if err := p.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
