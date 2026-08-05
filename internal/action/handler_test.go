package action_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Viking602/venat/internal/action"
	commandbus "github.com/Viking602/venat/internal/command"
	"github.com/Viking602/venat/internal/core/model"
	"github.com/Viking602/venat/internal/core/ports"
	"github.com/Viking602/venat/internal/memory"
)

func TestStartActionAttemptRequiresActionCapableTask(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()
	saveActionFixture(ctx, t, uow, false)

	bus := commandbus.NewBus()
	action.RegisterHandlers(bus, action.HandlerOptions{NewID: func(string) string { return "attempt-generated" }})

	_, err = bus.Execute(ctx, uow, action.StartActionAttemptCommand{
		RunID:       "run-1",
		TaskID:      "task-1",
		LeaseID:     "lease-1",
		HolderType:  model.HolderAgent,
		HolderID:    "agent-1",
		TaskVersion: 1,
		ToolName:    "deploy",
	})
	if !errors.Is(err, model.ErrActionTaskRequired) {
		t.Fatalf("StartActionAttempt error = %v, want ErrActionTaskRequired", err)
	}
}

func TestStartActionAttemptPersistsAttemptAndEvent(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()
	saveActionFixture(ctx, t, uow, true)

	bus := commandbus.NewBus()
	action.RegisterHandlers(bus, action.HandlerOptions{NewID: func(string) string { return "attempt-generated" }})

	result, err := bus.Execute(ctx, uow, action.StartActionAttemptCommand{
		ActionID:       "action-1",
		RunID:          "run-1",
		TaskID:         "task-1",
		LeaseID:        "lease-1",
		HolderType:     model.HolderAgent,
		HolderID:       "agent-1",
		TaskVersion:    1,
		ToolName:       "deploy",
		IdempotencyKey: "idem-1",
		InputHash:      "hash-1",
	})
	if err != nil {
		t.Fatalf("StartActionAttempt error = %v", err)
	}
	attempt := result.(model.ActionAttempt)
	if attempt.AttemptID != "attempt-generated" || attempt.Status != model.ActionAttemptRunning || attempt.ToolName != "deploy" {
		t.Fatalf("attempt = %#v", attempt)
	}
	events, err := uow.Events().ListEvents(ctx, "run-1")
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(events) != 1 || events[0].Type != model.EventActionAttemptStarted {
		t.Fatalf("events = %#v", events)
	}
}

func TestStartActionAttemptReturnsExistingForSameIdempotencyKey(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()
	saveActionFixture(ctx, t, uow, true)

	nextID := 0
	bus := commandbus.NewBus()
	action.RegisterHandlers(bus, action.HandlerOptions{NewID: func(string) string {
		nextID++
		if nextID == 1 {
			return "attempt-first"
		}
		return "attempt-second"
	}})

	cmd := action.StartActionAttemptCommand{
		ActionID:       "action-1",
		RunID:          "run-1",
		TaskID:         "task-1",
		LeaseID:        "lease-1",
		HolderType:     model.HolderAgent,
		HolderID:       "agent-1",
		TaskVersion:    1,
		ToolName:       "deploy",
		IdempotencyKey: "idem-1",
		InputHash:      "hash-1",
	}
	first, err := bus.Execute(ctx, uow, cmd)
	if err != nil {
		t.Fatalf("first StartActionAttempt error = %v", err)
	}
	second, err := bus.Execute(ctx, uow, cmd)
	if err != nil {
		t.Fatalf("second StartActionAttempt error = %v", err)
	}
	if first.(model.ActionAttempt).AttemptID != second.(model.ActionAttempt).AttemptID {
		t.Fatalf("expected existing attempt, got first=%#v second=%#v", first, second)
	}
	events, err := uow.Events().ListEvents(ctx, "run-1")
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected duplicate idempotency start to avoid duplicate event, got %d events", len(events))
	}
}

func TestStartActionAttemptReauthorizesIdempotentReplay(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()
	saveActionFixture(ctx, t, uow, true)

	denied := errors.New("current action policy denied replay")
	authorizations := 0
	bus := commandbus.NewBus()
	action.RegisterHandlers(bus, action.HandlerOptions{
		NewID: func(string) string { return "attempt-1" },
		Authorize: func(
			context.Context,
			ports.UnitOfWork,
			model.PolicyRequest,
		) (model.PolicyDecision, error) {
			authorizations++
			if authorizations == 2 {
				return model.PolicyDecision{}, denied
			}
			return model.PolicyDecision{Effect: model.PolicyEffectAllow}, nil
		},
	})
	cmd := action.StartActionAttemptCommand{
		ActionID: "action-1", RunID: "run-1", TaskID: "task-1", LeaseID: "lease-1",
		HolderType: model.HolderAgent, HolderID: "agent-1", TaskVersion: 1,
		ToolName: "deploy", IdempotencyKey: "idem-1", InputHash: "hash-1",
	}
	if _, err := bus.Execute(ctx, uow, cmd); err != nil {
		t.Fatalf("first StartActionAttempt error = %v", err)
	}
	if _, err := bus.Execute(ctx, uow, cmd); !errors.Is(err, denied) {
		t.Fatalf("replayed StartActionAttempt error = %v, want current policy denial", err)
	}
	if authorizations != 2 {
		t.Fatalf("action authorizations = %d, want one per start attempt", authorizations)
	}
}

func TestStartActionAttemptUnderReplacementLeaseRequiresReconciliation(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = uow.Rollback(ctx) }()
	saveActionFixture(ctx, t, uow, true)
	bus := commandbus.NewBus()
	action.RegisterHandlers(bus, action.HandlerOptions{NewID: func(string) string { return "attempt-1" }})
	cmd := action.StartActionAttemptCommand{
		ActionID: "action-1", RunID: "run-1", TaskID: "task-1", LeaseID: "lease-1",
		HolderType: model.HolderAgent, HolderID: "agent-1", TaskVersion: 1,
		ToolName: "deploy", IdempotencyKey: "operation:turn:1:call:0", InputHash: "hash-1",
	}
	firstRaw, err := bus.Execute(ctx, uow, cmd)
	if err != nil {
		t.Fatal(err)
	}
	first := firstRaw.(model.ActionAttempt)
	if first.Status != model.ActionAttemptRunning || first.LeaseID != "lease-1" {
		t.Fatalf("first attempt = %#v", first)
	}
	oldLease, err := uow.Leases().LoadLease(ctx, "lease-1")
	if err != nil {
		t.Fatal(err)
	}
	oldLease.Status = model.LeaseStatusReleased
	if err := uow.Leases().SaveLease(ctx, oldLease); err != nil {
		t.Fatal(err)
	}
	oldLease, err = uow.Leases().LoadLease(ctx, "lease-1")
	if err != nil {
		t.Fatal(err)
	}
	acquired, err := uow.Leases().AcquireWithExpectedVersion(ctx, model.TaskExecutionLease{
		ID: "lease-2", RunID: "run-1", TaskID: "task-1",
		HolderType: model.HolderAgent, HolderID: "agent-1", TaskVersion: 1,
		Status: model.LeaseStatusActive, ExpiresAt: time.Now().Add(time.Hour),
	}, oldLease.Version)
	if err != nil || !acquired {
		t.Fatalf("replacement lease acquired=%v error=%v", acquired, err)
	}
	cmd.LeaseID = "lease-2"
	replayedRaw, err := bus.Execute(ctx, uow, cmd)
	if err != nil {
		t.Fatalf("replayed StartActionAttempt error = %v", err)
	}
	replayed := replayedRaw.(model.ActionAttempt)
	if replayed.AttemptID != first.AttemptID || replayed.LeaseID != "lease-1" ||
		replayed.Status != model.ActionAttemptUnknown || !replayed.RequiresReconcile {
		t.Fatalf("replayed attempt = %#v", replayed)
	}
	task, err := uow.Tasks().LoadTask(ctx, "run-1", "task-1")
	if err != nil {
		t.Fatal(err)
	}
	run, err := uow.Runs().LoadRun(ctx, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := uow.Leases().LoadLease(ctx, "lease-2")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != model.TaskStatusReconcileRequired ||
		run.Status != model.RunStatusReconcileRequired ||
		replacement.Status != model.LeaseStatusReleased {
		t.Fatalf("replay state task=%#v run=%#v lease=%#v", task, run, replacement)
	}
}

func TestStartActionAttemptRejectsSameIdempotencyKeyWithDifferentInputHash(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()
	saveActionFixture(ctx, t, uow, true)

	nextID := 0
	bus := commandbus.NewBus()
	action.RegisterHandlers(bus, action.HandlerOptions{NewID: func(string) string {
		nextID++
		if nextID == 1 {
			return "attempt-first"
		}
		return "attempt-second"
	}})

	base := action.StartActionAttemptCommand{
		ActionID:       "action-1",
		RunID:          "run-1",
		TaskID:         "task-1",
		LeaseID:        "lease-1",
		HolderType:     model.HolderAgent,
		HolderID:       "agent-1",
		TaskVersion:    1,
		ToolName:       "deploy",
		IdempotencyKey: "idem-1",
		InputHash:      "hash-1",
	}
	if _, err := bus.Execute(ctx, uow, base); err != nil {
		t.Fatalf("first StartActionAttempt error = %v", err)
	}
	base.InputHash = "hash-2"
	_, err = bus.Execute(ctx, uow, base)
	if !errors.Is(err, model.ErrIdempotencyConflict) {
		t.Fatalf("expected ErrIdempotencyConflict, got %v", err)
	}
}

func TestStartActionAttemptWithoutIdempotencyKeyCreatesDistinctAttempts(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()
	saveActionFixture(ctx, t, uow, true)

	nextID := 0
	bus := commandbus.NewBus()
	action.RegisterHandlers(bus, action.HandlerOptions{NewID: func(string) string {
		nextID++
		if nextID == 1 {
			return "attempt-first"
		}
		return "attempt-second"
	}})

	cmd := action.StartActionAttemptCommand{
		ActionID:    "action-1",
		RunID:       "run-1",
		TaskID:      "task-1",
		LeaseID:     "lease-1",
		HolderType:  model.HolderAgent,
		HolderID:    "agent-1",
		TaskVersion: 1,
		ToolName:    "deploy",
	}
	first, err := bus.Execute(ctx, uow, cmd)
	if err != nil {
		t.Fatalf("first StartActionAttempt error = %v", err)
	}
	second, err := bus.Execute(ctx, uow, cmd)
	if err != nil {
		t.Fatalf("second StartActionAttempt error = %v", err)
	}
	if first.(model.ActionAttempt).AttemptID == second.(model.ActionAttempt).AttemptID {
		t.Fatalf("expected distinct attempts without idempotency key, got first=%#v second=%#v", first, second)
	}
}

func TestResolveActionAttemptResumesReconcileRequiredTaskAndRun(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()
	saveReconcileFixture(ctx, t, uow)

	bus := commandbus.NewBus()
	action.RegisterHandlers(bus, action.HandlerOptions{})
	cmd := action.ResolveActionAttemptCommand{
		AttemptID:         "attempt-1",
		Status:            model.ActionAttemptSucceeded,
		ExternalResultRef: "result://deploy-1",
	}
	raw, err := bus.Execute(ctx, uow, cmd)
	if err != nil {
		t.Fatalf("ResolveActionAttempt error = %v", err)
	}
	result := raw.(action.ResolveAttemptResult)
	if result.Attempt.Status != model.ActionAttemptSucceeded || result.Attempt.RequiresReconcile {
		t.Fatalf("attempt = %#v", result.Attempt)
	}
	if result.Task.Status != model.TaskStatusDispatched || !result.TaskTransition {
		t.Fatalf("task = %#v, transitioned = %t", result.Task, result.TaskTransition)
	}
	if result.Run.Status != model.RunStatusRunning || !result.RunTransition {
		t.Fatalf("run = %#v, transitioned = %t", result.Run, result.RunTransition)
	}
	if result.Envelope.ID == "" ||
		result.Envelope.Status != "pending" ||
		result.Envelope.TaskVersion != result.Task.Version ||
		result.Envelope.TargetAgentID != result.Task.OwnerAgentID {
		t.Fatalf("redispatched envelope = %#v", result.Envelope)
	}
	envelopes, err := uow.MailboxOutbox().ListEnvelopes(ctx, "run-1")
	if err != nil {
		t.Fatalf("ListEnvelopes() error = %v", err)
	}
	if len(envelopes) != 1 || envelopes[0].ID != result.Envelope.ID {
		t.Fatalf("redispatched envelopes = %#v, want %#v", envelopes, result.Envelope)
	}
	events, err := uow.Events().ListEvents(ctx, "run-1")
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(events) != 3 ||
		events[0].Type != model.EventActionAttemptUpdated ||
		events[1].Type != model.EventTaskDispatched ||
		events[2].Type != model.EventRunStatusChanged {
		t.Fatalf("events = %#v", events)
	}

	duplicate, err := bus.Execute(ctx, uow, cmd)
	if err != nil {
		t.Fatalf("duplicate ResolveActionAttempt error = %v", err)
	}
	if !reflect.DeepEqual(duplicate.(action.ResolveAttemptResult).Attempt, result.Attempt) {
		t.Fatalf("duplicate result = %#v, want %#v", duplicate, result.Attempt)
	}
	events, err = uow.Events().ListEvents(ctx, "run-1")
	if err != nil {
		t.Fatalf("ListEvents() after duplicate error = %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("duplicate resolution appended events: %d", len(events))
	}
	envelopes, err = uow.MailboxOutbox().ListEnvelopes(ctx, "run-1")
	if err != nil {
		t.Fatalf("ListEnvelopes() after duplicate error = %v", err)
	}
	if len(envelopes) != 1 {
		t.Fatalf("duplicate resolution queued envelopes: %d", len(envelopes))
	}
}

func TestResolveActionAttemptPreservesRemainingReconciliationBarriers(t *testing.T) {
	t.Run("same task", func(t *testing.T) {
		ctx := context.Background()
		provider := memory.NewProvider()
		uow, err := provider.Begin(ctx)
		if err != nil {
			t.Fatalf("Begin() error = %v", err)
		}
		defer func() { _ = uow.Rollback(ctx) }()
		saveReconcileFixture(ctx, t, uow)
		if err := uow.ActionAttempts().SaveActionAttempt(ctx, model.ActionAttempt{
			AttemptID:         "attempt-2",
			ActionID:          "action-2",
			RunID:             "run-1",
			TaskID:            "task-1",
			ToolName:          "deploy",
			Status:            model.ActionAttemptUnknown,
			IdempotencyKey:    "idem-2",
			InputHash:         "hash-2",
			RequiresReconcile: true,
		}); err != nil {
			t.Fatalf("SaveActionAttempt() error = %v", err)
		}
		bus := commandbus.NewBus()
		action.RegisterHandlers(bus, action.HandlerOptions{})
		first, err := bus.Execute(ctx, uow, action.ResolveActionAttemptCommand{
			AttemptID: "attempt-1",
			Status:    model.ActionAttemptSucceeded,
		})
		if err != nil {
			t.Fatalf("first ResolveActionAttempt error = %v", err)
		}
		firstResult := first.(action.ResolveAttemptResult)
		if firstResult.TaskTransition || firstResult.RunTransition ||
			firstResult.Task.Status != model.TaskStatusReconcileRequired ||
			firstResult.Run.Status != model.RunStatusReconcileRequired {
			t.Fatalf("first resolution crossed remaining barrier: %#v", firstResult)
		}
		second, err := bus.Execute(ctx, uow, action.ResolveActionAttemptCommand{
			AttemptID: "attempt-2",
			Status:    model.ActionAttemptTimeout,
		})
		if err != nil {
			t.Fatalf("timeout ResolveActionAttempt error = %v", err)
		}
		secondResult := second.(action.ResolveAttemptResult)
		if !secondResult.TaskTransition || !secondResult.RunTransition ||
			secondResult.Task.Status != model.TaskStatusDispatched ||
			secondResult.Run.Status != model.RunStatusRunning {
			t.Fatalf("final resolution did not clear barriers: %#v", secondResult)
		}
	})

	t.Run("other task", func(t *testing.T) {
		ctx := context.Background()
		provider := memory.NewProvider()
		uow, err := provider.Begin(ctx)
		if err != nil {
			t.Fatalf("Begin() error = %v", err)
		}
		defer func() { _ = uow.Rollback(ctx) }()
		saveReconcileFixture(ctx, t, uow)
		if err := uow.Tasks().SaveTask(ctx, model.Task{
			ID:           "task-2",
			RunID:        "run-1",
			Status:       model.TaskStatusReconcileRequired,
			Version:      1,
			OwnerAgentID: "agent-2",
			AllowsAction: true,
		}); err != nil {
			t.Fatalf("SaveTask() error = %v", err)
		}
		bus := commandbus.NewBus()
		action.RegisterHandlers(bus, action.HandlerOptions{})
		raw, err := bus.Execute(ctx, uow, action.ResolveActionAttemptCommand{
			AttemptID: "attempt-1",
			Status:    model.ActionAttemptSucceeded,
		})
		if err != nil {
			t.Fatalf("ResolveActionAttempt error = %v", err)
		}
		result := raw.(action.ResolveAttemptResult)
		if !result.TaskTransition || result.RunTransition ||
			result.Task.Status != model.TaskStatusDispatched ||
			result.Run.Status != model.RunStatusReconcileRequired {
			t.Fatalf("resolution ignored another task barrier: %#v", result)
		}
	})
}

func TestResolveActionAttemptAcceptsTimeoutDecision(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()
	saveReconcileFixture(ctx, t, uow)

	bus := commandbus.NewBus()
	action.RegisterHandlers(bus, action.HandlerOptions{})
	raw, err := bus.Execute(ctx, uow, action.ResolveActionAttemptCommand{
		AttemptID:         "attempt-1",
		Status:            model.ActionAttemptTimeout,
		ExternalResultRef: "timeout://operator-confirmed",
	})
	if err != nil {
		t.Fatalf("ResolveActionAttempt(timeout) error = %v", err)
	}
	result := raw.(action.ResolveAttemptResult)
	if result.Attempt.Status != model.ActionAttemptTimeout ||
		result.Attempt.RequiresReconcile ||
		result.Task.Status != model.TaskStatusDispatched ||
		result.Envelope.Status != "pending" {
		t.Fatalf("timeout reconciliation result = %#v", result)
	}
}

func TestResolveActionAttemptRejectsConflictingDecision(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()
	saveReconcileFixture(ctx, t, uow)

	bus := commandbus.NewBus()
	action.RegisterHandlers(bus, action.HandlerOptions{})
	if _, err := bus.Execute(ctx, uow, action.ResolveActionAttemptCommand{
		AttemptID: "attempt-1",
		Status:    model.ActionAttemptFailed,
	}); err != nil {
		t.Fatalf("first ResolveActionAttempt error = %v", err)
	}
	_, err = bus.Execute(ctx, uow, action.ResolveActionAttemptCommand{
		AttemptID: "attempt-1",
		Status:    model.ActionAttemptSucceeded,
	})
	if !errors.Is(err, model.ErrIdempotencyConflict) {
		t.Fatalf("conflicting ResolveActionAttempt error = %v, want ErrIdempotencyConflict", err)
	}
}

func TestCompleteActionAttemptRejectsAttemptFromDifferentLease(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()
	saveActionFixture(ctx, t, uow, true)
	if err := uow.ActionAttempts().SaveActionAttempt(ctx, model.ActionAttempt{
		AttemptID: "attempt-old", RunID: "run-1", TaskID: "task-1",
		LeaseID: "lease-old", Status: model.ActionAttemptRunning,
	}); err != nil {
		t.Fatalf("SaveActionAttempt() error = %v", err)
	}

	bus := commandbus.NewBus()
	action.RegisterHandlers(bus, action.HandlerOptions{})
	_, err = bus.Execute(ctx, uow, action.CompleteActionAttemptCommand{
		RunID: "run-1", TaskID: "task-1", LeaseID: "lease-1",
		HolderType: model.HolderAgent, HolderID: "agent-1", TaskVersion: 1,
		AttemptID: "attempt-old", Status: model.ActionAttemptSucceeded,
	})
	if !errors.Is(err, model.ErrLeaseNotActive) {
		t.Fatalf("CompleteActionAttempt error = %v, want ErrLeaseNotActive", err)
	}
	stored, err := uow.ActionAttempts().LoadActionAttempt(ctx, "attempt-old")
	if err != nil {
		t.Fatalf("LoadActionAttempt() error = %v", err)
	}
	if stored.Status != model.ActionAttemptRunning {
		t.Fatalf("rejected completion mutated attempt to %q", stored.Status)
	}
}

func TestCompleteActionAttemptRejectsInvalidToolResultWithoutMutation(t *testing.T) {
	tests := []struct {
		name   string
		result json.RawMessage
	}{
		{name: "wrong shape", result: json.RawMessage(`"not-a-tool-result"`)},
		{name: "too large", result: json.RawMessage(`"` + strings.Repeat("x", 8<<20) + `"`)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			provider := memory.NewProvider()
			uow, err := provider.Begin(ctx)
			if err != nil {
				t.Fatalf("Begin() error = %v", err)
			}
			defer func() { _ = uow.Rollback(ctx) }()
			saveActionFixture(ctx, t, uow, true)
			if err := uow.ActionAttempts().SaveActionAttempt(ctx, model.ActionAttempt{
				AttemptID: "attempt-1", RunID: "run-1", TaskID: "task-1",
				LeaseID: "lease-1", Status: model.ActionAttemptRunning,
			}); err != nil {
				t.Fatalf("SaveActionAttempt() error = %v", err)
			}

			bus := commandbus.NewBus()
			action.RegisterHandlers(bus, action.HandlerOptions{})
			_, err = bus.Execute(ctx, uow, action.CompleteActionAttemptCommand{
				RunID: "run-1", TaskID: "task-1", LeaseID: "lease-1",
				HolderType: model.HolderAgent, HolderID: "agent-1", TaskVersion: 1,
				AttemptID: "attempt-1", Status: model.ActionAttemptSucceeded,
				ToolResult: test.result,
			})
			if !errors.Is(err, model.ErrInvalidCommand) {
				t.Fatalf("CompleteActionAttempt error = %v, want ErrInvalidCommand", err)
			}
			stored, err := uow.ActionAttempts().LoadActionAttempt(ctx, "attempt-1")
			if err != nil {
				t.Fatalf("LoadActionAttempt() error = %v", err)
			}
			if stored.Status != model.ActionAttemptRunning || len(stored.ToolResult) != 0 {
				t.Fatalf("rejected completion mutated attempt: %#v", stored)
			}
		})
	}
}

func saveReconcileFixture(ctx context.Context, t *testing.T, uow ports.UnitOfWork) {
	t.Helper()
	if err := uow.Runs().SaveRun(ctx, model.Run{ID: "run-1", RootTaskID: "task-1", Status: model.RunStatusReconcileRequired}); err != nil {
		t.Fatalf("SaveRun() error = %v", err)
	}
	if err := uow.Tasks().SaveTask(ctx, model.Task{ID: "task-1", RunID: "run-1", Status: model.TaskStatusReconcileRequired, Version: 1, OwnerAgentID: "agent-1", AllowsAction: true}); err != nil {
		t.Fatalf("SaveTask() error = %v", err)
	}
	if err := uow.ActionAttempts().SaveActionAttempt(ctx, model.ActionAttempt{
		AttemptID:         "attempt-1",
		ActionID:          "action-1",
		RunID:             "run-1",
		TaskID:            "task-1",
		ToolName:          "deploy",
		Status:            model.ActionAttemptUnknown,
		IdempotencyKey:    "idem-1",
		InputHash:         "hash-1",
		RequiresReconcile: true,
	}); err != nil {
		t.Fatalf("SaveActionAttempt() error = %v", err)
	}
}

func saveActionFixture(ctx context.Context, t *testing.T, uow ports.UnitOfWork, allowsAction bool) {
	t.Helper()
	if err := uow.Runs().SaveRun(ctx, model.Run{ID: "run-1", RootTaskID: "task-1", Status: model.RunStatusRunning}); err != nil {
		t.Fatalf("SaveRun() error = %v", err)
	}
	if err := uow.Tasks().SaveTask(ctx, model.Task{ID: "task-1", RunID: "run-1", Status: model.TaskStatusRunning, Version: 1, OwnerAgentID: "agent-1", AllowsAction: allowsAction}); err != nil {
		t.Fatalf("SaveTask() error = %v", err)
	}
	if err := uow.Leases().SaveLease(ctx, model.TaskExecutionLease{ID: "lease-1", RunID: "run-1", TaskID: "task-1", HolderType: model.HolderAgent, HolderID: "agent-1", TaskVersion: 1, Status: model.LeaseStatusActive, ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatalf("SaveLease() error = %v", err)
	}
}
