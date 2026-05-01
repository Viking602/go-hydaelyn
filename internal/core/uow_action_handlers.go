package core

import (
	"context"
	"time"

	commandbus "github.com/Viking602/go-hydaelyn/internal/core/command"
	"github.com/Viking602/go-hydaelyn/internal/core/ports"
)

func registerActionUoWCommandHandlers(runtime *Runtime) {
	commandbus.Register[StartActionAttemptCommand](runtime.commandBus, startActionAttemptHandler{runtime: runtime})
	commandbus.Register[CompleteActionAttemptCommand](runtime.commandBus, completeActionAttemptHandler{})
}

type completeActionAttemptResult struct {
	Attempt       ActionAttempt
	Task          Task
	Run           Run
	Lease         TaskExecutionLease
	Reconcile     bool
	RunTransition bool
}

type startActionAttemptHandler struct{ runtime *Runtime }

func (startActionAttemptHandler) Name() string { return StartActionAttemptCommand{}.CommandName() }

func (h startActionAttemptHandler) Handle(ctx context.Context, uow ports.FullUnitOfWork, cmd StartActionAttemptCommand) (any, error) {
	_, task, _, err := validateSubmissionUoW(ctx, uow, cmd.RunID, cmd.TaskID, cmd.LeaseID, cmd.HolderType, cmd.HolderID, cmd.TaskVersion)
	if err != nil {
		return nil, err
	}
	if !task.AllowsAction {
		return nil, ErrActionTaskRequired
	}
	attemptID := cmd.AttemptID
	if attemptID == "" {
		attemptID = h.runtime.newID("attempt")
	}
	attempt := ActionAttempt{AttemptID: attemptID, ActionID: cmd.ActionID, RunID: cmd.RunID, TaskID: cmd.TaskID, ToolName: cmd.ToolName, Status: ActionAttemptRunning, IdempotencyKey: cmd.IdempotencyKey, InputHash: cmd.InputHash}
	if _, err := h.runtime.authorizeUoW(ctx, uow, PolicyRequest{Operation: PolicyOperationAction, RunID: cmd.RunID, TaskID: cmd.TaskID, Actor: actorFromHolder(cmd.HolderType, cmd.HolderID), Action: &attempt}); err != nil {
		return nil, err
	}
	if err := uow.ActionAttempts().SaveActionAttempt(ctx, attempt); err != nil {
		return nil, err
	}
	if err := uow.Events().AppendEvent(ctx, Event{RunID: cmd.RunID, TaskID: cmd.TaskID, Type: EventActionAttemptStarted, Payload: map[string]any{"attemptId": attempt.AttemptID, "actionId": attempt.ActionID, "toolName": attempt.ToolName, "idempotencyKey": attempt.IdempotencyKey}, RecordedAt: time.Now().UTC()}); err != nil {
		return nil, err
	}
	return attempt, nil
}

type completeActionAttemptHandler struct{}

func (completeActionAttemptHandler) Name() string {
	return CompleteActionAttemptCommand{}.CommandName()
}

func (completeActionAttemptHandler) Handle(ctx context.Context, uow ports.FullUnitOfWork, cmd CompleteActionAttemptCommand) (any, error) {
	if cmd.LeaseID == "" {
		return nil, ErrLeaseNotActive
	}
	run, task, lease, err := validateSubmissionUoW(ctx, uow, cmd.RunID, cmd.TaskID, cmd.LeaseID, cmd.HolderType, cmd.HolderID, cmd.TaskVersion)
	if err != nil {
		return nil, err
	}
	if !task.AllowsAction {
		return nil, ErrActionTaskRequired
	}
	attempt, err := uow.ActionAttempts().LoadActionAttempt(ctx, cmd.AttemptID)
	if err != nil {
		return nil, err
	}
	if attempt.RunID != cmd.RunID || attempt.TaskID != cmd.TaskID {
		return nil, ErrNotFound
	}
	attempt.Status = cmd.Status
	attempt.ExternalRequestID = cmd.ExternalRequestID
	attempt.ExternalResultRef = cmd.ExternalResultRef
	attempt.RequiresReconcile = cmd.RequiresReconcile || cmd.Status == ActionAttemptUnknown
	if err := uow.ActionAttempts().SaveActionAttempt(ctx, attempt); err != nil {
		return nil, err
	}
	if err := uow.Events().AppendEvent(ctx, Event{RunID: cmd.RunID, TaskID: cmd.TaskID, Type: EventActionAttemptUpdated, Payload: map[string]any{"attemptId": attempt.AttemptID, "status": string(attempt.Status), "externalRequestId": attempt.ExternalRequestID, "externalResultRef": attempt.ExternalResultRef, "requiresReconcile": attempt.RequiresReconcile}, RecordedAt: time.Now().UTC()}); err != nil {
		return nil, err
	}
	result := completeActionAttemptResult{Attempt: attempt}
	if attempt.RequiresReconcile {
		nextTask, err := transitionTaskPure(task, TaskStatusReconcileRequired, true)
		if err != nil {
			return nil, err
		}
		nextTask.Error = "action attempt requires reconciliation"
		if err := uow.Tasks().SaveTask(ctx, nextTask); err != nil {
			return nil, err
		}
		lease.Status = LeaseStatusReleased
		if err := uow.Leases().SaveLease(ctx, lease); err != nil {
			return nil, err
		}
		if err := uow.Events().AppendEvent(ctx, Event{RunID: lease.RunID, TaskID: lease.TaskID, Type: EventTaskExecutionReleased, Payload: map[string]any{"leaseId": lease.ID}, RecordedAt: time.Now().UTC()}); err != nil {
			return nil, err
		}
		nextRun, err := transitionRunPure(run, RunStatusReconcileRequired)
		if err != nil {
			return nil, err
		}
		if err := uow.Runs().SaveRun(ctx, nextRun); err != nil {
			return nil, err
		}
		if err := uow.Events().AppendEvent(ctx, Event{RunID: nextRun.ID, TaskID: nextRun.RootTaskID, Type: EventRunStatusChanged, Payload: map[string]any{"from": string(run.Status), "to": string(nextRun.Status), "run": runPayload(nextRun)}, RecordedAt: time.Now().UTC()}); err != nil {
			return nil, err
		}
		if err := uow.Events().AppendEvent(ctx, Event{RunID: cmd.RunID, TaskID: cmd.TaskID, Type: EventActionReconcileRequired, Payload: map[string]any{"attemptId": attempt.AttemptID, "status": string(attempt.Status)}, RecordedAt: time.Now().UTC()}); err != nil {
			return nil, err
		}
		result.Task = nextTask
		result.Run = nextRun
		result.Lease = lease
		result.Reconcile = true
		result.RunTransition = run.Status != nextRun.Status
	}
	return result, nil
}
