package execution

import (
	"context"
	"time"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/internal/core/ports"
	corestate "github.com/Viking602/venat/internal/core/state"
)

// ValidateSubmission loads and validates run, task, and lease for a submission
// operation. It checks terminal state, task version staleness, lease ownership,
// and lease expiry.
func ValidateSubmission(ctx context.Context, uow ports.UnitOfWork, runID, taskID, leaseID string, holderType api.HolderType, holderID string, taskVersion int) (api.Run, api.Task, api.TaskExecutionLease, error) {
	run, err := uow.Runs().LoadRun(ctx, runID)
	if err != nil {
		return api.Run{}, api.Task{}, api.TaskExecutionLease{}, err
	}
	if corestate.IsTerminalRun(run.Status) {
		return api.Run{}, api.Task{}, api.TaskExecutionLease{}, api.ErrTerminalState
	}
	task, err := uow.Tasks().LoadTask(ctx, runID, taskID)
	if err != nil {
		return api.Run{}, api.Task{}, api.TaskExecutionLease{}, err
	}
	if corestate.IsTerminalTask(task.Status) {
		return api.Run{}, api.Task{}, api.TaskExecutionLease{}, api.ErrTerminalState
	}
	if taskVersion != task.Version {
		return api.Run{}, api.Task{}, api.TaskExecutionLease{}, api.ErrStaleTaskVersion
	}
	lease, err := uow.Leases().LoadLease(ctx, leaseID)
	if err != nil {
		return api.Run{}, api.Task{}, api.TaskExecutionLease{}, err
	}
	if lease.Status != api.LeaseStatusActive {
		return api.Run{}, api.Task{}, api.TaskExecutionLease{}, api.ErrLeaseNotActive
	}
	if lease.RunID != runID || lease.TaskID != taskID || lease.HolderType != holderType || lease.HolderID != holderID {
		return api.Run{}, api.Task{}, api.TaskExecutionLease{}, api.ErrLeaseHolderMismatch
	}
	expiry := api.LeaseExpiry(lease)
	if expiry.IsZero() || !expiry.After(time.Now().UTC()) {
		return api.Run{}, api.Task{}, api.TaskExecutionLease{}, api.ErrLeaseNotActive
	}
	return run, task, lease, nil
}
