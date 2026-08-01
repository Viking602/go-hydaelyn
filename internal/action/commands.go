package action

import (
	"encoding/json"

	"github.com/Viking602/venat/internal/core/model"
)

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
	ToolResult        json.RawMessage
	RequiresReconcile bool
}

type ResolveActionAttemptCommand struct {
	AttemptID         string
	Status            model.ActionAttemptStatus
	ToolResult        json.RawMessage
	ExternalResultRef string
}

func (StartActionAttemptCommand) CommandName() string    { return "action_attempt.start" }
func (CompleteActionAttemptCommand) CommandName() string { return "action_attempt.complete" }
func (ResolveActionAttemptCommand) CommandName() string  { return "action_attempt.resolve" }
