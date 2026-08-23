package lifecycle

import (
	"time"

	"github.com/Viking602/venat/api"
)

// NewApprovalPair creates a new ApprovalRequest and ResumeToken pair for the given task.
// newID is a factory function that generates a unique ID with a given prefix.
func NewApprovalPair(newID func(string) string, task api.Task, reason, requester string) (api.ApprovalRequest, api.ResumeToken) {
	now := time.Now().UTC()
	approval := api.ApprovalRequest{
		ApprovalID:       newID("approval"),
		RunID:            task.RunID,
		TaskID:           task.ID,
		RequesterAgentID: requester,
		Reason:           reason,
		Status:           "pending",
		ExpiresAt:        now.Add(24 * time.Hour),
	}
	token := api.ResumeToken{
		TokenID:         newID("resume"),
		RunID:           task.RunID,
		TaskID:          task.ID,
		ApprovalID:      approval.ApprovalID,
		ExpiresAt:       approval.ExpiresAt,
		ResumeCommand:   "approval.decide",
		ResumeRunState:  api.RunStatusRunning,
		ResumeTaskState: api.TaskStatusDispatched,
	}
	return approval, token
}
