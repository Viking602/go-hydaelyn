package run

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/Viking602/go-hydaelyn/internal/core/model"
	"github.com/Viking602/go-hydaelyn/internal/core/ports"
	corestate "github.com/Viking602/go-hydaelyn/internal/core/state"
	"github.com/Viking602/go-hydaelyn/internal/eventpayload"
)

type IDGenerator func(prefix string) string

type StartInput struct {
	RunID      string
	RootTaskID string
	Request    string
	Metadata   map[string]string
}

type CreateTaskInput struct {
	RunID              string
	TaskID             string
	ParentTaskID       string
	Type               model.TaskType
	Goal               string
	Input              json.RawMessage
	AssignedAgentID    string
	OwnerAgentID       string
	OwnerComponent     string
	AllowsAction       bool
	Tags               []string
	CompletionCriteria []string
	DependsOn          []string
	AwaitMode          model.AwaitMode
	AwaitQuorum        int
	OnDependencyFailed model.OnDependencyFailed
	ReadSelectors      []model.BlackboardSelector
	WriteTargets       []string
	RetryPolicy        model.RetryPolicy
	PolicyDecisions    []model.PolicyDecision
	InputSchema        json.RawMessage
	OutputSchema       json.RawMessage
	Budget             *model.TaskBudget
}

func Start(ctx context.Context, uow ports.UnitOfWork, newID IDGenerator, input StartInput) (model.Run, model.Task, error) {
	now := time.Now().UTC()
	runID := input.RunID
	if runID == "" {
		runID = newID("run")
	}
	rootID := input.RootTaskID
	if rootID == "" {
		rootID = newID("task")
	}
	run := model.Run{
		ID:         runID,
		Status:     model.RunStatusCreated,
		Request:    input.Request,
		RootTaskID: rootID,
		Metadata:   maps.Clone(input.Metadata),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	root := model.Task{
		ID:             rootID,
		RunID:          runID,
		Type:           model.TaskTypeWorker,
		OwnerComponent: "orchestrator",
		Status:         model.TaskStatusCreated,
		Version:        1,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := uow.Runs().SaveRun(ctx, run); err != nil {
		return model.Run{}, model.Task{}, err
	}
	if err := uow.Tasks().SaveTask(ctx, root); err != nil {
		return model.Run{}, model.Task{}, err
	}
	if err := uow.Events().AppendEvent(ctx, model.Event{RunID: runID, TaskID: rootID, Type: model.EventRunStarted, Payload: map[string]any{"request": input.Request, "run": eventpayload.Run(run)}, RecordedAt: now}); err != nil {
		return model.Run{}, model.Task{}, err
	}
	if err := uow.Events().AppendEvent(ctx, model.Event{RunID: runID, TaskID: rootID, Type: model.EventTaskCreated, Payload: eventpayload.Task(root), RecordedAt: now}); err != nil {
		return model.Run{}, model.Task{}, err
	}
	return run, root, nil
}

// cloneTaskBudget deep-copies the per-task budget so the stored task does not
// alias the caller's pointer. The budget is a flat value struct, so copying
// the pointee is a complete clone.
func cloneTaskBudget(budget *model.TaskBudget) *model.TaskBudget {
	if budget == nil {
		return nil
	}
	cloned := *budget
	return &cloned
}

func CreateTask(ctx context.Context, uow ports.UnitOfWork, newID IDGenerator, input CreateTaskInput) (model.Task, error) {
	if err := validateTaskJSON(input); err != nil {
		return model.Task{}, err
	}
	run, err := uow.Runs().LoadRun(ctx, input.RunID)
	if err != nil {
		return model.Task{}, err
	}
	if corestate.IsTerminalRun(run.Status) {
		return model.Task{}, model.ErrTerminalState
	}
	taskID := input.TaskID
	if taskID == "" {
		taskID = newID("task")
	}
	now := time.Now().UTC()
	status := model.TaskStatusCreated
	if len(input.DependsOn) > 0 {
		status = model.TaskStatusWaitingDependency
	}
	task := model.Task{
		ID:                 taskID,
		RunID:              input.RunID,
		ParentTaskID:       input.ParentTaskID,
		Type:               input.Type,
		Goal:               input.Goal,
		Input:              slices.Clone(input.Input),
		AssignedAgentID:    input.AssignedAgentID,
		OwnerAgentID:       input.OwnerAgentID,
		OwnerComponent:     input.OwnerComponent,
		Status:             status,
		Version:            1,
		AllowsAction:       input.AllowsAction,
		Tags:               slices.Clone(input.Tags),
		CompletionCriteria: slices.Clone(input.CompletionCriteria),
		DependsOn:          slices.Clone(input.DependsOn),
		AwaitMode:          input.AwaitMode,
		AwaitQuorum:        input.AwaitQuorum,
		OnDependencyFailed: input.OnDependencyFailed,
		ReadSelectors:      slices.Clone(input.ReadSelectors),
		WriteTargets:       slices.Clone(input.WriteTargets),
		RetryPolicy:        input.RetryPolicy,
		PolicyDecisions:    slices.Clone(input.PolicyDecisions),
		InputSchema:        slices.Clone(input.InputSchema),
		OutputSchema:       slices.Clone(input.OutputSchema),
		Budget:             cloneTaskBudget(input.Budget),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if task.Type == "" {
		task.Type = model.TaskTypeWorker
	}
	if task.AssignedAgentID == "" && task.OwnerAgentID != "" {
		task.AssignedAgentID = task.OwnerAgentID
	}
	if task.OwnerAgentID != "" {
		task.OwnerHistory = []string{task.OwnerAgentID}
	}
	if err := uow.Tasks().SaveTask(ctx, task); err != nil {
		return model.Task{}, err
	}
	if err := uow.Events().AppendEvent(ctx, model.Event{RunID: input.RunID, TaskID: task.ID, Type: model.EventTaskCreated, Payload: eventpayload.Task(task), RecordedAt: now}); err != nil {
		return model.Task{}, err
	}
	return task, nil
}

func validateTaskJSON(input CreateTaskInput) error {
	for _, field := range []struct {
		name  string
		value json.RawMessage
	}{
		{name: "input", value: input.Input},
		{name: "input schema", value: input.InputSchema},
		{name: "output schema", value: input.OutputSchema},
	} {
		if len(field.value) > 0 && !json.Valid(field.value) {
			return fmt.Errorf("run: task %s must be valid JSON", field.name)
		}
	}
	return nil
}
