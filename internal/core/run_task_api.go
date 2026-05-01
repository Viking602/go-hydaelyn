package core

import (
	"context"
	"slices"
)

type StartRunCommand struct {
	RunID      string
	RootTaskID string
	Request    string
	Metadata   map[string]string
}

type CreateTaskCommand struct {
	RunID              string
	TaskID             string
	ParentTaskID       string
	Type               TaskType
	Goal               string
	AssignedAgentID    string
	OwnerAgentID       string
	OwnerComponent     string
	AllowsAction       bool
	Tags               []string
	CompletionCriteria []string
	DependsOn          []string
	AwaitMode          AwaitMode
	AwaitQuorum        int
	OnDependencyFailed OnDependencyFailed
	ReadSelectors      []BlackboardSelector
	WriteTargets       []string
	RetryPolicy        RetryPolicy
	PolicyDecisions    []PolicyDecision
}

func (r *Runtime) StartRun(ctx context.Context, cmd StartRunCommand) (Run, Task, error) {
	result, err := r.ExecuteCommand(ctx, cmd)
	if err != nil {
		return Run{}, Task{}, err
	}
	items, ok := result.([]any)
	if !ok || len(items) < 2 {
		return Run{}, Task{}, ErrInvalidCommand
	}
	run, okRun := items[0].(Run)
	root, okTask := items[1].(Task)
	if !okRun || !okTask {
		return Run{}, Task{}, ErrInvalidCommand
	}
	return run, root, nil
}

func (r *Runtime) CreateTask(ctx context.Context, cmd CreateTaskCommand) (Task, error) {
	result, err := r.ExecuteCommand(ctx, cmd)
	if err != nil {
		return Task{}, err
	}
	task, ok := result.(Task)
	if !ok {
		return Task{}, ErrInvalidCommand
	}
	return task, nil
}

func (r *Runtime) Run(ctx context.Context, runID string) (Run, error) {
	uow, done, err := r.beginReadUoW(ctx)
	if err != nil {
		return Run{}, err
	}
	defer done()
	return uow.Runs().LoadRun(ctx, runID)
}

func (r *Runtime) Task(ctx context.Context, runID, taskID string) (Task, error) {
	uow, done, err := r.beginReadUoW(ctx)
	if err != nil {
		return Task{}, err
	}
	defer done()
	return uow.Tasks().LoadTask(ctx, runID, taskID)
}

func (r *Runtime) ReadyTasks(runID string) []Task {
	ctx := context.Background()
	uow, done, err := r.beginReadUoW(ctx)
	if err != nil {
		return nil
	}
	defer done()
	tasks, err := uow.Tasks().ListTasks(ctx, runID)
	if err != nil {
		return nil
	}
	byID := make(map[string]Task, len(tasks))
	for _, task := range tasks {
		byID[task.ID] = task
	}
	out := make([]Task, 0, len(tasks))
	for _, task := range tasks {
		ready, _ := dependencyGate(task, byID)
		if !taskCanBecomeReady(task.Status) || !ready {
			continue
		}
		out = append(out, task)
	}
	slices.SortFunc(out, func(a, b Task) int {
		return stringsCompare(a.ID, b.ID)
	})
	return out
}

func (r *Runtime) Events(runID string) []Event {
	events, _ := r.RunEvents(context.Background(), runID)
	return events
}

func (r *Runtime) RunEvents(ctx context.Context, runID string) ([]Event, error) {
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

func (r *Runtime) ActiveLeaseCount(runID, taskID string) int {
	ctx := context.Background()
	uow, done, err := r.beginReadUoW(ctx)
	if err != nil {
		return 0
	}
	defer done()
	lease, ok, err := uow.Leases().ActiveLeaseForTask(ctx, runID, taskID)
	if err != nil || !ok || lease.Status != LeaseStatusActive {
		return 0
	}
	return 1
}
