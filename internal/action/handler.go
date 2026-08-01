package action

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	commandbus "github.com/Viking602/venat/internal/command"
	"github.com/Viking602/venat/internal/core/model"
	"github.com/Viking602/venat/internal/core/ports"
	corestate "github.com/Viking602/venat/internal/core/state"
	"github.com/Viking602/venat/internal/eventpayload"
	"github.com/Viking602/venat/internal/execution"
	"github.com/Viking602/venat/message"
)

const maxActionAttemptResultBytes = 8 << 20

type IDGenerator func(string) string

type Authorizer func(context.Context, ports.UnitOfWork, model.PolicyRequest) (model.PolicyDecision, error)

type HandlerOptions struct {
	NewID     IDGenerator
	Authorize Authorizer
}

func RegisterHandlers(bus *commandbus.Bus, options HandlerOptions) {
	commandbus.Register[StartActionAttemptCommand](bus, startActionAttemptHandler{options: options})
	commandbus.Register[CompleteActionAttemptCommand](bus, completeActionAttemptHandler{})
	commandbus.Register[ResolveActionAttemptCommand](bus, resolveActionAttemptHandler{options: options})
}

type CompleteAttemptResult struct {
	Attempt       model.ActionAttempt
	Task          model.Task
	Run           model.Run
	Lease         model.TaskExecutionLease
	Reconcile     bool
	RunTransition bool
}

type ResolveAttemptResult struct {
	Attempt        model.ActionAttempt
	Task           model.Task
	Run            model.Run
	Envelope       model.TaskEnvelope
	TaskTransition bool
	RunTransition  bool
}

type startActionAttemptHandler struct{ options HandlerOptions }

func (startActionAttemptHandler) Name() string { return StartActionAttemptCommand{}.CommandName() }

func (h startActionAttemptHandler) Handle(ctx context.Context, uow ports.UnitOfWork, cmd StartActionAttemptCommand) (any, error) {
	run, task, lease, err := execution.ValidateSubmission(ctx, uow, cmd.RunID, cmd.TaskID, cmd.LeaseID, cmd.HolderType, cmd.HolderID, cmd.TaskVersion)
	if err != nil {
		return nil, err
	}
	if !task.AllowsAction {
		return nil, model.ErrActionTaskRequired
	}
	if cmd.IdempotencyKey != "" {
		existing, err := uow.ActionAttempts().LoadActionAttemptByIdempotencyKey(ctx, cmd.RunID, cmd.TaskID, cmd.ToolName, cmd.IdempotencyKey)
		if err == nil {
			if existing.InputHash != cmd.InputHash {
				return nil, model.ErrIdempotencyConflict
			}
			if existing.Status == model.ActionAttemptRunning && existing.LeaseID != cmd.LeaseID {
				existing.Status = model.ActionAttemptUnknown
				existing.RequiresReconcile = true
				if err := uow.ActionAttempts().SaveActionAttempt(ctx, existing); err != nil {
					return nil, err
				}
				if err := uow.Events().AppendEvent(ctx, model.Event{
					RunID: cmd.RunID, TaskID: cmd.TaskID, Type: model.EventActionAttemptUpdated,
					Payload: map[string]any{
						"attemptId": existing.AttemptID, "status": string(existing.Status),
						"requiresReconcile": true, "replayedUnderLeaseId": cmd.LeaseID,
					},
					RecordedAt: time.Now().UTC(),
				}); err != nil {
					return nil, err
				}
				reconciled, err := reconcileAttempt(ctx, uow, run, task, lease, existing)
				if err != nil {
					return nil, err
				}
				return reconciled.Attempt, nil
			}
			return existing, nil
		}
		if !errors.Is(err, model.ErrNotFound) {
			return nil, err
		}
	}
	attemptID := cmd.AttemptID
	if attemptID == "" {
		attemptID = h.options.NewID("attempt")
	}
	attempt := model.ActionAttempt{AttemptID: attemptID, ActionID: cmd.ActionID, RunID: cmd.RunID, TaskID: cmd.TaskID, LeaseID: cmd.LeaseID, ToolName: cmd.ToolName, Status: model.ActionAttemptRunning, IdempotencyKey: cmd.IdempotencyKey, InputHash: cmd.InputHash}
	if h.options.Authorize != nil {
		if _, err := h.options.Authorize(ctx, uow, model.PolicyRequest{Operation: model.PolicyOperationAction, RunID: cmd.RunID, TaskID: cmd.TaskID, Actor: actorFromHolder(cmd.HolderType, cmd.HolderID), Action: &attempt}); err != nil {
			return nil, err
		}
	}
	if err := uow.ActionAttempts().SaveActionAttempt(ctx, attempt); err != nil {
		return nil, err
	}
	if err := uow.Events().AppendEvent(ctx, model.Event{RunID: cmd.RunID, TaskID: cmd.TaskID, Type: model.EventActionAttemptStarted, Payload: map[string]any{"attemptId": attempt.AttemptID, "actionId": attempt.ActionID, "leaseId": attempt.LeaseID, "toolName": attempt.ToolName, "idempotencyKey": attempt.IdempotencyKey}, RecordedAt: time.Now().UTC()}); err != nil {
		return nil, err
	}
	return attempt, nil
}

type completeActionAttemptHandler struct{}

func (completeActionAttemptHandler) Name() string {
	return CompleteActionAttemptCommand{}.CommandName()
}

func (completeActionAttemptHandler) Handle(ctx context.Context, uow ports.UnitOfWork, cmd CompleteActionAttemptCommand) (any, error) {
	if cmd.LeaseID == "" {
		return nil, model.ErrLeaseNotActive
	}
	run, task, lease, err := execution.ValidateSubmission(ctx, uow, cmd.RunID, cmd.TaskID, cmd.LeaseID, cmd.HolderType, cmd.HolderID, cmd.TaskVersion)
	if err != nil {
		return nil, err
	}
	if !task.AllowsAction {
		return nil, model.ErrActionTaskRequired
	}
	attempt, err := uow.ActionAttempts().LoadActionAttempt(ctx, cmd.AttemptID)
	if err != nil {
		return nil, err
	}
	if attempt.RunID != cmd.RunID || attempt.TaskID != cmd.TaskID {
		return nil, model.ErrNotFound
	}
	if attempt.LeaseID != cmd.LeaseID {
		return nil, model.ErrLeaseNotActive
	}
	if !isTerminalAttemptStatus(cmd.Status) {
		return nil, model.ErrInvalidCommand
	}
	if err := validateActionAttemptResult(cmd.ExternalResultRef, cmd.ToolResult); err != nil {
		return nil, err
	}
	if isTerminalAttemptStatus(attempt.Status) {
		if sameTerminalAttempt(attempt, cmd) {
			return CompleteAttemptResult{Attempt: attempt}, nil
		}
		return nil, model.ErrIdempotencyConflict
	}
	attempt.Status = cmd.Status
	attempt.ExternalRequestID = cmd.ExternalRequestID
	attempt.ExternalResultRef = cmd.ExternalResultRef
	attempt.ToolResult = append(attempt.ToolResult[:0], cmd.ToolResult...)
	attempt.RequiresReconcile = cmd.RequiresReconcile || cmd.Status == model.ActionAttemptUnknown
	if err := uow.ActionAttempts().SaveActionAttempt(ctx, attempt); err != nil {
		return nil, err
	}
	if err := uow.Events().AppendEvent(ctx, model.Event{RunID: cmd.RunID, TaskID: cmd.TaskID, Type: model.EventActionAttemptUpdated, Payload: map[string]any{"attemptId": attempt.AttemptID, "status": string(attempt.Status), "externalRequestId": attempt.ExternalRequestID, "externalResultRef": attempt.ExternalResultRef, "requiresReconcile": attempt.RequiresReconcile}, RecordedAt: time.Now().UTC()}); err != nil {
		return nil, err
	}
	result := CompleteAttemptResult{Attempt: attempt}
	if !attempt.RequiresReconcile {
		return result, nil
	}
	return reconcileAttempt(ctx, uow, run, task, lease, attempt)
}

type resolveActionAttemptHandler struct{ options HandlerOptions }

func (resolveActionAttemptHandler) Name() string {
	return ResolveActionAttemptCommand{}.CommandName()
}

func (h resolveActionAttemptHandler) Handle(ctx context.Context, uow ports.UnitOfWork, cmd ResolveActionAttemptCommand) (any, error) {
	if cmd.AttemptID == "" || !isResolutionAttemptStatus(cmd.Status) {
		return nil, model.ErrInvalidCommand
	}
	if err := validateActionAttemptResult(cmd.ExternalResultRef, cmd.ToolResult); err != nil {
		return nil, err
	}
	attempt, err := uow.ActionAttempts().LoadActionAttempt(ctx, cmd.AttemptID)
	if err != nil {
		return nil, err
	}
	if !attempt.RequiresReconcile || attempt.Status != model.ActionAttemptUnknown {
		if sameResolvedAttempt(attempt, cmd) {
			return ResolveAttemptResult{Attempt: attempt}, nil
		}
		return nil, model.ErrIdempotencyConflict
	}
	task, err := uow.Tasks().LoadTask(ctx, attempt.RunID, attempt.TaskID)
	if err != nil {
		return nil, err
	}
	run, err := uow.Runs().LoadRun(ctx, attempt.RunID)
	if err != nil {
		return nil, err
	}
	resolved := attempt
	resolved.Status = cmd.Status
	resolved.ExternalResultRef = cmd.ExternalResultRef
	resolved.ToolResult = append(resolved.ToolResult[:0], cmd.ToolResult...)
	resolved.RequiresReconcile = false
	swapped, err := uow.ActionAttempts().ResolveActionAttempt(ctx, resolved)
	if err != nil {
		return nil, err
	}
	if !swapped {
		return nil, model.ErrIdempotencyConflict
	}

	result, err := applyResolvedAttemptState(ctx, uow, task, run, resolved, h.options.NewID)
	if err != nil {
		return nil, err
	}
	if err := appendResolvedAttemptEvents(ctx, uow, attempt, run, result); err != nil {
		return nil, err
	}
	return result, nil
}

func applyResolvedAttemptState(
	ctx context.Context,
	uow ports.UnitOfWork,
	task model.Task,
	run model.Run,
	resolved model.ActionAttempt,
	newID IDGenerator,
) (ResolveAttemptResult, error) {
	result := ResolveAttemptResult{Attempt: resolved, Task: task, Run: run}
	requiresReconcile := true
	pendingAttempts, err := uow.ActionAttempts().ListActionAttempts(ctx, model.ActionAttemptSelector{
		RunID:             resolved.RunID,
		Statuses:          []model.ActionAttemptStatus{model.ActionAttemptUnknown},
		RequiresReconcile: &requiresReconcile,
	})
	if err != nil {
		return ResolveAttemptResult{}, err
	}
	taskStillBlocked := false
	for _, pending := range pendingAttempts {
		if pending.TaskID == task.ID {
			taskStillBlocked = true
			break
		}
	}
	if task.Status == model.TaskStatusReconcileRequired && !taskStillBlocked {
		nextTask, err := corestate.TransitionTask(task, model.TaskStatusDispatched, false)
		if err != nil {
			return ResolveAttemptResult{}, err
		}
		nextTask.Error = ""
		if err := uow.Tasks().SaveTask(ctx, nextTask); err != nil {
			return ResolveAttemptResult{}, err
		}
		envelopeID := "env-" + resolved.AttemptID
		if newID != nil {
			envelopeID = newID("env")
		}
		envelope := model.TaskEnvelope{
			ID:              envelopeID,
			RunID:           nextTask.RunID,
			TaskID:          nextTask.ID,
			TargetAgentID:   nextTask.OwnerAgentID,
			TargetComponent: nextTask.OwnerComponent,
			Type:            "TaskEnvelope",
			Status:          "pending",
			TaskVersion:     nextTask.Version,
			ReadSelectors:   slices.Clone(nextTask.ReadSelectors),
			WriteTargets:    slices.Clone(nextTask.WriteTargets),
			RetryPolicy:     nextTask.RetryPolicy,
			CreatedAt:       time.Now().UTC(),
		}
		if err := uow.MailboxOutbox().QueueEnvelope(ctx, envelope); err != nil {
			return ResolveAttemptResult{}, err
		}
		result.Envelope = envelope
		result.Task = nextTask
		result.TaskTransition = true
	}

	tasks, err := uow.Tasks().ListTasks(ctx, run.ID)
	if err != nil {
		return ResolveAttemptResult{}, err
	}
	runStillBlocked := false
	for _, candidate := range tasks {
		if candidate.Status == model.TaskStatusReconcileRequired {
			runStillBlocked = true
			break
		}
	}
	if run.Status == model.RunStatusReconcileRequired && !runStillBlocked {
		nextRun, err := corestate.TransitionRun(run, model.RunStatusRunning)
		if err != nil {
			return ResolveAttemptResult{}, err
		}
		if err := uow.Runs().SaveRun(ctx, nextRun); err != nil {
			return ResolveAttemptResult{}, err
		}
		result.Run = nextRun
		result.RunTransition = true
	}
	return result, nil
}

func appendResolvedAttemptEvents(
	ctx context.Context,
	uow ports.UnitOfWork,
	attempt model.ActionAttempt,
	previousRun model.Run,
	result ResolveAttemptResult,
) error {
	now := time.Now().UTC()
	if err := uow.Events().AppendEvent(ctx, model.Event{
		RunID:  attempt.RunID,
		TaskID: attempt.TaskID,
		Type:   model.EventActionAttemptUpdated,
		Payload: map[string]any{
			"attemptId":         result.Attempt.AttemptID,
			"status":            string(result.Attempt.Status),
			"externalResultRef": result.Attempt.ExternalResultRef,
			"requiresReconcile": false,
			"resolved":          true,
			"task":              eventpayload.Task(result.Task),
		},
		RecordedAt: now,
	}); err != nil {
		return err
	}
	if result.TaskTransition {
		if err := uow.Events().AppendEvent(ctx, model.Event{
			RunID:      result.Task.RunID,
			TaskID:     result.Task.ID,
			Type:       model.EventTaskDispatched,
			Payload:    map[string]any{"reason": "action_reconciled", "task": eventpayload.Task(result.Task), "envelope": eventpayload.Envelope(result.Envelope)},
			RecordedAt: now,
		}); err != nil {
			return err
		}
	}
	if result.RunTransition {
		if err := uow.Events().AppendEvent(ctx, model.Event{
			RunID:      result.Run.ID,
			TaskID:     result.Run.RootTaskID,
			Type:       model.EventRunStatusChanged,
			Payload:    map[string]any{"from": string(previousRun.Status), "to": string(result.Run.Status), "run": eventpayload.Run(result.Run)},
			RecordedAt: now,
		}); err != nil {
			return err
		}
	}
	return nil
}

func sameResolvedAttempt(attempt model.ActionAttempt, cmd ResolveActionAttemptCommand) bool {
	return !attempt.RequiresReconcile &&
		attempt.Status == cmd.Status &&
		attempt.ExternalResultRef == cmd.ExternalResultRef &&
		bytes.Equal(attempt.ToolResult, cmd.ToolResult)
}

func isResolutionAttemptStatus(status model.ActionAttemptStatus) bool {
	switch status {
	case model.ActionAttemptSucceeded, model.ActionAttemptFailed, model.ActionAttemptTimeout, model.ActionAttemptCancelled:
		return true
	default:
		return false
	}
}

func sameTerminalAttempt(attempt model.ActionAttempt, cmd CompleteActionAttemptCommand) bool {
	return attempt.Status == cmd.Status &&
		attempt.ExternalRequestID == cmd.ExternalRequestID &&
		attempt.ExternalResultRef == cmd.ExternalResultRef &&
		bytes.Equal(attempt.ToolResult, cmd.ToolResult) &&
		attempt.RequiresReconcile == (cmd.RequiresReconcile || cmd.Status == model.ActionAttemptUnknown)
}

func validateActionAttemptResult(externalResultRef string, toolResult json.RawMessage) error {
	if len(externalResultRef) > maxActionAttemptResultBytes || len(toolResult) > maxActionAttemptResultBytes {
		return fmt.Errorf(
			"action: result exceeds %d bytes: %w",
			maxActionAttemptResultBytes,
			model.ErrInvalidCommand,
		)
	}
	if len(toolResult) == 0 {
		return nil
	}
	var result message.ToolResult
	if err := json.Unmarshal(toolResult, &result); err != nil {
		return fmt.Errorf("action: invalid tool result: %w: %v", model.ErrInvalidCommand, err)
	}
	return nil
}

func reconcileAttempt(ctx context.Context, uow ports.UnitOfWork, run model.Run, task model.Task, lease model.TaskExecutionLease, attempt model.ActionAttempt) (CompleteAttemptResult, error) {
	nextTask, err := corestate.TransitionTask(task, model.TaskStatusReconcileRequired, true)
	if err != nil {
		return CompleteAttemptResult{}, err
	}
	nextTask.Error = "action attempt requires reconciliation"
	if err := uow.Tasks().SaveTask(ctx, nextTask); err != nil {
		return CompleteAttemptResult{}, err
	}
	lease.Status = model.LeaseStatusReleased
	if err := uow.Leases().SaveLease(ctx, lease); err != nil {
		return CompleteAttemptResult{}, err
	}
	if err := uow.Events().AppendEvent(ctx, model.Event{RunID: lease.RunID, TaskID: lease.TaskID, Type: model.EventTaskExecutionReleased, Payload: map[string]any{"leaseId": lease.ID}, RecordedAt: time.Now().UTC()}); err != nil {
		return CompleteAttemptResult{}, err
	}
	nextRun, err := corestate.TransitionRun(run, model.RunStatusReconcileRequired)
	if err != nil {
		return CompleteAttemptResult{}, err
	}
	if err := uow.Runs().SaveRun(ctx, nextRun); err != nil {
		return CompleteAttemptResult{}, err
	}
	if err := uow.Events().AppendEvent(ctx, model.Event{RunID: nextRun.ID, TaskID: nextRun.RootTaskID, Type: model.EventRunStatusChanged, Payload: map[string]any{"from": string(run.Status), "to": string(nextRun.Status), "run": eventpayload.Run(nextRun)}, RecordedAt: time.Now().UTC()}); err != nil {
		return CompleteAttemptResult{}, err
	}
	if err := uow.Events().AppendEvent(ctx, model.Event{RunID: task.RunID, TaskID: task.ID, Type: model.EventActionReconcileRequired, Payload: map[string]any{"attemptId": attempt.AttemptID, "status": string(attempt.Status), "task": eventpayload.Task(nextTask)}, RecordedAt: time.Now().UTC()}); err != nil {
		return CompleteAttemptResult{}, err
	}
	return CompleteAttemptResult{
		Attempt:       attempt,
		Task:          nextTask,
		Run:           nextRun,
		Lease:         lease,
		Reconcile:     true,
		RunTransition: run.Status != nextRun.Status,
	}, nil
}

func isTerminalAttemptStatus(status model.ActionAttemptStatus) bool {
	switch status {
	case model.ActionAttemptSucceeded,
		model.ActionAttemptFailed,
		model.ActionAttemptTimeout,
		model.ActionAttemptUnknown,
		model.ActionAttemptCancelled:
		return true
	default:
		return false
	}
}

func actorFromHolder(holderType model.HolderType, holderID string) model.SourceIdentity {
	switch holderType {
	case model.HolderAgent:
		return model.SourceIdentity{Type: model.SourceAgent, ID: holderID}
	case model.HolderComponent:
		return model.SourceIdentity{Type: model.SourceComponent, ID: holderID}
	default:
		return model.SourceIdentity{Type: model.SourceSystem, ID: holderID}
	}
}
