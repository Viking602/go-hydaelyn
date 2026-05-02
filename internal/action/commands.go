package action

import "github.com/Viking602/go-hydaelyn/internal/core/model"

type StartActionAttemptCommand struct {
	AttemptID      string
	ActionID       string
	RunID          string
	TaskID         string
	LeaseID        string
	HolderType     model.HolderType
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
	HolderType        model.HolderType
	HolderID          string
	TaskVersion       int
	AttemptID         string
	Status            model.ActionAttemptStatus
	ExternalRequestID string
	ExternalResultRef string
	RequiresReconcile bool
}

func (StartActionAttemptCommand) CommandName() string    { return "action_attempt.start" }
func (CompleteActionAttemptCommand) CommandName() string { return "action_attempt.complete" }
