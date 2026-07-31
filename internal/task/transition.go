package task

import (
	"context"
	"time"

	"github.com/Viking602/venat/internal/core/model"
	"github.com/Viking602/venat/internal/core/ports"
	corestate "github.com/Viking602/venat/internal/core/state"
	"github.com/Viking602/venat/internal/eventpayload"
)

func TransitionRun(ctx context.Context, uow ports.UnitOfWork, runID string, to model.RunStatus) (model.Run, bool, error) {
	run, err := uow.Runs().LoadRun(ctx, runID)
	if err != nil {
		return model.Run{}, false, err
	}
	next, err := corestate.TransitionRun(run, to)
	if err != nil {
		return model.Run{}, false, err
	}
	if next.Status == run.Status {
		return next, false, nil
	}
	if err := uow.Runs().SaveRun(ctx, next); err != nil {
		return model.Run{}, false, err
	}
	if err := uow.Events().AppendEvent(ctx, model.Event{RunID: next.ID, TaskID: next.RootTaskID, Type: model.EventRunStatusChanged, Payload: map[string]any{"from": string(run.Status), "to": string(next.Status), "run": eventpayload.Run(next)}, RecordedAt: time.Now().UTC()}); err != nil {
		return model.Run{}, false, err
	}
	return next, true, nil
}

func TransitionTask(ctx context.Context, uow ports.UnitOfWork, runID, taskID string, to model.TaskStatus, bumpVersion bool) (model.Task, error) {
	task, err := uow.Tasks().LoadTask(ctx, runID, taskID)
	if err != nil {
		return model.Task{}, err
	}
	next, err := corestate.TransitionTask(task, to, bumpVersion)
	if err != nil {
		return model.Task{}, err
	}
	if err := uow.Tasks().SaveTask(ctx, next); err != nil {
		return model.Task{}, err
	}
	return next, nil
}

func PureRunTransition(run model.Run, to model.RunStatus) (model.Run, error) {
	return corestate.TransitionRun(run, to)
}

func PureTaskTransition(task model.Task, to model.TaskStatus, bumpVersion bool) (model.Task, error) {
	return corestate.TransitionTask(task, to, bumpVersion)
}
