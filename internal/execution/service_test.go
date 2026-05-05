package execution_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Viking602/go-hydaelyn/internal/core/model"
	"github.com/Viking602/go-hydaelyn/internal/execution"
	"github.com/Viking602/go-hydaelyn/internal/memory"
	runsvc "github.com/Viking602/go-hydaelyn/internal/run"
)

func executionIDGenerator() execution.IDGenerator {
	next := 0
	return func(prefix string) string {
		next++
		return fmt.Sprintf("%s-%d", prefix, next)
	}
}

func TestAcquireReturnsExistingActiveLease(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()

	run, root, err := runsvc.Start(ctx, uow, func(prefix string) string { return prefix + "-seed" }, runsvc.StartInput{
		RunID:      "run-1",
		RootTaskID: "root",
		Request:    "execute",
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	root.Status = model.TaskStatusDispatched
	if err := uow.Tasks().SaveTask(ctx, root); err != nil {
		t.Fatalf("SaveTask(root) error = %v", err)
	}
	first, err := execution.Acquire(ctx, uow, executionIDGenerator(), execution.AcquireInput{
		RunID:      run.ID,
		TaskID:     root.ID,
		HolderType: model.HolderComponent,
		HolderID:   "orchestrator",
		TTL:        time.Hour,
	})
	if err != nil {
		t.Fatalf("Acquire(first) error = %v", err)
	}
	second, err := execution.Acquire(ctx, uow, executionIDGenerator(), execution.AcquireInput{
		RunID:      run.ID,
		TaskID:     root.ID,
		HolderType: model.HolderComponent,
		HolderID:   "orchestrator",
		TTL:        time.Hour,
	})
	if err != nil {
		t.Fatalf("Acquire(second) error = %v", err)
	}
	if !first.Acquired || second.Acquired || second.Lease.ID != first.Lease.ID {
		t.Fatalf("Acquire() active lease contract: first=%#v second=%#v", first, second)
	}
}

func TestAcquireRejectsStaleEnvelopeVersion(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()

	run, root, err := runsvc.Start(ctx, uow, func(prefix string) string { return prefix + "-seed" }, runsvc.StartInput{
		RunID:      "run-1",
		RootTaskID: "root",
		Request:    "execute",
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := uow.MailboxOutbox().QueueEnvelope(ctx, model.TaskEnvelope{
		ID:            "env-1",
		RunID:         run.ID,
		TaskID:        root.ID,
		TargetAgentID: "agent-1",
		TaskVersion:   root.Version + 1,
		Status:        "pending",
	}); err != nil {
		t.Fatalf("QueueEnvelope() error = %v", err)
	}

	_, err = execution.Acquire(ctx, uow, executionIDGenerator(), execution.AcquireInput{
		RunID:      run.ID,
		TaskID:     root.ID,
		EnvelopeID: "env-1",
		HolderType: model.HolderAgent,
		HolderID:   "agent-1",
	})
	if !errors.Is(err, model.ErrStaleTaskVersion) {
		t.Fatalf("Acquire() error = %v, want ErrStaleTaskVersion", err)
	}
}
