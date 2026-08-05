package run_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Viking602/venat/internal/core/model"
	"github.com/Viking602/venat/internal/memory"
	runsvc "github.com/Viking602/venat/internal/run"
)

func testIDGenerator() runsvc.IDGenerator {
	next := 0
	return func(prefix string) string {
		next++
		return fmt.Sprintf("%s-%d", prefix, next)
	}
}

func TestStartCreatesRunRootTaskAndEvents(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()

	metadata := map[string]string{"owner": "runtime"}
	run, root, err := runsvc.Start(ctx, uow, testIDGenerator(), runsvc.StartInput{
		Request:  "ship agent runtime",
		Metadata: metadata,
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	metadata["owner"] = "mutated"

	if run.ID == "" || root.ID == "" || run.RootTaskID != root.ID {
		t.Fatalf("Start() returned inconsistent IDs: run=%#v root=%#v", run, root)
	}
	if run.Metadata["owner"] != "runtime" {
		t.Fatalf("Start() did not clone metadata: %#v", run.Metadata)
	}
	if root.Type != model.TaskTypeWorker || root.Status != model.TaskStatusCreated || root.Version != 1 {
		t.Fatalf("unexpected root task contract: %#v", root)
	}

	events, err := uow.Events().ListEvents(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(events) != 2 || events[0].Type != model.EventRunStarted || events[1].Type != model.EventTaskCreated {
		t.Fatalf("Start() events = %#v", events)
	}
}

func TestStartIsIdempotentForExplicitRunIdentity(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()
	input := runsvc.StartInput{
		RunID: "run-idempotent", RootTaskID: "root-idempotent",
		Request: "ship once", Metadata: map[string]string{"owner": "runtime"},
	}
	firstRun, firstRoot, err := runsvc.Start(ctx, uow, testIDGenerator(), input)
	if err != nil {
		t.Fatalf("Start(first) error = %v", err)
	}
	secondRun, secondRoot, err := runsvc.Start(ctx, uow, testIDGenerator(), input)
	if err != nil {
		t.Fatalf("Start(retry) error = %v", err)
	}
	if !secondRun.CreatedAt.Equal(firstRun.CreatedAt) || !secondRoot.CreatedAt.Equal(firstRoot.CreatedAt) {
		t.Fatalf("retry replaced durable records: first=%#v/%#v second=%#v/%#v", firstRun, firstRoot, secondRun, secondRoot)
	}
	events, err := uow.Events().ListEvents(ctx, input.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("retry events = %d, want original two", len(events))
	}
	conflict := input
	conflict.Request = "replace existing run"
	if _, _, err := runsvc.Start(ctx, uow, testIDGenerator(), conflict); !errors.Is(err, model.ErrIdempotencyConflict) {
		t.Fatalf("Start(conflict) error = %v, want ErrIdempotencyConflict", err)
	}
}

func TestCreateTaskDefaultsAndCopiesMutableInput(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()

	run, _, err := runsvc.Start(ctx, uow, testIDGenerator(), runsvc.StartInput{Request: "root"})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	tags := []string{"research"}
	deps := []string{"dep-1"}
	task, err := runsvc.CreateTask(ctx, uow, testIDGenerator(), runsvc.CreateTaskInput{
		RunID:           run.ID,
		TaskID:          "task-explicit",
		Goal:            "collect evidence",
		OwnerAgentID:    "agent-1",
		Tags:            tags,
		DependsOn:       deps,
		WriteTargets:    []string{"findings"},
		ReadSelectors:   []model.BlackboardSelector{{Keys: []string{"context"}}},
		RetryPolicy:     model.RetryPolicy{MaxAttempts: 3},
		PolicyDecisions: []model.PolicyDecision{{Effect: model.PolicyEffectAllow}},
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	tags[0] = "mutated"
	deps[0] = "mutated"

	if task.Type != model.TaskTypeWorker {
		t.Fatalf("CreateTask() default type = %q", task.Type)
	}
	if task.Status != model.TaskStatusWaitingDependency {
		t.Fatalf("CreateTask() status with dependencies = %q", task.Status)
	}
	if task.AssignedAgentID != "agent-1" || len(task.OwnerHistory) != 1 || task.OwnerHistory[0] != "agent-1" {
		t.Fatalf("CreateTask() owner contract = %#v", task)
	}
	if task.Tags[0] != "research" || task.DependsOn[0] != "dep-1" {
		t.Fatalf("CreateTask() did not copy mutable inputs: %#v", task)
	}
}

func TestCreateTaskRejectsInvalidJSON(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()
	run, _, err := runsvc.Start(ctx, uow, testIDGenerator(), runsvc.StartInput{Request: "root"})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if _, err := runsvc.CreateTask(ctx, uow, testIDGenerator(), runsvc.CreateTaskInput{
		RunID: run.ID,
		Input: json.RawMessage(`{`),
	}); err == nil {
		t.Fatal("CreateTask() accepted malformed input JSON")
	}
}

func TestCreateTaskRejectsUnsafeRetryPolicy(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = uow.Rollback(ctx) }()
	run, _, err := runsvc.Start(ctx, uow, testIDGenerator(), runsvc.StartInput{Request: "root"})
	if err != nil {
		t.Fatal(err)
	}
	for _, policy := range []model.RetryPolicy{
		{MaxAttempts: model.MaxRetryAttempts + 1},
		{MaxAttempts: -1},
		{MaxAttempts: 2, Backoff: -time.Second},
		{MaxAttempts: 2, MaxBackoff: -time.Second},
	} {
		if _, err := runsvc.CreateTask(ctx, uow, testIDGenerator(), runsvc.CreateTaskInput{
			RunID: run.ID, RetryPolicy: policy,
		}); !errors.Is(err, model.ErrInvalidCommand) {
			t.Fatalf("CreateTask(%+v) error = %v, want ErrInvalidCommand", policy, err)
		}
	}
}
