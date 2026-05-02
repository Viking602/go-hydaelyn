package response

import "github.com/Viking602/go-hydaelyn/internal/core/model"

type SubmitOutputCommand struct {
	RunID          string
	TaskID         string
	LeaseID        string
	HolderType     model.HolderType
	HolderID       string
	TaskVersion    int
	Type           model.UserMessageType
	Title          string
	Payload        string
	IdempotencyKey string
}

type PublishCommand struct {
	RunID     string
	MessageID string
}

func (SubmitOutputCommand) CommandName() string { return "response.submit_output" }
func (PublishCommand) CommandName() string      { return "response.publish" }
