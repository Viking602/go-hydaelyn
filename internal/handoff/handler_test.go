package handoff_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/Viking602/go-hydaelyn/internal/core/model"
	"github.com/Viking602/go-hydaelyn/internal/handoff"
	"github.com/Viking602/go-hydaelyn/internal/memory"
)

func handoffIDGenerator() handoff.IDGenerator {
	next := 0
	return func(prefix string) string {
		next++
		return fmt.Sprintf("%s-%d", prefix, next)
	}
}

func TestApplierTransfersOwnerWritesContextAndQueuesEnvelope(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()

	task := model.Task{ID: "task-1", RunID: "run-1", Status: model.TaskStatusRunning, Version: 2, OwnerAgentID: "agent-a", OwnerHistory: []string{"agent-a"}}
	result, err := handoff.NewApplier(handoff.HandlerOptions{NewID: handoffIDGenerator()}).Apply(ctx, uow, task, &model.HandoffRequest{
		RunID:          task.RunID,
		TaskID:         task.ID,
		FromAgentID:    "agent-a",
		ToAgentID:      "agent-b",
		TaskVersion:    task.Version,
		ContextSummary: "handoff context",
		Reason:         "specialist needed",
	}, "")
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if result.Task.OwnerAgentID != "agent-b" || result.Task.Version != 3 || result.Task.HandoffCount != 1 {
		t.Fatalf("handoff task = %#v", result.Task)
	}
	if !result.HasContext || result.BlackboardItem.Key != "handoff_context" {
		t.Fatalf("handoff context = %#v", result)
	}
	if result.Envelope.TargetAgentID != "agent-b" || result.Envelope.Type != "HandoffEnvelope" {
		t.Fatalf("handoff envelope = %#v", result.Envelope)
	}
}

func TestApplierRejectsOwnerCycle(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()

	task := model.Task{ID: "task-1", RunID: "run-1", Status: model.TaskStatusRunning, Version: 1, OwnerAgentID: "agent-a", OwnerHistory: []string{"agent-a", "agent-b"}}
	_, err = handoff.NewApplier(handoff.HandlerOptions{NewID: handoffIDGenerator()}).Apply(ctx, uow, task, &model.HandoffRequest{
		RunID:       task.RunID,
		TaskID:      task.ID,
		FromAgentID: "agent-a",
		ToAgentID:   "agent-b",
		TaskVersion: task.Version,
	}, "")
	if !errors.Is(err, model.ErrHandoffCycle) {
		t.Fatalf("Apply() error = %v, want ErrHandoffCycle", err)
	}
}
