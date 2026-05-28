package multiagent

import (
	"context"
	"errors"
	"time"

	"github.com/Viking602/go-hydaelyn/api"
)

// Executor runs one Dispatch and returns its TypedReport. Implementations
// typically run an agent.Engine against Dispatch.Task; the durable runner
// integration supplies an Executor that persists each step through the
// Runner (task, lease, report, events). Keeping execution behind this
// interface lets Drive stay within the multiagent boundary
// (api/agent/stdlib only) — the Runner dependency lives in the Executor.
type Executor interface {
	Execute(ctx context.Context, dispatch Dispatch) (api.TypedReport, error)
}

// ExecutorFunc adapts a plain function to an Executor.
type ExecutorFunc func(ctx context.Context, dispatch Dispatch) (api.TypedReport, error)

// Execute implements Executor.
func (f ExecutorFunc) Execute(ctx context.Context, dispatch Dispatch) (api.TypedReport, error) {
	return f(ctx, dispatch)
}

// DriveOptions bounds a Drive run.
type DriveOptions struct {
	// MaxTicks caps scheduler iterations to guard against a Scheduler that
	// never reaches a terminal state. Defaults to 64.
	MaxTicks int
}

// DriveResult is the terminal snapshot after the scheduler loop ends.
type DriveResult struct {
	State TeamState
	Ticks int
}

// ErrMaxTicksExceeded is returned when a Scheduler keeps emitting
// dispatches past DriveOptions.MaxTicks.
var ErrMaxTicksExceeded = errors.New("multiagent: scheduler exceeded max ticks")

const defaultMaxTicks = 64

// Drive runs scheduler to a terminal state (Next returns no dispatches),
// executing each Dispatch with executor and folding the result back into
// TeamState for the next tick. It is the in-process scheduler loop that
// makes the reference Schedulers runnable; it makes no durability
// guarantees of its own — the durable runner integration supplies an
// Executor that persists each step, then Drive provides the loop.
//
// An Executor error records a failed instance (which the reference
// Schedulers treat as terminal) and returns the error.
func Drive(ctx context.Context, runID string, scheduler Scheduler, executor Executor, opts DriveOptions) (DriveResult, error) {
	maxTicks := opts.MaxTicks
	if maxTicks <= 0 {
		maxTicks = defaultMaxTicks
	}
	state := TeamState{RunID: runID}
	for tick := 1; tick <= maxTicks; tick++ {
		if err := ctx.Err(); err != nil {
			return DriveResult{State: state, Ticks: tick - 1}, err
		}
		dispatches, err := scheduler.Next(ctx, state)
		if err != nil {
			return DriveResult{State: state, Ticks: tick - 1}, err
		}
		if len(dispatches) == 0 {
			return DriveResult{State: state, Ticks: tick - 1}, nil
		}
		next, execErr := applyDispatches(ctx, runID, state, dispatches, executor)
		state = next
		if execErr != nil {
			return DriveResult{State: state, Ticks: tick}, execErr
		}
	}
	return DriveResult{State: state, Ticks: maxTicks}, ErrMaxTicksExceeded
}

func applyDispatches(ctx context.Context, runID string, state TeamState, dispatches []Dispatch, executor Executor) (TeamState, error) {
	for _, dispatch := range dispatches {
		if dispatch.Skip {
			continue
		}
		instance, task, err := executeDispatch(ctx, runID, dispatch, executor)
		state.Instances = append(state.Instances, instance)
		state.Tasks = append(state.Tasks, task)
		if err != nil {
			return state, err
		}
	}
	return state, nil
}

func executeDispatch(ctx context.Context, runID string, dispatch Dispatch, executor Executor) (AgentInstance, api.Task, error) {
	report, execErr := executor.Execute(ctx, dispatch)
	instance := AgentInstance{
		ID:        dispatch.To,
		ClassName: classNameFromTaskID(runID, dispatch.Task.ID),
		RunID:     runID,
		TaskID:    dispatch.Task.ID,
		State:     InstanceStateFinished,
		CreatedAt: time.Now().UTC(),
	}
	task := dispatch.Task
	if execErr != nil {
		instance.State = InstanceStateFailed
		task.Status = api.TaskStatusFailed
		task.Error = execErr.Error()
		return instance, task, execErr
	}
	stored := report
	task.Status = api.TaskStatusCompleted
	task.Result = &stored
	return instance, task, nil
}
