package execution

import (
	"context"
	"time"

	"github.com/Viking602/go-hydaelyn/internal/core/model"
	"github.com/Viking602/go-hydaelyn/internal/core/ports"
	corestate "github.com/Viking602/go-hydaelyn/internal/core/state"
)

// ValidateSubmission loads and validates run, task, and lease for a submission
// operation. It checks terminal state, task version staleness, lease ownership,
// and lease expiry.
func ValidateSubmission(ctx context.Context, uow ports.FullUnitOfWork, runID, taskID, leaseID string, holderType model.HolderType, holderID string, taskVersion int) (model.Run, model.Task, model.TaskExecutionLease, error) {
	run, err := uow.Runs().LoadRun(ctx, runID)
	if err != nil {
		return model.Run{}, model.Task{}, model.TaskExecutionLease{}, err
	}
	if corestate.IsTerminalRun(run.Status) {
		return model.Run{}, model.Task{}, model.TaskExecutionLease{}, model.ErrTerminalState
	}
	task, err := uow.Tasks().LoadTask(ctx, runID, taskID)
	if err != nil {
		return model.Run{}, model.Task{}, model.TaskExecutionLease{}, err
	}
	if corestate.IsTerminalTask(task.Status) {
		return model.Run{}, model.Task{}, model.TaskExecutionLease{}, model.ErrTerminalState
	}
	if taskVersion != task.Version {
		return model.Run{}, model.Task{}, model.TaskExecutionLease{}, model.ErrStaleTaskVersion
	}
	lease, err := uow.Leases().LoadLease(ctx, leaseID)
	if err != nil {
		return model.Run{}, model.Task{}, model.TaskExecutionLease{}, err
	}
	if lease.Status != model.LeaseStatusActive {
		return model.Run{}, model.Task{}, model.TaskExecutionLease{}, model.ErrLeaseNotActive
	}
	if lease.RunID != runID || lease.TaskID != taskID || lease.HolderType != holderType || lease.HolderID != holderID {
		return model.Run{}, model.Task{}, model.TaskExecutionLease{}, model.ErrLeaseHolderMismatch
	}
	if lease.ExpiresAt.Before(time.Now().UTC()) {
		lease.Status = model.LeaseStatusExpired
		_ = uow.Leases().SaveLease(ctx, lease)
		return model.Run{}, model.Task{}, model.TaskExecutionLease{}, model.ErrLeaseNotActive
	}
	return run, task, lease, nil
}
