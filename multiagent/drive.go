package multiagent

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/stream"
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

// StreamingExecutor is an optional Executor that can emit live stream.Frames
// for a dispatch while it runs (e.g. by driving agent.Engine.RunStream). Drive
// calls ExecuteStream only when DriveOptions.Sink is set and the executor
// implements this interface; otherwise it uses Execute and the run proceeds
// without frames. The returned report and error must match what Execute would
// return — frames are a transient side-channel that never enters TeamState or
// the event stream (final-state-only durability).
type StreamingExecutor interface {
	Executor
	ExecuteStream(ctx context.Context, dispatch Dispatch, sink stream.Sink) (api.TypedReport, error)
}

// DriveOptions bounds a Drive run.
type DriveOptions struct {
	// MaxTicks caps scheduler iterations to guard against a Scheduler that
	// never reaches a terminal state. Defaults to 64.
	MaxTicks int
	// MaxConcurrency bounds how many of a tick's dispatches execute in
	// parallel. It defaults to 4; 1 forces sequential execution. Schedulers
	// that emit one dispatch per tick are unaffected by this field.
	MaxConcurrency int
	// UnlimitedConcurrency makes a zero MaxConcurrency run every ready
	// dispatch concurrently. It must be explicit so a zero-value options
	// struct cannot create unbounded goroutines.
	UnlimitedConcurrency bool
	// Sink, when set, receives the live stream.Frames of every node whose
	// Executor implements StreamingExecutor, each frame stamped with the node
	// name (AgentInstance.ClassName) as its Source. It is a transient
	// side-channel: frames never enter TeamState or the event stream, so a run
	// with a Sink folds the identical snapshot as one without (final-state-only
	// durability). If a dispatched Executor does not implement
	// StreamingExecutor, that node runs without frames. nil disables streaming.
	Sink stream.Sink
	// InitialState resumes from a previously persisted scheduler snapshot.
	InitialState *TeamState
	// AfterTick checkpoints the folded state after each completed tick.
	AfterTick func(context.Context, TeamState) error
}

// DriveResult is the terminal snapshot after the scheduler loop ends.
type DriveResult struct {
	State TeamState
	Ticks int
}

// ErrMaxTicksExceeded is returned when a Scheduler keeps emitting
// dispatches past DriveOptions.MaxTicks.
var ErrMaxTicksExceeded = errors.New("multiagent: scheduler exceeded max ticks")

// ErrExecutionSuspended marks a non-terminal executor interruption. Durable
// integrations use it to checkpoint pending work without failing the team run.
var ErrExecutionSuspended = errors.New("multiagent: execution suspended")

// ErrExecutorPanic marks a panic contained at the executor boundary. Drive
// reports the affected instance as failed instead of crashing the process.
var ErrExecutorPanic = errors.New("multiagent: executor panic recovered")

// ExecutionSuspendedError carries the durable task state captured at the
// suspension boundary.
type ExecutionSuspendedError struct {
	Task  api.Task
	Cause error
}

func (e *ExecutionSuspendedError) Error() string {
	if e.Cause == nil {
		return ErrExecutionSuspended.Error()
	}
	return fmt.Sprintf("%s: %v", ErrExecutionSuspended, e.Cause)
}

func (e *ExecutionSuspendedError) Unwrap() error { return e.Cause }

func (e *ExecutionSuspendedError) Is(target error) bool {
	return target == ErrExecutionSuspended
}

// SchedulerFailureError wraps an error returned by Scheduler.Next.
// Drive returns it so integrations can detect scheduler-level failure
// with errors.As, surface a typed terminal Run status, and emit an
// EventSchedulerFailure event — per ADR-016 §6 a scheduler failure must
// not cross the boundary as a bare error.
//
// Three terminal errors are deliberately NOT scheduler failures:
// executor errors (they surface as failed instances), context
// cancellation/deadline (environmental, returned unwrapped), and
// ErrMaxTicksExceeded (the host budget ran out; the scheduler made no
// erroneous decision).
type SchedulerFailureError struct {
	RunID string
	Tick  int
	Err   error
}

func (e *SchedulerFailureError) Error() string {
	return fmt.Sprintf("multiagent: scheduler failed on tick %d of run %s: %v", e.Tick, e.RunID, e.Err)
}

func (e *SchedulerFailureError) Unwrap() error { return e.Err }

const (
	defaultMaxTicks       = 64
	defaultMaxConcurrency = 4
)

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
	if opts.InitialState != nil {
		state = cloneTeamState(*opts.InitialState)
		if state.RunID == "" {
			state.RunID = runID
		}
	}
	for additionalTick := 1; additionalTick <= maxTicks; additionalTick++ {
		tick := state.Tick + 1
		if err := ctx.Err(); err != nil {
			return DriveResult{State: state, Ticks: state.Tick}, err
		}
		dispatches, err := scheduler.Next(ctx, state)
		if err != nil {
			// Cancellation surfacing through a custom Scheduler is not a
			// scheduler failure — pass it through unwrapped so integrations
			// do not map it to a SchedulerFailure event.
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return DriveResult{State: state, Ticks: state.Tick}, err
			}
			return DriveResult{State: state, Ticks: state.Tick}, &SchedulerFailureError{RunID: runID, Tick: tick, Err: err}
		}
		if len(dispatches) == 0 {
			return DriveResult{State: state, Ticks: state.Tick}, nil
		}
		maxConcurrency := opts.MaxConcurrency
		if maxConcurrency <= 0 && !opts.UnlimitedConcurrency {
			maxConcurrency = defaultMaxConcurrency
		}
		next, execErr := applyDispatches(ctx, runID, state, dispatches, executor, maxConcurrency, opts.Sink)
		state = next
		state.Tick = tick
		if opts.AfterTick != nil {
			if err := opts.AfterTick(ctx, cloneTeamState(state)); err != nil {
				return DriveResult{State: state, Ticks: state.Tick}, err
			}
		}
		if execErr != nil {
			return DriveResult{State: state, Ticks: state.Tick}, execErr
		}
	}
	return DriveResult{State: state, Ticks: state.Tick}, ErrMaxTicksExceeded
}

func cloneTeamState(state TeamState) TeamState {
	state.Tasks = append([]api.Task(nil), state.Tasks...)
	state.Instances = append([]AgentInstance(nil), state.Instances...)
	state.Blackboard = append([]api.BlackboardItem(nil), state.Blackboard...)
	return state
}

func applyDispatches(ctx context.Context, runID string, state TeamState, dispatches []Dispatch, executor Executor, maxConcurrency int, sink stream.Sink) (TeamState, error) {
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
	// single-dispatch ticks and when concurrency is explicitly disabled. Only
	// one goroutine touches sink here, so it needs no serialization.
	if len(work) == 1 || maxConcurrency == 1 {
		for _, dispatch := range work {
			instance, task, err := executeDispatch(ctx, runID, dispatch, executor, sink)
			state.Instances = append(state.Instances, instance)
			state.Tasks = append(state.Tasks, task)
			if err != nil {
				return state, err
			}
		}
		return state, nil
	}
	return applyConcurrent(ctx, runID, state, work, executor, maxConcurrency, sink)
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
func applyConcurrent(ctx context.Context, runID string, state TeamState, work []Dispatch, executor Executor, maxConcurrency int, sink stream.Sink) (TeamState, error) {
	type outcome struct {
		instance AgentInstance
		task     api.Task
		err      error
	}

	cctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Frames from the tick's goroutines all funnel into one consumer Sink;
	// serialize Emit so the caller's Sink need not be concurrency-safe.
	if sink != nil {
		sink = &serialSink{dst: sink}
	}

	limit := maxConcurrency
	if limit <= 0 || limit > len(work) {
		limit = len(work)
	}
	sem := make(chan struct{}, limit)
	results := make([]outcome, len(work))
	var (
		wg            sync.WaitGroup
		once          sync.Once
		suspensionMu  sync.Mutex
		triggerErr    error
		suspensionErr error
	)

	spawned := 0
spawnLoop:
	for i, dispatch := range work {
		// Fail-fast: once a dispatch has errored (or ctx was cancelled
		// externally) stop launching the rest of this tick rather than
		// running it under an already-cancelled context.
		if cctx.Err() != nil {
			break
		}
		select {
		case sem <- struct{}{}:
		case <-cctx.Done():
			break spawnLoop
		}
		wg.Add(1)
		spawned++
		go func(i int, dispatch Dispatch) {
			defer wg.Done()
			defer func() { <-sem }()
			instance, task, err := executeDispatch(cctx, runID, dispatch, executor, sink)
			results[i] = outcome{instance: instance, task: task, err: err}
			if err == nil {
				return
			}
			if errors.Is(err, ErrExecutionSuspended) {
				suspensionMu.Lock()
				if suspensionErr == nil {
					suspensionErr = err
				}
				suspensionMu.Unlock()
				return
			}
			once.Do(func() {
				triggerErr = err
				cancel()
			})
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
	if triggerErr != nil {
		return state, triggerErr
	}
	return state, suspensionErr
}

func executeDispatch(ctx context.Context, runID string, dispatch Dispatch, executor Executor, sink stream.Sink) (AgentInstance, api.Task, error) {
	className := dispatch.ClassName
	if className == "" {
		className = classNameFromTaskID(runID, dispatch.Task.ID)
	}
	agentClassName := dispatch.AgentClassName
	if agentClassName == "" {
		agentClassName = className
	}
	instance := AgentInstance{
		ID:             dispatch.To,
		ClassName:      className,
		AgentClassName: agentClassName,
		RunID:          runID,
		TaskID:         dispatch.Task.ID,
		State:          InstanceStateRunning,
		CreatedAt:      time.Now().UTC(),
	}
	var report api.TypedReport
	execErr := ValidateDispatch(dispatch)
	if execErr == nil {
		report, execErr = runDispatch(ctx, dispatch, executor, sink, className)
	}
	task := dispatch.Task
	var suspension *ExecutionSuspendedError
	if errors.As(execErr, &suspension) {
		instance.State = InstanceStatePending
		if suspension.Task.ID != "" {
			task = suspension.Task
		}
		return instance, task, execErr
	}
	if execErr != nil {
		instance.State = InstanceStateFailed
		task.Status = api.TaskStatusFailed
		task.Error = execErr.Error()
		return instance, task, execErr
	}
	instance.State = InstanceStateFinished
	stored := report
	task.Status = api.TaskStatusCompleted
	task.Result = &stored
	return instance, task, nil
}

// runDispatch executes dispatch, streaming frames stamped with label when sink
// is set and executor is a StreamingExecutor; otherwise it runs the plain
// Execute path. The report and error are identical to the non-streaming path —
// frames never affect the folded TeamState.
func runDispatch(
	ctx context.Context,
	dispatch Dispatch,
	executor Executor,
	sink stream.Sink,
	label string,
) (report api.TypedReport, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			report = api.TypedReport{}
			err = fmt.Errorf("%w: %v", ErrExecutorPanic, recovered)
		}
	}()
	if sink == nil {
		return executor.Execute(ctx, dispatch)
	}
	streamer, ok := executor.(StreamingExecutor)
	if !ok {
		return executor.Execute(ctx, dispatch)
	}
	return streamer.ExecuteStream(ctx, dispatch, labeledSink(label, sink))
}

// labeledSink stamps Frame.Source with label (when a frame does not already
// carry one) before forwarding to dst, so a single consumer can attribute each
// frame to the node that produced it.
func labeledSink(label string, dst stream.Sink) stream.Sink {
	return stream.SinkFunc(func(ctx context.Context, frame stream.Frame) error {
		if frame.Source == "" {
			frame.Source = label
		}
		return dst.Emit(ctx, frame)
	})
}

// serialSink serializes Emit so concurrent node goroutines can share one
// consumer Sink that is not itself concurrency-safe.
type serialSink struct {
	mu  sync.Mutex
	dst stream.Sink
}

func (s *serialSink) Emit(ctx context.Context, frame stream.Frame) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dst.Emit(ctx, frame)
}
