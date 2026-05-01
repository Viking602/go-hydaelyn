package core

import (
	"context"
	"maps"
	"slices"
	"time"

	commandbus "github.com/Viking602/go-hydaelyn/internal/core/command"
	"github.com/Viking602/go-hydaelyn/internal/core/ports"
)

func (r *Runtime) registerUoWCommandHandlers() {
	commandbus.Register[StartRunCommand](r.commandBus, startRunHandler{runtime: r})
	commandbus.Register[CreateTaskCommand](r.commandBus, createTaskHandler{runtime: r})
	registerStateUoWCommandHandlers(r)
	registerAdvanceRunUoWCommandHandlers(r)
	registerBlackboardUoWCommandHandlers(r)
	registerMailboxUoWCommandHandlers(r)
	registerMailboxDispatchUoWCommandHandlers(r)
	registerDeadLetterUoWCommandHandlers(r)
	registerExecutionUoWCommandHandlers(r)
	registerResponseUoWCommandHandlers(r)
	registerReportUoWCommandHandlers(r)
	registerUserInputUoWCommandHandlers(r)
	registerActionUoWCommandHandlers(r)
	registerApprovalUoWCommandHandlers(r)
	registerHandoffUoWCommandHandlers(r)
	registerToolUoWCommandHandlers(r)
	registerTraceUoWCommandHandlers(r)
}

type startRunHandler struct {
	runtime *Runtime
}

func (h startRunHandler) Name() string { return StartRunCommand{}.CommandName() }

func (h startRunHandler) Handle(ctx context.Context, uow ports.FullUnitOfWork, cmd StartRunCommand) (any, error) {
	now := time.Now().UTC()
	runID := cmd.RunID
	if runID == "" {
		runID = h.runtime.newID("run")
	}
	rootID := cmd.RootTaskID
	if rootID == "" {
		rootID = h.runtime.newID("task")
	}
	run := Run{
		ID:         runID,
		Status:     RunStatusCreated,
		Request:    cmd.Request,
		RootTaskID: rootID,
		Metadata:   maps.Clone(cmd.Metadata),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	root := Task{
		ID:             rootID,
		RunID:          runID,
		Type:           TaskTypeWorker,
		OwnerComponent: "orchestrator",
		Status:         TaskStatusCreated,
		Version:        1,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := uow.Runs().SaveRun(ctx, run); err != nil {
		return nil, err
	}
	if err := uow.Tasks().SaveTask(ctx, root); err != nil {
		return nil, err
	}
	if err := uow.Events().AppendEvent(ctx, Event{RunID: runID, TaskID: rootID, Type: EventRunStarted, Payload: map[string]any{"request": cmd.Request, "run": runPayload(run)}, RecordedAt: now}); err != nil {
		return nil, err
	}
	if err := uow.Events().AppendEvent(ctx, Event{RunID: runID, TaskID: rootID, Type: EventTaskCreated, Payload: taskEventPayload(root), RecordedAt: now}); err != nil {
		return nil, err
	}
	return []any{run, root}, nil
}

type createTaskHandler struct {
	runtime *Runtime
}

func (h createTaskHandler) Name() string { return CreateTaskCommand{}.CommandName() }

func (h createTaskHandler) Handle(ctx context.Context, uow ports.FullUnitOfWork, cmd CreateTaskCommand) (any, error) {
	run, err := uow.Runs().LoadRun(ctx, cmd.RunID)
	if err != nil {
		return nil, err
	}
	if isTerminalRun(run.Status) {
		return nil, ErrTerminalState
	}
	taskID := cmd.TaskID
	if taskID == "" {
		taskID = h.runtime.newID("task")
	}
	now := time.Now().UTC()
	status := TaskStatusCreated
	if len(cmd.DependsOn) > 0 {
		status = TaskStatusWaitingDependency
	}
	task := Task{
		ID:                 taskID,
		RunID:              cmd.RunID,
		ParentTaskID:       cmd.ParentTaskID,
		Type:               cmd.Type,
		Goal:               cmd.Goal,
		AssignedAgentID:    cmd.AssignedAgentID,
		OwnerAgentID:       cmd.OwnerAgentID,
		OwnerComponent:     cmd.OwnerComponent,
		Status:             status,
		Version:            1,
		AllowsAction:       cmd.AllowsAction,
		Tags:               slices.Clone(cmd.Tags),
		CompletionCriteria: slices.Clone(cmd.CompletionCriteria),
		DependsOn:          slices.Clone(cmd.DependsOn),
		AwaitMode:          cmd.AwaitMode,
		AwaitQuorum:        cmd.AwaitQuorum,
		OnDependencyFailed: cmd.OnDependencyFailed,
		ReadSelectors:      slices.Clone(cmd.ReadSelectors),
		WriteTargets:       slices.Clone(cmd.WriteTargets),
		RetryPolicy:        cmd.RetryPolicy,
		PolicyDecisions:    slices.Clone(cmd.PolicyDecisions),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if task.Type == "" {
		task.Type = TaskTypeWorker
	}
	if task.AssignedAgentID == "" && task.OwnerAgentID != "" {
		task.AssignedAgentID = task.OwnerAgentID
	}
	if task.OwnerAgentID != "" {
		task.OwnerHistory = []string{task.OwnerAgentID}
	}
	if err := uow.Tasks().SaveTask(ctx, task); err != nil {
		return nil, err
	}
	if err := uow.Events().AppendEvent(ctx, Event{RunID: cmd.RunID, TaskID: task.ID, Type: EventTaskCreated, Payload: taskEventPayload(task), RecordedAt: now}); err != nil {
		return nil, err
	}
	return task, nil
}
