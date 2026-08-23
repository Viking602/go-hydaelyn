package run

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/internal/core/ports"
	corestate "github.com/Viking602/venat/internal/core/state"
	"github.com/Viking602/venat/internal/eventpayload"
)

type IDGenerator func(prefix string) string

type StartInput struct {
	RunID        string
	RootTaskID   string
	Request      string
	AgentVersion string
	Metadata     map[string]string
}

type CreateTaskInput struct {
	RunID              string
	TaskID             string
	ParentTaskID       string
	Type               api.TaskType
	Goal               string
	Input              json.RawMessage
	AssignedAgentID    string
	OwnerAgentID       string
	OwnerComponent     string
	AllowsAction       bool
	Tags               []string
	CompletionCriteria []string
	DependsOn          []string
	AwaitMode          api.AwaitMode
	AwaitQuorum        int
	OnDependencyFailed api.OnDependencyFailed
	ReadSelectors      []api.BlackboardSelector
	WriteTargets       []string
	RetryPolicy        api.RetryPolicy
	PolicyDecisions    []api.PolicyDecision
	InputSchema        json.RawMessage
	OutputSchema       json.RawMessage
	Budget             *api.TaskBudget
	ResourceClaims     []api.ResourceClaimSpec
}

func Start(ctx context.Context, uow ports.UnitOfWork, newID IDGenerator, input StartInput) (api.Run, api.Task, error) {
	run, root, _, err := start(ctx, uow, newID, input)
	return run, root, err
}

func start(ctx context.Context, uow ports.UnitOfWork, newID IDGenerator, input StartInput) (api.Run, api.Task, bool, error) {
	now := time.Now().UTC()
	runID := input.RunID
	if runID == "" {
		runID = newID("run")
	}
	rootID := input.RootTaskID
	if rootID == "" {
		rootID = newID("task")
	}
	existing, err := uow.Runs().LoadRun(ctx, runID)
	if err == nil {
		if existing.RootTaskID != rootID || existing.Request != input.Request ||
			existing.AgentVersion != input.AgentVersion ||
			!maps.Equal(existing.Metadata, input.Metadata) {
			return api.Run{}, api.Task{}, false, fmt.Errorf(
				"run: start input conflicts with existing run %q: %w",
				runID,
				api.ErrIdempotencyConflict,
			)
		}
		root, loadErr := uow.Tasks().LoadTask(ctx, runID, rootID)
		if loadErr != nil {
			return api.Run{}, api.Task{}, false, loadErr
		}
		return existing, root, false, nil
	}
	if !errors.Is(err, api.ErrNotFound) {
		return api.Run{}, api.Task{}, false, err
	}
	run := api.Run{
		ID:           runID,
		Status:       api.RunStatusCreated,
		Request:      input.Request,
		RootTaskID:   rootID,
		AgentVersion: input.AgentVersion,
		Metadata:     maps.Clone(input.Metadata),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	root := api.Task{
		ID:             rootID,
		RunID:          runID,
		Type:           api.TaskTypeWorker,
		OwnerComponent: "orchestrator",
		Status:         api.TaskStatusCreated,
		Version:        1,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := uow.Runs().SaveRun(ctx, run); err != nil {
		return api.Run{}, api.Task{}, false, err
	}
	if err := uow.Tasks().SaveTask(ctx, root); err != nil {
		return api.Run{}, api.Task{}, false, err
	}
	if err := uow.Events().AppendEvent(ctx, api.Event{RunID: runID, TaskID: rootID, Type: api.EventRunStarted, Payload: map[string]any{"request": input.Request, "run": eventpayload.Run(run)}, RecordedAt: now}); err != nil {
		return api.Run{}, api.Task{}, false, err
	}
	if err := uow.Events().AppendEvent(ctx, api.Event{RunID: runID, TaskID: rootID, Type: api.EventTaskCreated, Payload: eventpayload.Task(root), RecordedAt: now}); err != nil {
		return api.Run{}, api.Task{}, false, err
	}
	return run, root, true, nil
}

// cloneTaskBudget deep-copies the per-task budget so the stored task does not
// alias the caller's pointer. The budget is a flat value struct, so copying
// the pointee is a complete clone.
func cloneTaskBudget(budget *api.TaskBudget) *api.TaskBudget {
	if budget == nil {
		return nil
	}
	cloned := *budget
	return &cloned
}

func CreateTask(ctx context.Context, uow ports.UnitOfWork, newID IDGenerator, input CreateTaskInput) (api.Task, error) {
	if err := validateTaskInput(input); err != nil {
		return api.Task{}, err
	}
	run, err := uow.Runs().LoadRun(ctx, input.RunID)
	if err != nil {
		return api.Task{}, err
	}
	if corestate.IsTerminalRun(run.Status) {
		return api.Task{}, api.ErrTerminalState
	}
	taskID := input.TaskID
	if taskID == "" {
		taskID = newID("task")
	}
	now := time.Now().UTC()
	status := api.TaskStatusCreated
	if len(input.DependsOn) > 0 {
		status = api.TaskStatusWaitingDependency
	}
	task := api.Task{
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
		ResourceClaims:     slices.Clone(input.ResourceClaims),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if task.Type == "" {
		task.Type = api.TaskTypeWorker
	}
	if task.AssignedAgentID == "" && task.OwnerAgentID != "" {
		task.AssignedAgentID = task.OwnerAgentID
	}
	if task.OwnerAgentID != "" {
		task.OwnerHistory = []string{task.OwnerAgentID}
	}
	if err := uow.Tasks().SaveTask(ctx, task); err != nil {
		return api.Task{}, err
	}
	if err := uow.Events().AppendEvent(ctx, api.Event{RunID: input.RunID, TaskID: task.ID, Type: api.EventTaskCreated, Payload: eventpayload.Task(task), RecordedAt: now}); err != nil {
		return api.Task{}, err
	}
	return task, nil
}

func validateTaskInput(input CreateTaskInput) error {
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
	if input.RetryPolicy.MaxAttempts < 0 || input.RetryPolicy.MaxAttempts > api.MaxRetryAttempts {
		return fmt.Errorf(
			"run: task retry max attempts must be between 0 and %d: %w",
			api.MaxRetryAttempts,
			api.ErrInvalidCommand,
		)
	}
	if input.RetryPolicy.Backoff < 0 || input.RetryPolicy.MaxBackoff < 0 {
		return fmt.Errorf("run: task retry delays must not be negative: %w", api.ErrInvalidCommand)
	}
	claimKeys := make(map[string]struct{}, len(input.ResourceClaims))
	for _, claim := range input.ResourceClaims {
		if claim.ID != "" || strings.TrimSpace(claim.Key) == "" {
			return fmt.Errorf("run: task resource claims require a key and cannot preassign an ID: %w", api.ErrInvalidCommand)
		}
		if claim.Mode != api.ResourceClaimShared && claim.Mode != api.ResourceClaimExclusive {
			return fmt.Errorf("run: task resource claim %q has invalid mode %q: %w", claim.Key, claim.Mode, api.ErrInvalidCommand)
		}
		if _, duplicate := claimKeys[claim.Key]; duplicate {
			return fmt.Errorf("run: duplicate task resource claim key %q: %w", claim.Key, api.ErrInvalidCommand)
		}
		claimKeys[claim.Key] = struct{}{}
	}
	return nil
}
