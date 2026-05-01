package core

import (
	"context"
	"time"

	commandbus "github.com/Viking602/go-hydaelyn/internal/core/command"
	"github.com/Viking602/go-hydaelyn/internal/core/ports"
	corestate "github.com/Viking602/go-hydaelyn/internal/core/state"
)

func registerStateUoWCommandHandlers(runtime *Runtime) {
	commandbus.Register[TransitionRunCommand](runtime.commandBus, transitionRunHandler{})
	commandbus.Register[TransitionTaskCommand](runtime.commandBus, transitionTaskHandler{})
}

type transitionRunHandler struct{}

func (transitionRunHandler) Name() string { return TransitionRunCommand{}.CommandName() }

func (transitionRunHandler) Handle(ctx context.Context, uow ports.FullUnitOfWork, cmd TransitionRunCommand) (any, error) {
	run, err := uow.Runs().LoadRun(ctx, cmd.RunID)
	if err != nil {
		return nil, err
	}
	next, err := transitionRunPure(run, cmd.To)
	if err != nil {
		return nil, err
	}
	if next.Status == run.Status {
		return nil, nil
	}
	if err := uow.Runs().SaveRun(ctx, next); err != nil {
		return nil, err
	}
	if err := uow.Events().AppendEvent(ctx, Event{RunID: next.ID, TaskID: next.RootTaskID, Type: EventRunStatusChanged, Payload: map[string]any{"from": string(run.Status), "to": string(next.Status), "run": runPayload(next)}, RecordedAt: time.Now().UTC()}); err != nil {
		return nil, err
	}
	return next, nil
}

type transitionTaskHandler struct{}

func (transitionTaskHandler) Name() string { return TransitionTaskCommand{}.CommandName() }

func (transitionTaskHandler) Handle(ctx context.Context, uow ports.FullUnitOfWork, cmd TransitionTaskCommand) (any, error) {
	task, err := uow.Tasks().LoadTask(ctx, cmd.RunID, cmd.TaskID)
	if err != nil {
		return nil, err
	}
	next, err := transitionTaskPure(task, cmd.To, true)
	if err != nil {
		return nil, err
	}
	if err := uow.Tasks().SaveTask(ctx, next); err != nil {
		return nil, err
	}
	return next, nil
}

func transitionRunPure(run Run, to RunStatus) (Run, error) {
	return corestate.TransitionRun(run, to)
}

func transitionTaskPure(task Task, to TaskStatus, bumpVersion bool) (Task, error) {
	return corestate.TransitionTask(task, to, bumpVersion)
}
