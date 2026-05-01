package core

import "context"

type StartActionAttemptCommand struct {
	AttemptID      string
	ActionID       string
	RunID          string
	TaskID         string
	LeaseID        string
	HolderType     HolderType
	HolderID       string
	TaskVersion    int
	ToolName       string
	IdempotencyKey string
	InputHash      string
}

type CompleteActionAttemptCommand struct {
	RunID             string
	TaskID            string
	LeaseID           string
	HolderType        HolderType
	HolderID          string
	TaskVersion       int
	AttemptID         string
	Status            ActionAttemptStatus
	ExternalRequestID string
	ExternalResultRef string
	RequiresReconcile bool
}

func (StartActionAttemptCommand) CommandName() string    { return "action_attempt.start" }
func (CompleteActionAttemptCommand) CommandName() string { return "action_attempt.complete" }

func (r *Runtime) StartActionAttempt(ctx context.Context, cmd StartActionAttemptCommand) (ActionAttempt, error) {
	result, err := r.ExecuteCommand(ctx, cmd)
	if err != nil {
		return ActionAttempt{}, err
	}
	attempt, ok := result.(ActionAttempt)
	if !ok {
		return ActionAttempt{}, ErrInvalidCommand
	}
	return attempt, nil
}

func (r *Runtime) CompleteActionAttempt(ctx context.Context, cmd CompleteActionAttemptCommand) (ActionAttempt, error) {
	result, err := r.ExecuteCommand(ctx, cmd)
	if err != nil {
		return ActionAttempt{}, err
	}
	attempt, ok := result.(ActionAttempt)
	if !ok {
		return ActionAttempt{}, ErrInvalidCommand
	}
	return attempt, nil
}
