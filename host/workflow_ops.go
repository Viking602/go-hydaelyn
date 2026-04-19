package host

import (
	"context"
	"fmt"

	"github.com/Viking602/go-hydaelyn/workflow"
)

func (r *Runtime) StartWorkflow(ctx context.Context, name string, input map[string]any) (workflow.State, error) {
	driver, ok := r.workflows.Driver(name)
	if !ok {
		return workflow.State{}, fmt.Errorf("workflow not found: %s", name)
	}
	state, err := driver.Start(ctx, input)
	if err != nil {
		return workflow.State{}, err
	}
	if err := r.storage.Workflows().Save(ctx, state); err != nil {
		return workflow.State{}, err
	}
	return state, nil
}

func (r *Runtime) ResumeWorkflow(ctx context.Context, workflowID string) (workflow.State, error) {
	current, err := r.storage.Workflows().Load(ctx, workflowID)
	if err != nil {
		return workflow.State{}, err
	}
	driver, ok := r.workflows.Driver(current.Name)
	if !ok {
		return workflow.State{}, fmt.Errorf("workflow not found: %s", current.Name)
	}
	next, err := driver.Resume(ctx, current)
	if err != nil {
		return workflow.State{}, err
	}
	if err := r.storage.Workflows().Save(ctx, next); err != nil {
		return workflow.State{}, err
	}
	return next, nil
}

func (r *Runtime) AbortWorkflow(ctx context.Context, workflowID string) (workflow.State, error) {
	current, err := r.storage.Workflows().Load(ctx, workflowID)
	if err != nil {
		return workflow.State{}, err
	}
	driver, ok := r.workflows.Driver(current.Name)
	if !ok {
		return workflow.State{}, fmt.Errorf("workflow not found: %s", current.Name)
	}
	next, err := driver.Abort(ctx, current)
	if err != nil {
		return workflow.State{}, err
	}
	if err := r.storage.Workflows().Save(ctx, next); err != nil {
		return workflow.State{}, err
	}
	return next, nil
}
