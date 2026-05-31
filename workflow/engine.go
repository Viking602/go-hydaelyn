package workflow

import (
	"context"
	"errors"

	"github.com/Viking602/go-hydaelyn/multiagent"
)

var (
	ErrExecutorMissing = errors.New("workflow: executor missing")
	ErrWorkflowMissing = errors.New("workflow: compiled workflow missing")
)

type Engine struct {
	Executor multiagent.Executor
	Options  multiagent.DriveOptions
}

type StartRequest struct {
	RunID    string
	Workflow Compiled
}

type Run struct {
	RunID  string
	Result multiagent.DriveResult
}

func (e Engine) Start(ctx context.Context, req StartRequest) (Run, error) {
	if e.Executor == nil {
		return Run{}, ErrExecutorMissing
	}
	if req.Workflow.graph == nil {
		return Run{}, ErrWorkflowMissing
	}
	result, err := multiagent.Drive(ctx, req.RunID, req.Workflow.Scheduler(), e.Executor, e.Options)
	if err != nil {
		return Run{RunID: req.RunID, Result: result}, err
	}
	return Run{RunID: req.RunID, Result: result}, nil
}
