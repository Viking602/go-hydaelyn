package core

import (
	"context"
	"slices"
	"time"

	"github.com/Viking602/venat/internal/core/model"
	runsvc "github.com/Viking602/venat/internal/run"
)

type (
	StartRunCommand   = runsvc.StartRunCommand
	CreateTaskCommand = runsvc.CreateTaskCommand
	StartRunResult    = runsvc.StartRunResult
)

func (r *Runtime) StartRunWithResult(ctx context.Context, cmd StartRunCommand) (StartRunResult, error) {
	result, err := r.ExecuteCommand(ctx, cmd)
	if err != nil {
		return StartRunResult{}, err
	}
	started, ok := result.(StartRunResult)
	if !ok {
		return StartRunResult{}, ErrInvalidCommand
	}
	return started, nil
}

func (r *Runtime) StartRun(ctx context.Context, cmd StartRunCommand) (model.Run, model.Task, error) {
	started, err := r.StartRunWithResult(ctx, cmd)
	if err != nil {
		return model.Run{}, model.Task{}, err
	}
	return started.Run, started.Root, nil
}

func (r *Runtime) CreateTask(ctx context.Context, cmd CreateTaskCommand) (model.Task, error) {
	result, err := r.ExecuteCommand(ctx, cmd)
	if err != nil {
		return model.Task{}, err
	}
	task, ok := result.(model.Task)
	if !ok {
		return model.Task{}, ErrInvalidCommand
	}
	return task, nil
}

func (r *Runtime) Run(ctx context.Context, runID string) (model.Run, error) {
	uow, done, err := r.beginReadUoW(ctx)
	if err != nil {
		return model.Run{}, err
	}
	defer done()
	return uow.Runs().LoadRun(ctx, runID)
}

func (r *Runtime) Task(ctx context.Context, runID, taskID string) (model.Task, error) {
	uow, done, err := r.beginReadUoW(ctx)
	if err != nil {
		return model.Task{}, err
	}
	defer done()
	return uow.Tasks().LoadTask(ctx, runID, taskID)
}

// ReadyTasks returns tasks that can become ready. Store errors are returned;
// an empty slice means the store confirmed there are no ready tasks.
func (r *Runtime) ReadyTasks(ctx context.Context, runID string) ([]model.Task, error) {
	uow, done, err := r.beginReadUoW(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	tasks, err := uow.Tasks().ListTasks(ctx, runID)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]model.Task, len(tasks))
	for _, task := range tasks {
		byID[task.ID] = task
	}
	out := make([]model.Task, 0, len(tasks))
	for _, task := range tasks {
		ready, _ := dependencyGate(task, byID)
		if !taskCanBecomeReady(task.Status) || !ready {
			continue
		}
		out = append(out, task)
	}
	slices.SortFunc(out, func(a, b model.Task) int {
		return stringsCompare(a.ID, b.ID)
	})
	return out, nil
}

func (r *Runtime) Events(ctx context.Context, runID string) []model.Event {
	events, _ := r.RunEvents(ctx, runID)
	return events
}

func (r *Runtime) RunEvents(ctx context.Context, runID string) ([]model.Event, error) {
	uow, done, err := r.beginReadUoW(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	events, err := uow.Events().ListEvents(ctx, runID)
	if err != nil {
		return nil, err
	}
	if len(events) == 0 {
		if _, err := uow.Runs().LoadRun(ctx, runID); err != nil {
			return nil, err
		}
	}
	return slices.Clone(events), nil
}

// ActiveLeaseCount returns 1 when the task has a non-expired active lease.
// A count of 0 means the store confirmed there is none; store errors are
// returned instead of collapsing to 0.
func (r *Runtime) ActiveLeaseCount(ctx context.Context, runID, taskID string) (int, error) {
	uow, done, err := r.beginReadUoW(ctx)
	if err != nil {
		return 0, err
	}
	defer done()
	lease, ok, err := uow.Leases().ActiveLeaseForTask(ctx, runID, taskID)
	if err != nil {
		return 0, err
	}
	if !ok || lease.Status != model.LeaseStatusActive || !model.LeaseExpiry(lease).After(time.Now().UTC()) {
		return 0, nil
	}
	return 1, nil
}
