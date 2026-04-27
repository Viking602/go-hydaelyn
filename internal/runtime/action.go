package runtime

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
	r.mu.Lock()
	defer r.mu.Unlock()
	_, task, err := r.validateSubmissionLocked(cmd.RunID, cmd.TaskID, cmd.LeaseID, cmd.HolderType, cmd.HolderID, cmd.TaskVersion)
	if err != nil {
		return ActionAttempt{}, err
	}
	if task.Type != TaskTypeAction {
		return ActionAttempt{}, ErrActionTaskRequired
	}
	attempt := ActionAttempt{
		AttemptID:      firstNonEmpty(cmd.AttemptID, r.newID("attempt")),
		ActionID:       cmd.ActionID,
		RunID:          cmd.RunID,
		TaskID:         cmd.TaskID,
		ToolName:       cmd.ToolName,
		Status:         ActionAttemptRunning,
		IdempotencyKey: cmd.IdempotencyKey,
		InputHash:      cmd.InputHash,
	}
	if _, err := r.authorizeLocked(ctx, PolicyRequest{
		Operation: PolicyOperationAction,
		RunID:     cmd.RunID,
		TaskID:    cmd.TaskID,
		Actor:     actorFromHolder(cmd.HolderType, cmd.HolderID),
		Action:    &attempt,
	}); err != nil {
		return ActionAttempt{}, err
	}
	r.actionAttempts[attempt.AttemptID] = attempt
	r.appendEventLocked(cmd.RunID, cmd.TaskID, EventActionAttemptStarted, map[string]any{
		"attemptId":      attempt.AttemptID,
		"actionId":       attempt.ActionID,
		"toolName":       attempt.ToolName,
		"idempotencyKey": attempt.IdempotencyKey,
	})
	return attempt, nil
}

func (r *Runtime) CompleteActionAttempt(_ context.Context, cmd CompleteActionAttemptCommand) (ActionAttempt, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if cmd.LeaseID == "" {
		return ActionAttempt{}, ErrLeaseNotActive
	}
	if _, task, err := r.validateSubmissionLocked(cmd.RunID, cmd.TaskID, cmd.LeaseID, cmd.HolderType, cmd.HolderID, cmd.TaskVersion); err != nil {
		return ActionAttempt{}, err
	} else if task.Type != TaskTypeAction {
		return ActionAttempt{}, ErrActionTaskRequired
	}
	attempt, ok := r.actionAttempts[cmd.AttemptID]
	if !ok || attempt.RunID != cmd.RunID || attempt.TaskID != cmd.TaskID {
		return ActionAttempt{}, ErrNotFound
	}
	attempt.Status = cmd.Status
	attempt.ExternalRequestID = cmd.ExternalRequestID
	attempt.ExternalResultRef = cmd.ExternalResultRef
	attempt.RequiresReconcile = cmd.RequiresReconcile || cmd.Status == ActionAttemptUnknown
	r.actionAttempts[attempt.AttemptID] = attempt
	r.appendEventLocked(cmd.RunID, cmd.TaskID, EventActionAttemptUpdated, map[string]any{
		"attemptId":         attempt.AttemptID,
		"status":            string(attempt.Status),
		"externalRequestId": attempt.ExternalRequestID,
		"externalResultRef": attempt.ExternalResultRef,
		"requiresReconcile": attempt.RequiresReconcile,
	})
	if attempt.RequiresReconcile {
		task := r.tasks[cmd.RunID][cmd.TaskID]
		var err error
		task, err = r.transitionTaskLocked(task, TaskStatusReconcileRequired)
		if err != nil {
			return ActionAttempt{}, err
		}
		task.Error = "action attempt requires reconciliation"
		r.saveTaskLocked(task)
		r.releaseLeaseLocked(cmd.LeaseID)
		if run, ok := r.runs[cmd.RunID]; ok {
			if _, err := r.transitionRunLocked(run, RunStatusReconcileRequired); err != nil {
				return ActionAttempt{}, err
			}
		}
		r.appendEventLocked(cmd.RunID, cmd.TaskID, EventActionReconcileRequired, map[string]any{
			"attemptId": attempt.AttemptID,
			"status":    string(attempt.Status),
		})
	}
	return attempt, nil
}
