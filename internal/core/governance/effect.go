// Package governance provides pure policy-effect helpers that operate only
// through ports.FullUnitOfWork. Handlers in the composition root call these
// functions after receiving a policy decision; governance itself never mutates
// committed state directly.
package governance

import (
	"context"
	"errors"

	"github.com/Viking602/go-hydaelyn/internal/core/model"
	"github.com/Viking602/go-hydaelyn/internal/core/ports"
	corestate "github.com/Viking602/go-hydaelyn/internal/core/state"
)

// EffectTaskFromRequest loads the task identified by request if both RunID
// and TaskID are set. Returns (task, false, nil) when no task context exists.
func EffectTaskFromRequest(ctx context.Context, uow ports.FullUnitOfWork, request model.PolicyRequest) (model.Task, bool, error) {
	if request.RunID == "" || request.TaskID == "" {
		return model.Task{}, false, nil
	}
	task, err := uow.Tasks().LoadTask(ctx, request.RunID, request.TaskID)
	if errors.Is(err, model.ErrNotFound) {
		return model.Task{}, false, nil
	}
	if err != nil {
		return model.Task{}, false, err
	}
	return task, true, nil
}

// PauseTaskForPolicy transitions the task to Paused and releases its active
// lease. Idempotent: silently skips terminal or already-paused tasks.
func PauseTaskForPolicy(ctx context.Context, uow ports.FullUnitOfWork, task model.Task, reason string) error {
	if corestate.IsTerminalTask(task.Status) {
		return nil
	}
	paused, err := corestate.TransitionTask(task, model.TaskStatusPaused, true)
	if err != nil {
		if errors.Is(err, model.ErrInvalidTransition) || errors.Is(err, model.ErrTerminalState) {
			return nil
		}
		return err
	}
	if err := uow.Tasks().SaveTask(ctx, paused); err != nil {
		return err
	}
	if lease, ok, err := uow.Leases().ActiveLeaseForTask(ctx, paused.RunID, paused.ID); err != nil {
		return err
	} else if ok {
		lease.Status = model.LeaseStatusReleased
		if err := uow.Leases().SaveLease(ctx, lease); err != nil {
			return err
		}
	}
	return uow.Events().AppendEvent(ctx, model.Event{
		RunID:  paused.RunID,
		TaskID: paused.ID,
		Type:   model.EventTaskPaused,
		Payload: map[string]any{
			"reason": reason,
		},
	})
}

// TransitionRunForPolicy transitions the run to the given status. Idempotent:
// silently skips terminal runs and invalid transitions.
func TransitionRunForPolicy(ctx context.Context, uow ports.FullUnitOfWork, runID string, status model.RunStatus) error {
	run, err := uow.Runs().LoadRun(ctx, runID)
	if errors.Is(err, model.ErrNotFound) || corestate.IsTerminalRun(run.Status) {
		return nil
	}
	if err != nil {
		return err
	}
	next, err := corestate.TransitionRun(run, status)
	if err != nil {
		if errors.Is(err, model.ErrInvalidTransition) || errors.Is(err, model.ErrTerminalState) {
			return nil
		}
		return err
	}
	if err := uow.Runs().SaveRun(ctx, next); err != nil {
		return err
	}
	return uow.Events().AppendEvent(ctx, model.Event{
		RunID:  next.ID,
		TaskID: next.RootTaskID,
		Type:   model.EventRunStatusChanged,
		Payload: map[string]any{
			"from": string(run.Status),
			"to":   string(next.Status),
		},
	})
}
