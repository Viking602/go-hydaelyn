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

func (h submitUserInputHandler) Handle(ctx context.Context, uow ports.UnitOfWork, cmd SubmitUserInputCommand) (any, error) {
	run, err := uow.Runs().LoadRun(ctx, cmd.RunID)
	if err != nil {
		return nil, err
	}
	if isTerminalRun(run.Status) {
		return nil, ErrTerminalState
	}
	task, shouldRedispatch, err := loadUserInputTask(ctx, uow, cmd)
	if err != nil {
		return nil, err
	}
	if shouldRedispatch {
		if err := h.authorizeUserInputRedispatch(ctx, uow, cmd); err != nil {
			return nil, err
		}
	}
	now := time.Now().UTC()
	item, err := h.writeUserInputItem(ctx, uow, cmd, now)
	if err != nil {
		return nil, err
	}
	result := submitUserInputResult{Item: item, PreviousRun: run, Input: cmd.Input}
	nextRun, runTransition, err := h.resumeRunAfterUserInput(ctx, uow, run)
	if err != nil {
		return nil, err
	}
	result.Run = nextRun
	result.RunTransition = runTransition
	if shouldRedispatch {
		nextTask, env, taskTransition, err := h.redispatchUserInputTask(ctx, uow, task)
		if err != nil {
			return nil, err
		}
		result.Task = nextTask
		result.Envelope = env
		result.Redispatched = true
		result.TaskTransition = taskTransition
	}
	if err := uow.Events().AppendEvent(ctx, Event{RunID: cmd.RunID, TaskID: cmd.TaskID, Type: EventUserInputSubmitted, Payload: map[string]any{"input": cmd.Input}, RecordedAt: time.Now().UTC()}); err != nil {
		return nil, err
	}
	return result, nil
}

func loadUserInputTask(ctx context.Context, uow ports.UnitOfWork, cmd SubmitUserInputCommand) (Task, bool, error) {
	task, err := uow.Tasks().LoadTask(ctx, cmd.RunID, cmd.TaskID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Task{}, false, nil
		}
		return Task{}, false, err
	}
	shouldRedispatch := task.Status == TaskStatusBlocked || task.Status == TaskStatusWaitingUserInput
	return task, shouldRedispatch, nil
}

func (h submitUserInputHandler) authorizeUserInputRedispatch(ctx context.Context, uow ports.UnitOfWork, cmd SubmitUserInputCommand) error {
	_, err := h.runtime.authorizeUoW(ctx, uow, PolicyRequest{Operation: PolicyOperationDispatch, RunID: cmd.RunID, TaskID: cmd.TaskID, Actor: SourceIdentity{Type: SourceSystem, ID: "user_input"}})
	return err
}

func (h submitUserInputHandler) writeUserInputItem(ctx context.Context, uow ports.UnitOfWork, cmd SubmitUserInputCommand, now time.Time) (BlackboardItem, error) {
	item := BlackboardItem{ID: h.runtime.newID("bb"), RunID: cmd.RunID, TaskID: cmd.TaskID, Source: SourceIdentity{Type: SourceSystem, ID: "user"}, Visibility: BlackboardVisibilityAgentVisible, Key: "user_input", Payload: cmd.Input, CreatedAt: now}
	if err := uow.Blackboard().WriteItem(ctx, item); err != nil {
		return BlackboardItem{}, err
	}
	if err := h.runtime.recordEndedTraceUoW(ctx, uow, cmd.RunID, cmd.TaskID, "blackboard.write", "blackboard"); err != nil {
		return BlackboardItem{}, err
	}
	if err := appendBlackboardWrittenEventUoW(ctx, uow, item); err != nil {
		return BlackboardItem{}, err
	}
	return item, nil
}

func (h submitUserInputHandler) resumeRunAfterUserInput(ctx context.Context, uow ports.UnitOfWork, run Run) (Run, bool, error) {
	nextRun, err := transitionRunPure(run, RunStatusRunning)
	if err != nil {
		return Run{}, false, err
	}
	if nextRun.Status == run.Status && nextRun.UpdatedAt.Equal(run.UpdatedAt) {
		return nextRun, false, nil
	}
	if err := uow.Runs().SaveRun(ctx, nextRun); err != nil {
		return Run{}, false, err
	}
	if run.Status == nextRun.Status {
		return nextRun, false, nil
	}
	err = uow.Events().AppendEvent(ctx, Event{RunID: nextRun.ID, TaskID: nextRun.RootTaskID, Type: EventRunStatusChanged, Payload: map[string]any{"from": string(run.Status), "to": string(nextRun.Status), "run": runPayload(nextRun)}, RecordedAt: time.Now().UTC()})
	return nextRun, true, err
}

func (h submitUserInputHandler) redispatchUserInputTask(ctx context.Context, uow ports.UnitOfWork, task Task) (Task, TaskEnvelope, bool, error) {
	nextTask, err := transitionTaskPure(task, TaskStatusDispatched, true)
	if err != nil {
		return Task{}, TaskEnvelope{}, false, err
	}
	nextTask.Error = ""
	if err := uow.Tasks().SaveTask(ctx, nextTask); err != nil {
		return Task{}, TaskEnvelope{}, false, err
	}
	if err := h.runtime.recordEndedTraceUoW(ctx, uow, nextTask.RunID, nextTask.ID, "mailbox.dispatch", "mailbox"); err != nil {
		return Task{}, TaskEnvelope{}, false, err
	}
	env := TaskEnvelope{ID: h.runtime.newID("env"), RunID: nextTask.RunID, TaskID: nextTask.ID, TargetAgentID: nextTask.OwnerAgentID, TargetComponent: nextTask.OwnerComponent, Type: "TaskEnvelope", Status: "pending", TaskVersion: nextTask.Version, ReadSelectors: slices.Clone(nextTask.ReadSelectors), WriteTargets: slices.Clone(nextTask.WriteTargets), RetryPolicy: nextTask.RetryPolicy, CreatedAt: time.Now().UTC()}
	if err := uow.MailboxOutbox().QueueEnvelope(ctx, env); err != nil {
		return Task{}, TaskEnvelope{}, false, err
	}
	if err := uow.Events().AppendEvent(ctx, Event{RunID: env.RunID, TaskID: env.TaskID, Type: EventTaskDispatched, Payload: map[string]any{"envelope": envPayload(env)}, RecordedAt: time.Now().UTC()}); err != nil {
		return Task{}, TaskEnvelope{}, false, err
	}
	return nextTask, env, task.Status != nextTask.Status, nil
}
