package lifecycle

import (
	"time"

	"github.com/Viking602/go-hydaelyn/internal/core/model"
)

// NewApprovalPair creates a new ApprovalRequest and ResumeToken pair for the given task.
// newID is a factory function that generates a unique ID with a given prefix.
func NewApprovalPair(newID func(string) string, task model.Task, reason, requester string) (model.ApprovalRequest, model.ResumeToken) {
	now := time.Now().UTC()
	approval := model.ApprovalRequest{
		ApprovalID:       newID("approval"),
		RunID:            task.RunID,
		TaskID:           task.ID,
		RequesterAgentID: requester,
		Reason:           reason,
		Status:           "pending",
		ExpiresAt:        now.Add(24 * time.Hour),
	}
	token := model.ResumeToken{
		TokenID:         newID("resume"),
		RunID:           task.RunID,
		TaskID:          task.ID,
		ApprovalID:      approval.ApprovalID,
		ExpiresAt:       approval.ExpiresAt,
		ResumeCommand:   "approval.decide",
		ResumeRunState:  model.RunStatusRunning,
		ResumeTaskState: model.TaskStatusDispatched,
	}
	return approval, token
}
