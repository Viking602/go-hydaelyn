package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Viking602/go-hydaelyn"
	"github.com/Viking602/go-hydaelyn/agent"
	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/multiagent"
)

// TeamRunner connects multiagent.Drive to Runner-backed execution and
// checkpoints TeamState after every scheduler tick.
type TeamRunner struct {
	Runner    *hydaelyn.Runner
	Team      multiagent.Team
	BuildDeps agent.BuildDeps
	Options   multiagent.DriveOptions
	TTL       time.Duration
}

func (r TeamRunner) Start(ctx context.Context, runID string) (multiagent.DriveResult, error) {
	if err := r.validate(ctx); err != nil {
		return multiagent.DriveResult{}, err
	}
	if _, err := r.loadState(ctx, runID); err == nil {
		return multiagent.DriveResult{}, api.ErrIdempotencyConflict
	} else if !errors.Is(err, api.ErrNotFound) {
		return multiagent.DriveResult{}, err
	}
	lease, err := r.acquireScheduler(ctx, runID)
	if err != nil {
		return multiagent.DriveResult{}, err
	}
	if _, err := r.loadState(ctx, runID); err == nil {
		return multiagent.DriveResult{}, r.releaseSchedulerLease(ctx, lease, api.ErrIdempotencyConflict)
	} else if !errors.Is(err, api.ErrNotFound) {
		return multiagent.DriveResult{}, r.releaseSchedulerLease(ctx, lease, err)
	}
	state := multiagent.TeamState{RunID: runID}
	if err := r.saveState(ctx, state, false); err != nil {
		return multiagent.DriveResult{}, r.releaseSchedulerLease(ctx, lease, err)
	}
	return r.drive(ctx, state, lease)
}

func (r TeamRunner) Resume(ctx context.Context, runID string) (multiagent.DriveResult, error) {
	if err := r.validate(ctx); err != nil {
		return multiagent.DriveResult{}, err
	}
	if _, err := r.Runner.Recover(ctx, runID); err != nil {
		return multiagent.DriveResult{}, err
	}
	checkpoint, err := r.loadState(ctx, runID)
	if err != nil {
		return multiagent.DriveResult{}, err
	}
	run, err := r.Runner.Run(ctx, runID)
	if err != nil {
		return multiagent.DriveResult{}, err
	}
	switch run.Status {
	case api.RunStatusCompleted:
		if err := checkpointFailure(checkpoint); err != nil {
			return multiagent.DriveResult{State: checkpoint, Ticks: checkpoint.Tick}, err
		}
		return multiagent.DriveResult{State: checkpoint, Ticks: checkpoint.Tick}, nil
	case api.RunStatusComposingResponse:
		if err := checkpointFailure(checkpoint); err != nil {
			return multiagent.DriveResult{State: checkpoint, Ticks: checkpoint.Tick}, err
		}
		if err := r.Runner.TransitionRun(ctx, api.TransitionRunCommand{RunID: runID, To: api.RunStatusCompleted}); err != nil {
			return multiagent.DriveResult{State: checkpoint, Ticks: checkpoint.Tick}, err
		}
		return multiagent.DriveResult{State: checkpoint, Ticks: checkpoint.Tick}, nil
	case api.RunStatusReconcileRequired:
		return multiagent.DriveResult{State: checkpoint, Ticks: checkpoint.Tick}, api.ErrActionReconcileRequired
	case api.RunStatusRunning, api.RunStatusCreated:
	default:
		return multiagent.DriveResult{State: checkpoint, Ticks: checkpoint.Tick}, fmt.Errorf("worker: run %q status %q is not resumable: %w", runID, run.Status, api.ErrInvalidTransition)
	}
	lease, err := r.acquireScheduler(ctx, runID)
	if err != nil {
		return multiagent.DriveResult{}, err
	}
	return r.drive(ctx, checkpoint, lease)
}

func (r TeamRunner) drive(ctx context.Context, state multiagent.TeamState, lease api.TaskExecutionLease) (multiagent.DriveResult, error) {
	runCtx, cancel := context.WithCancel(ctx)
	heartbeatDone := make(chan error, 1)
	go func() {
		err := r.heartbeatScheduler(runCtx, lease)
		heartbeatDone <- err
		if err != nil {
			cancel()
		}
	}()
	opts := r.Options
	userCheckpoint := opts.AfterTick
	opts.InitialState = &state
	opts.AfterTick = func(ctx context.Context, current multiagent.TeamState) error {
		if err := r.saveState(ctx, current, true); err != nil {
			return err
		}
		if userCheckpoint != nil {
			return userCheckpoint(ctx, current)
		}
		return nil
	}
	classes := make(map[string]multiagent.AgentClass, len(r.Team.Agents))
	for _, class := range r.Team.Agents {
		classes[class.Name] = class
	}
	result, driveErr := multiagent.Drive(runCtx, state.RunID, r.Team.Scheduler, RunnerExecutor{
		Runner:    r.Runner,
		Classes:   classes,
		BuildDeps: r.BuildDeps,
		TTL:       r.TTL,
	}, opts)
	cancel()
	heartbeatErr := <-heartbeatDone
	if heartbeatErr != nil {
		driveErr = errors.Join(driveErr, heartbeatErr)
	}
	if driveErr == nil {
		driveErr = checkpointFailure(result.State)
	}
	finalizeErr := r.finishScheduler(context.WithoutCancel(ctx), lease, driveErr)
	return result, errors.Join(driveErr, finalizeErr)
}

func (r TeamRunner) releaseSchedulerLease(ctx context.Context, lease api.TaskExecutionLease, cause error) error {
	cleanupCtx := context.WithoutCancel(ctx)
	releaseErr := r.Runner.ReleaseTaskExecution(cleanupCtx, api.ReleaseTaskExecutionCommand{
		LeaseID:  lease.ID,
		HolderID: lease.HolderID,
	})
	return errors.Join(cause, releaseErr)
}

func checkpointFailure(state multiagent.TeamState) error {
	tasks := make(map[string]api.Task, len(state.Tasks))
	for _, task := range state.Tasks {
		tasks[task.ID] = task
	}
	for _, instance := range state.Instances {
		if instance.State != multiagent.InstanceStateFailed {
			continue
		}
		reason := tasks[instance.TaskID].Error
		if reason == "" {
			reason = "agent execution failed"
		}
		return fmt.Errorf("%w %q: %s", ErrFailedCheckpoint, instance.ID, reason)
	}
	return nil
}

func (r TeamRunner) acquireScheduler(ctx context.Context, runID string) (api.TaskExecutionLease, error) {
	run, err := r.Runner.Run(ctx, runID)
	if err != nil {
		return api.TaskExecutionLease{}, err
	}
	if run.Status == api.RunStatusCreated {
		run, err = r.Runner.AdvanceRun(ctx, api.AdvanceRunCommand{RunID: runID})
		if err != nil {
			return api.TaskExecutionLease{}, err
		}
	}
	task, err := r.Runner.Task(ctx, runID, run.RootTaskID)
	if err != nil {
		return api.TaskExecutionLease{}, err
	}
	envelope, ok, err := taskEnvelope(ctx, r.Runner, runID, task.ID, "pending", "delivered")
	if err != nil {
		return api.TaskExecutionLease{}, err
	}
	if !ok {
		envelope, err = r.Runner.DispatchTask(ctx, api.DispatchTaskCommand{
			RunID:           runID,
			TaskID:          task.ID,
			TargetAgentID:   task.OwnerAgentID,
			TargetComponent: task.OwnerComponent,
		})
		if err != nil {
			return api.TaskExecutionLease{}, err
		}
	}
	holderType := api.HolderComponent
	holderID := envelope.TargetComponent
	if envelope.TargetAgentID != "" {
		holderType = api.HolderAgent
		holderID = envelope.TargetAgentID
	}
	if holderID == "" {
		return api.TaskExecutionLease{}, api.ErrInvalidConfiguration
	}
	lease, acquired, err := r.Runner.AcquireTaskExecution(ctx, api.AcquireTaskExecutionCommand{
		RunID:      runID,
		TaskID:     task.ID,
		EnvelopeID: envelope.ID,
		HolderType: holderType,
		HolderID:   holderID,
		TTL:        r.schedulerTTL(),
	})
	if err != nil {
		return api.TaskExecutionLease{}, err
	}
	if !acquired {
		return api.TaskExecutionLease{}, api.ErrLeaseNotActive
	}
	return lease, nil
}

func (r TeamRunner) heartbeatScheduler(ctx context.Context, lease api.TaskExecutionLease) error {
	ttl := r.schedulerTTL()
	ticker := time.NewTicker(ttl / 3)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := r.Runner.HeartbeatTaskExecution(ctx, api.HeartbeatTaskExecutionCommand{
				LeaseID:  lease.ID,
				HolderID: lease.HolderID,
				TTL:      ttl,
			}); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
		}
	}
}

func (r TeamRunner) finishScheduler(ctx context.Context, lease api.TaskExecutionLease, driveErr error) error {
	report := api.TypedReport{Status: api.ReportStatusSuccess, Summary: "team scheduler completed"}
	if driveErr != nil {
		report.Status = api.ReportStatusFailed
		report.Summary = driveErr.Error()
	}
	if err := r.Runner.SubmitTypedReport(ctx, api.SubmitTypedReportCommand{
		RunID:       lease.RunID,
		TaskID:      lease.TaskID,
		LeaseID:     lease.ID,
		HolderType:  lease.HolderType,
		HolderID:    lease.HolderID,
		TaskVersion: lease.TaskVersion,
		Report:      report,
	}); err != nil {
		return err
	}
	if driveErr != nil {
		var schedulerErr *multiagent.SchedulerFailureError
		if errors.As(driveErr, &schedulerErr) {
			if err := r.appendSchedulerFailure(ctx, *schedulerErr); err != nil {
				return err
			}
		}
	}
	if driveErr != nil {
		return r.Runner.TransitionRun(ctx, api.TransitionRunCommand{RunID: lease.RunID, To: api.RunStatusFailed})
	}
	if err := r.Runner.TransitionRun(ctx, api.TransitionRunCommand{RunID: lease.RunID, To: api.RunStatusComposingResponse}); err != nil {
		return err
	}
	return r.Runner.TransitionRun(ctx, api.TransitionRunCommand{RunID: lease.RunID, To: api.RunStatusCompleted})
}

func (r TeamRunner) appendSchedulerFailure(ctx context.Context, schedulerErr multiagent.SchedulerFailureError) error {
	uow, err := r.Runner.Begin(ctx)
	if err != nil {
		return err
	}
	if err := uow.Events().AppendEvent(ctx, api.Event{
		RunID:      schedulerErr.RunID,
		Type:       multiagent.EventSchedulerFailure,
		Payload:    map[string]any{"tick": schedulerErr.Tick, "error": schedulerErr.Error()},
		RecordedAt: time.Now().UTC(),
	}); err != nil {
		_ = uow.Rollback(ctx)
		return err
	}
	return uow.Commit(ctx)
}

func (r TeamRunner) schedulerTTL() time.Duration {
	if r.TTL > 0 {
		return r.TTL
	}
	return 30 * time.Second
}

func (r TeamRunner) loadState(ctx context.Context, runID string) (multiagent.TeamState, error) {
	uow, err := r.Runner.Begin(ctx)
	if err != nil {
		return multiagent.TeamState{}, err
	}
	defer func() { _ = uow.Rollback(ctx) }()
	record, err := uow.TeamStates().LoadTeamState(ctx, runID)
	if err != nil {
		return multiagent.TeamState{}, err
	}
	var state multiagent.TeamState
	if err := json.Unmarshal(record.State, &state); err != nil {
		return multiagent.TeamState{}, fmt.Errorf("worker: decode team state: %w", err)
	}
	if state.RunID == "" {
		state.RunID = runID
	}
	if state.RunID != runID {
		return multiagent.TeamState{}, api.ErrInvalidConfiguration
	}
	if state.Tick < record.Tick {
		state.Tick = record.Tick
	}
	return state, nil
}

func (r TeamRunner) saveState(ctx context.Context, state multiagent.TeamState, emitTick bool) error {
	raw, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("worker: encode team state: %w", err)
	}
	uow, err := r.Runner.Begin(ctx)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = uow.Rollback(ctx)
		}
	}()
	if err := uow.TeamStates().SaveTeamState(ctx, api.TeamStateRecord{
		RunID:     state.RunID,
		Tick:      state.Tick,
		State:     raw,
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		return err
	}
	if emitTick {
		if err := uow.Events().AppendEvent(ctx, api.Event{
			RunID:      state.RunID,
			Type:       multiagent.EventSchedulerTick,
			Payload:    map[string]any{"tick": state.Tick},
			RecordedAt: time.Now().UTC(),
		}); err != nil {
			return err
		}
	}
	if err := uow.Commit(ctx); err != nil {
		return err
	}
	committed = true
	return nil
}

func (r TeamRunner) validate(ctx context.Context) error {
	if r.Runner == nil {
		return ErrRunnerMissing
	}
	if r.Team.Scheduler == nil {
		return fmt.Errorf("worker: team scheduler missing: %w", api.ErrInvalidConfiguration)
	}
	capabilities, err := r.Runner.StoreCapabilities(ctx)
	if err != nil {
		return err
	}
	if !capabilities.SupportsTransactions {
		return fmt.Errorf("worker: durable team runner requires transactional storage: %w", api.ErrInvalidConfiguration)
	}
	return nil
}
