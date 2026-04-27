package runtime

import (
	"context"
	"time"
)

type RequestApprovalCommand struct {
	RunID            string
	TaskID           string
	ActionID         string
	RequesterAgentID string
	Reason           string
	RiskSummary      string
	RequestedAction  string
}

type DecideApprovalCommand struct {
	RunID      string
	ApprovalID string
	DecidedBy  string
	Decision   string
	Reason     string
}

type RecoverResumeTokenCommand struct {
	TokenID string
}

func (RequestApprovalCommand) CommandName() string    { return "approval.request" }
func (DecideApprovalCommand) CommandName() string     { return "approval.decide" }
func (RecoverResumeTokenCommand) CommandName() string { return "resume_token.recover" }

func (r *Runtime) RequestApproval(_ context.Context, cmd RequestApprovalCommand) (ApprovalRequest, ResumeToken, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	task, ok := r.tasks[cmd.RunID][cmd.TaskID]
	if !ok {
		return ApprovalRequest{}, ResumeToken{}, ErrNotFound
	}
	approval, token := r.createApprovalLocked(task, cmd.Reason, cmd.RequesterAgentID)
	approval.ActionID = cmd.ActionID
	approval.RiskSummary = cmd.RiskSummary
	approval.RequestedAction = cmd.RequestedAction
	r.approvals[approval.ApprovalID] = approval
	r.appendEventLocked(cmd.RunID, cmd.TaskID, EventApprovalRequested, map[string]any{
		"approvalId":  approval.ApprovalID,
		"resumeToken": token.TokenID,
		"reason":      approval.Reason,
	})
	return approval, token, nil
}

func (r *Runtime) DecideApproval(_ context.Context, cmd DecideApprovalCommand) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	approval, ok := r.approvals[cmd.ApprovalID]
	if !ok || approval.RunID != cmd.RunID {
		return ErrNotFound
	}
	approval.Status = cmd.Decision
	r.approvals[approval.ApprovalID] = approval
	r.appendEventLocked(approval.RunID, approval.TaskID, EventApprovalDecided, map[string]any{
		"approvalId": approval.ApprovalID,
		"decidedBy":  cmd.DecidedBy,
		"decision":   cmd.Decision,
		"reason":     cmd.Reason,
	})
	if task, ok := r.tasks[approval.RunID][approval.TaskID]; ok && task.Status == TaskStatusPaused && cmd.Decision == "approved" {
		if _, err := r.transitionTaskLocked(task, TaskStatusDispatched); err != nil {
			return err
		}
	}
	if run, ok := r.runs[approval.RunID]; ok && run.Status == RunStatusWaitingApproval && cmd.Decision == "approved" {
		if _, err := r.transitionRunLocked(run, RunStatusRunning); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runtime) RecoverResumeToken(_ context.Context, cmd RecoverResumeTokenCommand) (ResumeToken, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	token, ok := r.resumeTokens[cmd.TokenID]
	if !ok {
		return ResumeToken{}, ErrNotFound
	}
	if !token.ExpiresAt.IsZero() && token.ExpiresAt.Before(time.Now().UTC()) {
		return ResumeToken{}, ErrInvalidCommand
	}
	return token, nil
}

func (r *Runtime) createApprovalLocked(task Task, reason, requester string) (ApprovalRequest, ResumeToken) {
	now := time.Now().UTC()
	approval := ApprovalRequest{
		ApprovalID:       r.newID("approval"),
		RunID:            task.RunID,
		TaskID:           task.ID,
		RequesterAgentID: requester,
		Reason:           reason,
		Status:           "pending",
		ExpiresAt:        now.Add(24 * time.Hour),
	}
	token := ResumeToken{
		TokenID:         r.newID("resume"),
		RunID:           task.RunID,
		TaskID:          task.ID,
		ApprovalID:      approval.ApprovalID,
		ExpiresAt:       approval.ExpiresAt,
		ResumeCommand:   "approval.decide",
		ResumeRunState:  RunStatusRunning,
		ResumeTaskState: TaskStatusDispatched,
	}
	r.approvals[approval.ApprovalID] = approval
	r.resumeTokens[token.TokenID] = token
	r.appendEventLocked(task.RunID, task.ID, EventResumeTokenCreated, map[string]any{
		"tokenId":    token.TokenID,
		"approvalId": approval.ApprovalID,
		"expiresAt":  token.ExpiresAt,
	})
	return approval, token
}
