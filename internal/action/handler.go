package action

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/Viking602/venat/api"
	commandbus "github.com/Viking602/venat/internal/command"
	"github.com/Viking602/venat/internal/core/ports"
	corestate "github.com/Viking602/venat/internal/core/state"
	"github.com/Viking602/venat/internal/eventpayload"
	"github.com/Viking602/venat/internal/execution"
	"github.com/Viking602/venat/message"
)

const maxActionAttemptResultBytes = 8 << 20

type IDGenerator func(string) string

type Authorizer func(context.Context, ports.UnitOfWork, api.PolicyRequest) (api.PolicyDecision, error)

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
	Attempt       api.ActionAttempt
	Task          api.Task
	Run           api.Run
	Lease         api.TaskExecutionLease
	Reconcile     bool
	RunTransition bool
}

type ResolveAttemptResult struct {
	Attempt        api.ActionAttempt
	Task           api.Task
	Run            api.Run
	Envelope       api.TaskEnvelope
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
		return nil, api.ErrActionTaskRequired
	}
	if cmd.IdempotencyKey != "" {
		existing, err := uow.ActionAttempts().LoadActionAttemptByIdempotencyKey(ctx, cmd.RunID, cmd.TaskID, cmd.ToolName, cmd.IdempotencyKey)
		if err == nil {
			if existing.InputHash != cmd.InputHash {
				return nil, api.ErrIdempotencyConflict
			}
			if err := h.authorize(ctx, uow, cmd, existing); err != nil {
				return nil, err
			}
			if existing.Status == api.ActionAttemptRunning && existing.LeaseID != cmd.LeaseID {
				existing.Status = api.ActionAttemptUnknown
				existing.RequiresReconcile = true
				if err := uow.ActionAttempts().SaveActionAttempt(ctx, existing); err != nil {
					return nil, err
				}
				if err := uow.Events().AppendEvent(ctx, api.Event{
					RunID: cmd.RunID, TaskID: cmd.TaskID, Type: api.EventActionAttemptUpdated,
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
		if !errors.Is(err, api.ErrNotFound) {
			return nil, err
		}
	}
	attemptID := cmd.AttemptID
	if attemptID == "" {
		attemptID = h.options.NewID("attempt")
	}
	attempt := api.ActionAttempt{AttemptID: attemptID, ActionID: cmd.ActionID, RunID: cmd.RunID, TaskID: cmd.TaskID, LeaseID: cmd.LeaseID, ToolName: cmd.ToolName, Status: api.ActionAttemptRunning, IdempotencyKey: cmd.IdempotencyKey, InputHash: cmd.InputHash}
	if err := h.authorize(ctx, uow, cmd, attempt); err != nil {
		return nil, err
	}
	if err := uow.ActionAttempts().SaveActionAttempt(ctx, attempt); err != nil {
		return nil, err
	}
	if err := uow.Events().AppendEvent(ctx, api.Event{RunID: cmd.RunID, TaskID: cmd.TaskID, Type: api.EventActionAttemptStarted, Payload: map[string]any{"attemptId": attempt.AttemptID, "actionId": attempt.ActionID, "leaseId": attempt.LeaseID, "toolName": attempt.ToolName, "idempotencyKey": attempt.IdempotencyKey}, RecordedAt: time.Now().UTC()}); err != nil {
		return nil, err
	}
	return attempt, nil
}

func (h startActionAttemptHandler) authorize(
	ctx context.Context,
	uow ports.UnitOfWork,
	cmd StartActionAttemptCommand,
	attempt api.ActionAttempt,
) error {
	if h.options.Authorize == nil {
		return nil
	}
	_, err := h.options.Authorize(ctx, uow, api.PolicyRequest{
		Operation: api.PolicyOperationAction,
		RunID:     cmd.RunID,
		TaskID:    cmd.TaskID,
		Actor:     actorFromHolder(cmd.HolderType, cmd.HolderID),
		Action:    &attempt,
	})
	return err
}

type completeActionAttemptHandler struct{}

func (completeActionAttemptHandler) Name() string {
	return CompleteActionAttemptCommand{}.CommandName()
}

func (completeActionAttemptHandler) Handle(ctx context.Context, uow ports.UnitOfWork, cmd CompleteActionAttemptCommand) (any, error) {
	if cmd.LeaseID == "" {
		return nil, api.ErrLeaseNotActive
	}
	run, task, lease, err := execution.ValidateSubmission(ctx, uow, cmd.RunID, cmd.TaskID, cmd.LeaseID, cmd.HolderType, cmd.HolderID, cmd.TaskVersion)
	if err != nil {
		return nil, err
	}
	if !task.AllowsAction {
		return nil, api.ErrActionTaskRequired
	}
	attempt, err := uow.ActionAttempts().LoadActionAttempt(ctx, cmd.AttemptID)
	if err != nil {
		return nil, err
	}
	if attempt.RunID != cmd.RunID || attempt.TaskID != cmd.TaskID {
		return nil, api.ErrNotFound
	}
	if attempt.LeaseID != cmd.LeaseID {
		return nil, api.ErrLeaseNotActive
	}
	if !isTerminalAttemptStatus(cmd.Status) {
		return nil, api.ErrInvalidCommand
	}
	if err := validateActionAttemptResult(cmd.ExternalResultRef, cmd.ToolResult); err != nil {
		return nil, err
	}
	if isTerminalAttemptStatus(attempt.Status) {
		if sameTerminalAttempt(attempt, cmd) {
			return CompleteAttemptResult{Attempt: attempt}, nil
		}
		return nil, api.ErrIdempotencyConflict
	}
	attempt.Status = cmd.Status
	attempt.ExternalRequestID = cmd.ExternalRequestID
	attempt.ExternalResultRef = cmd.ExternalResultRef
	attempt.ToolResult = append(attempt.ToolResult[:0], cmd.ToolResult...)
	attempt.RequiresReconcile = cmd.RequiresReconcile || cmd.Status == api.ActionAttemptUnknown
	if cmd.UsageRecord != nil {
		if cmd.UsageRecord.RunID != cmd.RunID || cmd.UsageRecord.TaskID != cmd.TaskID {
			return nil, api.ErrInvalidCommand
		}
		if err := uow.UsageRecords().AppendUsage(ctx, *cmd.UsageRecord); err != nil {
			return nil, err
		}
	}
	if err := uow.ActionAttempts().SaveActionAttempt(ctx, attempt); err != nil {
		return nil, err
	}
	if err := uow.Events().AppendEvent(ctx, api.Event{RunID: cmd.RunID, TaskID: cmd.TaskID, Type: api.EventActionAttemptUpdated, Payload: map[string]any{"attemptId": attempt.AttemptID, "status": string(attempt.Status), "externalRequestId": attempt.ExternalRequestID, "externalResultRef": attempt.ExternalResultRef, "requiresReconcile": attempt.RequiresReconcile}, RecordedAt: time.Now().UTC()}); err != nil {
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
		return nil, api.ErrInvalidCommand
	}
	if err := validateActionAttemptResult(cmd.ExternalResultRef, cmd.ToolResult); err != nil {
		return nil, err
	}
	attempt, err := uow.ActionAttempts().LoadActionAttempt(ctx, cmd.AttemptID)
	if err != nil {
		return nil, err
	}
	if !attempt.RequiresReconcile || attempt.Status != api.ActionAttemptUnknown {
		if sameResolvedAttempt(attempt, cmd) {
			return ResolveAttemptResult{Attempt: attempt}, nil
		}
		return nil, api.ErrIdempotencyConflict
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
		return nil, api.ErrIdempotencyConflict
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
	task api.Task,
	run api.Run,
	resolved api.ActionAttempt,
	newID IDGenerator,
) (ResolveAttemptResult, error) {
	result := ResolveAttemptResult{Attempt: resolved, Task: task, Run: run}
	requiresReconcile := true
	pendingAttempts, err := uow.ActionAttempts().ListActionAttempts(ctx, api.ActionAttemptSelector{
		RunID:             resolved.RunID,
		Statuses:          []api.ActionAttemptStatus{api.ActionAttemptUnknown},
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
	if task.Status == api.TaskStatusReconcileRequired && !taskStillBlocked {
		nextTask, err := corestate.TransitionTask(task, api.TaskStatusDispatched, false)
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
		envelope := api.TaskEnvelope{
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
		if candidate.Status == api.TaskStatusReconcileRequired {
			runStillBlocked = true
			break
		}
	}
	if run.Status == api.RunStatusReconcileRequired && !runStillBlocked {
		nextRun, err := corestate.TransitionRun(run, api.RunStatusRunning)
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
	attempt api.ActionAttempt,
	previousRun api.Run,
	result ResolveAttemptResult,
) error {
	now := time.Now().UTC()
	if err := uow.Events().AppendEvent(ctx, api.Event{
		RunID:  attempt.RunID,
		TaskID: attempt.TaskID,
		Type:   api.EventActionAttemptUpdated,
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
		if err := uow.Events().AppendEvent(ctx, api.Event{
			RunID:      result.Task.RunID,
			TaskID:     result.Task.ID,
			Type:       api.EventTaskDispatched,
			Payload:    map[string]any{"reason": "action_reconciled", "task": eventpayload.Task(result.Task), "envelope": eventpayload.Envelope(result.Envelope)},
			RecordedAt: now,
		}); err != nil {
			return err
		}
	}
	if result.RunTransition {
		if err := uow.Events().AppendEvent(ctx, api.Event{
			RunID:      result.Run.ID,
			TaskID:     result.Run.RootTaskID,
			Type:       api.EventRunStatusChanged,
			Payload:    map[string]any{"from": string(previousRun.Status), "to": string(result.Run.Status), "run": eventpayload.Run(result.Run)},
			RecordedAt: now,
		}); err != nil {
			return err
		}
	}
	return nil
}

func sameResolvedAttempt(attempt api.ActionAttempt, cmd ResolveActionAttemptCommand) bool {
	return !attempt.RequiresReconcile &&
		attempt.Status == cmd.Status &&
		attempt.ExternalResultRef == cmd.ExternalResultRef &&
		bytes.Equal(attempt.ToolResult, cmd.ToolResult)
}

func isResolutionAttemptStatus(status api.ActionAttemptStatus) bool {
	switch status {
	case api.ActionAttemptSucceeded, api.ActionAttemptFailed, api.ActionAttemptTimeout, api.ActionAttemptCancelled:
		return true
	default:
		return false
	}
}

func sameTerminalAttempt(attempt api.ActionAttempt, cmd CompleteActionAttemptCommand) bool {
	return attempt.Status == cmd.Status &&
		attempt.ExternalRequestID == cmd.ExternalRequestID &&
		attempt.ExternalResultRef == cmd.ExternalResultRef &&
		bytes.Equal(attempt.ToolResult, cmd.ToolResult) &&
		attempt.RequiresReconcile == (cmd.RequiresReconcile || cmd.Status == api.ActionAttemptUnknown)
}

func validateActionAttemptResult(externalResultRef string, toolResult json.RawMessage) error {
	if len(externalResultRef) > maxActionAttemptResultBytes || len(toolResult) > maxActionAttemptResultBytes {
		return fmt.Errorf(
			"action: result exceeds %d bytes: %w",
			maxActionAttemptResultBytes,
			api.ErrInvalidCommand,
		)
	}
	if len(toolResult) == 0 {
		return nil
	}
	var result message.ToolResult
	if err := json.Unmarshal(toolResult, &result); err != nil {
		return fmt.Errorf("action: invalid tool result: %w: %v", api.ErrInvalidCommand, err)
	}
	return nil
}

func reconcileAttempt(ctx context.Context, uow ports.UnitOfWork, run api.Run, task api.Task, lease api.TaskExecutionLease, attempt api.ActionAttempt) (CompleteAttemptResult, error) {
	nextTask, err := corestate.TransitionTask(task, api.TaskStatusReconcileRequired, true)
	if err != nil {
		return CompleteAttemptResult{}, err
	}
	nextTask.Error = "action attempt requires reconciliation"
	if err := uow.Tasks().SaveTask(ctx, nextTask); err != nil {
		return CompleteAttemptResult{}, err
	}
	lease.Status = api.LeaseStatusReleased
	if err := uow.Leases().SaveLease(ctx, lease); err != nil {
		return CompleteAttemptResult{}, err
	}
	if err := execution.ReleaseResourceClaims(ctx, uow, lease.ID, time.Now().UTC()); err != nil {
		return CompleteAttemptResult{}, err
	}
	if err := uow.Events().AppendEvent(ctx, api.Event{RunID: lease.RunID, TaskID: lease.TaskID, Type: api.EventTaskExecutionReleased, Payload: map[string]any{"leaseId": lease.ID}, RecordedAt: time.Now().UTC()}); err != nil {
		return CompleteAttemptResult{}, err
	}
	nextRun, err := corestate.TransitionRun(run, api.RunStatusReconcileRequired)
	if err != nil {
		return CompleteAttemptResult{}, err
	}
	if err := uow.Runs().SaveRun(ctx, nextRun); err != nil {
		return CompleteAttemptResult{}, err
	}
	if err := uow.Events().AppendEvent(ctx, api.Event{RunID: nextRun.ID, TaskID: nextRun.RootTaskID, Type: api.EventRunStatusChanged, Payload: map[string]any{"from": string(run.Status), "to": string(nextRun.Status), "run": eventpayload.Run(nextRun)}, RecordedAt: time.Now().UTC()}); err != nil {
		return CompleteAttemptResult{}, err
	}
	if err := uow.Events().AppendEvent(ctx, api.Event{RunID: task.RunID, TaskID: task.ID, Type: api.EventActionReconcileRequired, Payload: map[string]any{"attemptId": attempt.AttemptID, "status": string(attempt.Status), "task": eventpayload.Task(nextTask)}, RecordedAt: time.Now().UTC()}); err != nil {
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

func isTerminalAttemptStatus(status api.ActionAttemptStatus) bool {
	switch status {
	case api.ActionAttemptSucceeded,
		api.ActionAttemptFailed,
		api.ActionAttemptTimeout,
		api.ActionAttemptUnknown,
		api.ActionAttemptCancelled:
		return true
	default:
		return false
	}
}

func actorFromHolder(holderType api.HolderType, holderID string) api.SourceIdentity {
	switch holderType {
	case api.HolderAgent:
		return api.SourceIdentity{Type: api.SourceAgent, ID: holderID}
	case api.HolderComponent:
		return api.SourceIdentity{Type: api.SourceComponent, ID: holderID}
	default:
		return api.SourceIdentity{Type: api.SourceSystem, ID: holderID}
	}
}
