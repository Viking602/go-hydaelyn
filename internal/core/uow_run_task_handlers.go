package core

import (
	"context"

	commandbus "github.com/Viking602/go-hydaelyn/internal/core/command"
	"github.com/Viking602/go-hydaelyn/internal/core/ports"
	runsvc "github.com/Viking602/go-hydaelyn/internal/run"
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

func (h startRunHandler) Handle(ctx context.Context, uow ports.UnitOfWork, cmd StartRunCommand) (any, error) {
	run, root, err := runsvc.Start(ctx, uow, h.runtime.newID, runsvc.StartInput{
		RunID:      cmd.RunID,
		RootTaskID: cmd.RootTaskID,
		Request:    cmd.Request,
		Metadata:   cmd.Metadata,
	})
	if err != nil {
		return nil, err
	}
	return []any{run, root}, nil
}

type createTaskHandler struct {
	runtime *Runtime
}

func (h createTaskHandler) Name() string { return CreateTaskCommand{}.CommandName() }

func (h createTaskHandler) Handle(ctx context.Context, uow ports.UnitOfWork, cmd CreateTaskCommand) (any, error) {
	return runsvc.CreateTask(ctx, uow, h.runtime.newID, runsvc.CreateTaskInput{
		RunID:              cmd.RunID,
		TaskID:             cmd.TaskID,
		ParentTaskID:       cmd.ParentTaskID,
		Type:               cmd.Type,
		Goal:               cmd.Goal,
		AssignedAgentID:    cmd.AssignedAgentID,
		OwnerAgentID:       cmd.OwnerAgentID,
		OwnerComponent:     cmd.OwnerComponent,
		AllowsAction:       cmd.AllowsAction,
		Tags:               cmd.Tags,
		CompletionCriteria: cmd.CompletionCriteria,
		DependsOn:          cmd.DependsOn,
		AwaitMode:          cmd.AwaitMode,
		AwaitQuorum:        cmd.AwaitQuorum,
		OnDependencyFailed: cmd.OnDependencyFailed,
		ReadSelectors:      cmd.ReadSelectors,
		WriteTargets:       cmd.WriteTargets,
		RetryPolicy:        cmd.RetryPolicy,
		PolicyDecisions:    cmd.PolicyDecisions,
	})
}
