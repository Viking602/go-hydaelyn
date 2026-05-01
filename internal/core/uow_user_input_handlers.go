package core

import (
	"context"
	"errors"
	"slices"
	"time"

	commandbus "github.com/Viking602/go-hydaelyn/internal/core/command"
	"github.com/Viking602/go-hydaelyn/internal/core/ports"
)

func registerUserInputUoWCommandHandlers(runtime *Runtime) {
	commandbus.Register[SubmitUserInputCommand](runtime.commandBus, submitUserInputHandler{runtime: runtime})
}

type submitUserInputResult struct {
	Item           BlackboardItem
	Run            Run
	PreviousRun    Run
	Task           Task
	Envelope       TaskEnvelope
	Input          string
	RunTransition  bool
	Redispatched   bool
	TaskTransition bool
}

type submitUserInputHandler struct{ runtime *Runtime }

func (submitUserInputHandler) Name() string { return SubmitUserInputCommand{}.CommandName() }

func (h submitUserInputHandler) Handle(ctx context.Context, uow ports.FullUnitOfWork, cmd SubmitUserInputCommand) (any, error) {
	run, err := uow.Runs().LoadRun(ctx, cmd.RunID)
	if err != nil {
		return nil, err
	}
	if isTerminalRun(run.Status) {
		return nil, ErrTerminalState
	}
	task, taskErr := uow.Tasks().LoadTask(ctx, cmd.RunID, cmd.TaskID)
	if taskErr != nil && !errors.Is(taskErr, ErrNotFound) {
		return nil, taskErr
	}
	shouldRedispatch := taskErr == nil && (task.Status == TaskStatusBlocked || task.Status == TaskStatusWaitingUserInput)
	if shouldRedispatch {
		if _, err := h.runtime.authorizeUoW(ctx, uow, PolicyRequest{Operation: PolicyOperationDispatch, RunID: cmd.RunID, TaskID: cmd.TaskID, Actor: SourceIdentity{Type: SourceSystem, ID: "user_input"}}); err != nil {
			return nil, err
		}
	}
	now := time.Now().UTC()
	item := BlackboardItem{ID: h.runtime.newID("bb"), RunID: cmd.RunID, TaskID: cmd.TaskID, Source: SourceIdentity{Type: SourceSystem, ID: "user"}, Visibility: BlackboardVisibilityAgentVisible, Key: "user_input", Payload: cmd.Input, CreatedAt: now}
	if err := uow.Blackboard().WriteItem(ctx, item); err != nil {
		return nil, err
	}
	if err := h.runtime.recordEndedTraceUoW(ctx, uow, cmd.RunID, cmd.TaskID, "blackboard.write", "blackboard"); err != nil {
		return nil, err
	}
	if err := appendBlackboardWrittenEventUoW(ctx, uow, item); err != nil {
		return nil, err
	}
	result := submitUserInputResult{Item: item, PreviousRun: run, Input: cmd.Input}
	nextRun, err := transitionRunPure(run, RunStatusRunning)
	if err != nil {
		return nil, err
	}
	if nextRun.Status != run.Status || !nextRun.UpdatedAt.Equal(run.UpdatedAt) {
		if err := uow.Runs().SaveRun(ctx, nextRun); err != nil {
			return nil, err
		}
		if run.Status != nextRun.Status {
			if err := uow.Events().AppendEvent(ctx, Event{RunID: nextRun.ID, TaskID: nextRun.RootTaskID, Type: EventRunStatusChanged, Payload: map[string]any{"from": string(run.Status), "to": string(nextRun.Status), "run": runPayload(nextRun)}, RecordedAt: time.Now().UTC()}); err != nil {
				return nil, err
			}
			result.RunTransition = true
		}
	}
	result.Run = nextRun
	if shouldRedispatch {
		nextTask, err := transitionTaskPure(task, TaskStatusDispatched, true)
		if err != nil {
			return nil, err
		}
		nextTask.Error = ""
		if err := uow.Tasks().SaveTask(ctx, nextTask); err != nil {
			return nil, err
		}
		if err := h.runtime.recordEndedTraceUoW(ctx, uow, cmd.RunID, cmd.TaskID, "mailbox.dispatch", "mailbox"); err != nil {
			return nil, err
		}
		env := TaskEnvelope{ID: h.runtime.newID("env"), RunID: cmd.RunID, TaskID: cmd.TaskID, TargetAgentID: nextTask.OwnerAgentID, TargetComponent: nextTask.OwnerComponent, Type: "TaskEnvelope", Status: "pending", TaskVersion: nextTask.Version, ReadSelectors: slices.Clone(nextTask.ReadSelectors), WriteTargets: slices.Clone(nextTask.WriteTargets), RetryPolicy: nextTask.RetryPolicy, CreatedAt: time.Now().UTC()}
		if err := uow.MailboxOutbox().QueueEnvelope(ctx, env); err != nil {
			return nil, err
		}
		if err := uow.Events().AppendEvent(ctx, Event{RunID: env.RunID, TaskID: env.TaskID, Type: EventTaskDispatched, Payload: map[string]any{"envelope": envPayload(env)}, RecordedAt: time.Now().UTC()}); err != nil {
			return nil, err
		}
		result.Task = nextTask
		result.Envelope = env
		result.Redispatched = true
		result.TaskTransition = task.Status != nextTask.Status
	}
	if err := uow.Events().AppendEvent(ctx, Event{RunID: cmd.RunID, TaskID: cmd.TaskID, Type: EventUserInputSubmitted, Payload: map[string]any{"input": cmd.Input}, RecordedAt: time.Now().UTC()}); err != nil {
		return nil, err
	}
	return result, nil
}
