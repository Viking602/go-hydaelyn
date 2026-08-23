// Package governance provides pure policy-effect helpers that operate only
// through ports.UnitOfWork. Handlers in the composition root call these
// functions after receiving a policy decision; governance itself never mutates
// committed state directly.
package governance

import (
	"context"
	"errors"
	"time"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/internal/core/ports"
	corestate "github.com/Viking602/venat/internal/core/state"
	"github.com/Viking602/venat/internal/execution"
)

// EffectTaskFromRequest loads the task identified by request if both RunID
// and TaskID are set. Returns (task, false, nil) when no task context exists.
func EffectTaskFromRequest(ctx context.Context, uow ports.UnitOfWork, request api.PolicyRequest) (api.Task, bool, error) {
	if request.RunID == "" || request.TaskID == "" {
		return api.Task{}, false, nil
	}
	task, err := uow.Tasks().LoadTask(ctx, request.RunID, request.TaskID)
	if errors.Is(err, api.ErrNotFound) {
		return api.Task{}, false, nil
	}
	if err != nil {
		return api.Task{}, false, err
	}
	return task, true, nil
}

// PauseTaskForPolicy transitions the task to Paused and releases its active
// lease. Idempotent: silently skips terminal or already-paused tasks.
func PauseTaskForPolicy(ctx context.Context, uow ports.UnitOfWork, task api.Task, reason string) error {
	if corestate.IsTerminalTask(task.Status) {
		return nil
	}
	paused, err := corestate.TransitionTask(task, api.TaskStatusPaused, true)
	if err != nil {
		if errors.Is(err, api.ErrInvalidTransition) || errors.Is(err, api.ErrTerminalState) {
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
		lease.Status = api.LeaseStatusReleased
		if err := uow.Leases().SaveLease(ctx, lease); err != nil {
			return err
		}
		if err := execution.ReleaseResourceClaims(ctx, uow, lease.ID, time.Now().UTC()); err != nil {
			return err
		}
	}
	return uow.Events().AppendEvent(ctx, api.Event{
		RunID:  paused.RunID,
		TaskID: paused.ID,
		Type:   api.EventTaskPaused,
		Payload: map[string]any{
			"reason": reason,
		},
	})
}

// TransitionRunForPolicy transitions the run to the given status. Idempotent:
// silently skips terminal runs and invalid transitions.
func TransitionRunForPolicy(ctx context.Context, uow ports.UnitOfWork, runID string, status api.RunStatus) error {
	run, err := uow.Runs().LoadRun(ctx, runID)
	if errors.Is(err, api.ErrNotFound) || corestate.IsTerminalRun(run.Status) {
		return nil
	}
	if err != nil {
		return err
	}
	next, err := corestate.TransitionRun(run, status)
	if err != nil {
		if errors.Is(err, api.ErrInvalidTransition) || errors.Is(err, api.ErrTerminalState) {
			return nil
		}
		return err
	}
	if err := uow.Runs().SaveRun(ctx, next); err != nil {
		return err
	}
	return uow.Events().AppendEvent(ctx, api.Event{
		RunID:  next.ID,
		TaskID: next.RootTaskID,
		Type:   api.EventRunStatusChanged,
		Payload: map[string]any{
			"from": string(run.Status),
			"to":   string(next.Status),
		},
	})
}
