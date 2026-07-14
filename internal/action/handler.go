package action

import (
	"context"
	"errors"
	"time"

	commandbus "github.com/Viking602/go-hydaelyn/internal/command"
	"github.com/Viking602/go-hydaelyn/internal/core/model"
	"github.com/Viking602/go-hydaelyn/internal/core/ports"
	corestate "github.com/Viking602/go-hydaelyn/internal/core/state"
	"github.com/Viking602/go-hydaelyn/internal/eventpayload"
	"github.com/Viking602/go-hydaelyn/internal/execution"
)

type IDGenerator func(string) string

type Authorizer func(context.Context, ports.UnitOfWork, model.PolicyRequest) (model.PolicyDecision, error)

type HandlerOptions struct {
	NewID     IDGenerator
	Authorize Authorizer
}

func RegisterHandlers(bus *commandbus.Bus, options HandlerOptions) {
	commandbus.Register[StartActionAttemptCommand](bus, startActionAttemptHandler{options: options})
	commandbus.Register[CompleteActionAttemptCommand](bus, completeActionAttemptHandler{})
}

type CompleteAttemptResult struct {
	Attempt       model.ActionAttempt
	Task          model.Task
	Run           model.Run
	Lease         model.TaskExecutionLease
	Reconcile     bool
	RunTransition bool
}

type startActionAttemptHandler struct{ options HandlerOptions }

func (startActionAttemptHandler) Name() string { return StartActionAttemptCommand{}.CommandName() }

func (h startActionAttemptHandler) Handle(ctx context.Context, uow ports.UnitOfWork, cmd StartActionAttemptCommand) (any, error) {
	_, task, _, err := execution.ValidateSubmission(ctx, uow, cmd.RunID, cmd.TaskID, cmd.LeaseID, cmd.HolderType, cmd.HolderID, cmd.TaskVersion)
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
	attempt := model.ActionAttempt{AttemptID: attemptID, ActionID: cmd.ActionID, RunID: cmd.RunID, TaskID: cmd.TaskID, ToolName: cmd.ToolName, Status: model.ActionAttemptRunning, IdempotencyKey: cmd.IdempotencyKey, InputHash: cmd.InputHash}
	if h.options.Authorize != nil {
		if _, err := h.options.Authorize(ctx, uow, model.PolicyRequest{Operation: model.PolicyOperationAction, RunID: cmd.RunID, TaskID: cmd.TaskID, Actor: actorFromHolder(cmd.HolderType, cmd.HolderID), Action: &attempt}); err != nil {
			return nil, err
		}
	}
	if err := uow.ActionAttempts().SaveActionAttempt(ctx, attempt); err != nil {
		return nil, err
	}
	if err := uow.Events().AppendEvent(ctx, model.Event{RunID: cmd.RunID, TaskID: cmd.TaskID, Type: model.EventActionAttemptStarted, Payload: map[string]any{"attemptId": attempt.AttemptID, "actionId": attempt.ActionID, "toolName": attempt.ToolName, "idempotencyKey": attempt.IdempotencyKey}, RecordedAt: time.Now().UTC()}); err != nil {
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
	if !isTerminalAttemptStatus(cmd.Status) {
		return nil, model.ErrInvalidCommand
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

func sameTerminalAttempt(attempt model.ActionAttempt, cmd CompleteActionAttemptCommand) bool {
	return attempt.Status == cmd.Status &&
		attempt.ExternalRequestID == cmd.ExternalRequestID &&
		attempt.ExternalResultRef == cmd.ExternalResultRef &&
		attempt.RequiresReconcile == (cmd.RequiresReconcile || cmd.Status == model.ActionAttemptUnknown)
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
