package multiagent

import (
	"context"
	"errors"
	"sort"
	"sync"
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
	// MaxConcurrency bounds how many of a tick's dispatches execute in
	// parallel. 0 means unbounded (run every ready dispatch concurrently);
	// 1 forces sequential execution. Schedulers that emit one dispatch per
	// tick (Sequential/Router/Supervisor) are unaffected by this field.
	MaxConcurrency int
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
		next, execErr := applyDispatches(ctx, runID, state, dispatches, executor, opts.MaxConcurrency)
		state = next
		if execErr != nil {
			return DriveResult{State: state, Ticks: tick}, execErr
		}
	}
	return DriveResult{State: state, Ticks: maxTicks}, ErrMaxTicksExceeded
}

func applyDispatches(ctx context.Context, runID string, state TeamState, dispatches []Dispatch, executor Executor, maxConcurrency int) (TeamState, error) {
	work := make([]Dispatch, 0, len(dispatches))
	for _, dispatch := range dispatches {
		if !dispatch.Skip {
			work = append(work, dispatch)
		}
	}
	if len(work) == 0 {
		return state, nil
	}
	// Sequential fast path preserves the original behavior exactly for
	// single-dispatch ticks and when concurrency is explicitly disabled.
	if len(work) == 1 || maxConcurrency == 1 {
		for _, dispatch := range work {
			instance, task, err := executeDispatch(ctx, runID, dispatch, executor)
			state.Instances = append(state.Instances, instance)
			state.Tasks = append(state.Tasks, task)
			if err != nil {
				return state, err
			}
		}
		return state, nil
	}
	return applyConcurrent(ctx, runID, state, work, executor, maxConcurrency)
}

// applyConcurrent executes a tick's dispatches in parallel (bounded by
// maxConcurrency; 0 means unbounded), then folds the results into TeamState
// ordered by node id (AgentInstance.ClassName, tie-broken by instance ID) —
// the same ordering Next and the sequential path use — so the folded state
// is identical regardless of completion order or concurrency setting, keeping
// Next a stable function of the snapshot. The first executor error is captured
// via sync.Once, cancels in-flight work, and stops the rest of the tick from
// launching (fail-fast); that root-cause error is returned after all spawned
// goroutines drain, never masked by a sibling's context.Canceled.
func applyConcurrent(ctx context.Context, runID string, state TeamState, work []Dispatch, executor Executor, maxConcurrency int) (TeamState, error) {
	type outcome struct {
		instance AgentInstance
		task     api.Task
		err      error
	}

	cctx, cancel := context.WithCancel(ctx)
	defer cancel()

	limit := maxConcurrency
	if limit <= 0 || limit > len(work) {
		limit = len(work)
	}
	sem := make(chan struct{}, limit)
	results := make([]outcome, len(work))
	var (
		wg         sync.WaitGroup
		once       sync.Once
		triggerErr error
	)

	spawned := 0
	for i, dispatch := range work {
		// Fail-fast: once a dispatch has errored (or ctx was cancelled
		// externally) stop launching the rest of this tick rather than
		// running it under an already-cancelled context.
		if cctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		spawned++
		go func(i int, dispatch Dispatch) {
			defer wg.Done()
			defer func() { <-sem }()
			instance, task, err := executeDispatch(cctx, runID, dispatch, executor)
			results[i] = outcome{instance: instance, task: task, err: err}
			if err != nil {
				once.Do(func() {
					triggerErr = err
					cancel()
				})
			}
		}(i, dispatch)
	}
	wg.Wait()

	// Fold only the dispatches that actually launched, ordered by node id so
	// the snapshot is independent of completion order and matches the
	// sequential path.
	folded := results[:spawned]
	sort.Slice(folded, func(a, b int) bool {
		if folded[a].instance.ClassName != folded[b].instance.ClassName {
			return folded[a].instance.ClassName < folded[b].instance.ClassName
		}
		return folded[a].instance.ID < folded[b].instance.ID
	})
	for _, result := range folded {
		state.Instances = append(state.Instances, result.instance)
		state.Tasks = append(state.Tasks, result.task)
	}
	return state, triggerErr
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
