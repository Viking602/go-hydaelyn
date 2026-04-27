package orchestrator

import "context"

type HandoffCommand struct {
	RunID          string
	TaskID         string
	FromAgentID    string
	ToAgentID      string
	TaskVersion    int
	HandoffContext string
}

func (r *Runtime) RequestHandoff(ctx context.Context, cmd HandoffCommand) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if run, ok := r.runs[cmd.RunID]; !ok {
		return ErrNotFound
	} else if isTerminalRun(run.Status) {
		return ErrTerminalState
	}
	task, ok := r.tasks[cmd.RunID][cmd.TaskID]
	if !ok {
		return ErrNotFound
	}
	request := &HandoffRequest{
		RunID:          cmd.RunID,
		TaskID:         cmd.TaskID,
		FromAgentID:    cmd.FromAgentID,
		ToAgentID:      cmd.ToAgentID,
		ContextSummary: cmd.HandoffContext,
		TaskVersion:    cmd.TaskVersion,
	}
	if _, err := r.authorizeLocked(ctx, PolicyRequest{
		Operation: PolicyOperationHandoff,
		RunID:     cmd.RunID,
		TaskID:    cmd.TaskID,
		Actor:     SourceIdentity{Type: SourceAgent, ID: cmd.FromAgentID},
		Handoff:   request,
	}); err != nil {
		return err
	}
	r.recordTraceLocked(cmd.RunID, cmd.TaskID, "handoff.request", "handoff")
	return r.applyHandoffLocked(&task, request, cmd.HandoffContext)
}

func (r *Runtime) applyHandoffLocked(task *Task, request *HandoffRequest, fallbackContext string) error {
	if isTerminalTask(task.Status) {
		return ErrTerminalState
	}
	if request.TaskVersion != 0 && request.TaskVersion != task.Version {
		return ErrStaleTaskVersion
	}
	fromAgentID := request.FromAgentID
	if fromAgentID == "" {
		fromAgentID = task.OwnerAgentID
	}
	if task.OwnerAgentID != fromAgentID {
		return ErrOwnerMismatch
	}
	if request.ToAgentID == "" {
		return ErrInvalidCommand
	}
	if task.HandoffCount >= maxHandoffDepth {
		return ErrHandoffDepthExceeded
	}
	if containsString(task.OwnerHistory, request.ToAgentID) {
		return ErrHandoffCycle
	}
	contextSummary := request.ContextSummary
	if contextSummary == "" {
		contextSummary = fallbackContext
	}
	if contextSummary != "" {
		r.writeBlackboardLocked(BlackboardItem{
			RunID:      task.RunID,
			TaskID:     task.ID,
			Type:       BlackboardItemHandoffContext,
			Source:     SourceIdentity{Type: SourceAgent, ID: fromAgentID},
			Visibility: BlackboardVisibilityAgentVisible,
			Key:        "handoff_context",
			Content:    contextSummary,
			Payload:    contextSummary,
			Version:    task.Version,
		})
	}
	r.appendEventLocked(task.RunID, task.ID, EventHandoffRequested, map[string]any{
		"fromAgentId": fromAgentID,
		"toAgentId":   request.ToAgentID,
		"reason":      request.Reason,
	})
	r.appendEventLocked(task.RunID, task.ID, EventHandoffApplied, map[string]any{
		"fromAgentId": fromAgentID,
		"toAgentId":   request.ToAgentID,
	})
	task.OwnerAgentID = request.ToAgentID
	task.OwnerComponent = ""
	task.Status = TaskStatusDispatched
	task.Version++
	task.HandoffCount++
	task.OwnerHistory = append(task.OwnerHistory, request.ToAgentID)
	*task = r.saveTaskLocked(*task)
	r.appendEventLocked(task.RunID, task.ID, EventTaskOwnerChanged, map[string]any{
		"ownerAgentId": request.ToAgentID,
		"version":      task.Version,
		"task":         taskEventPayload(*task),
	})
	env := TaskEnvelope{
		ID:            r.newID("env"),
		RunID:         task.RunID,
		TaskID:        task.ID,
		TargetAgentID: request.ToAgentID,
		Type:          "HandoffEnvelope",
		Status:        "pending",
		TaskVersion:   task.Version,
		Payload: map[string]any{
			"handoff": true,
			"reason":  request.Reason,
		},
		CreatedAt: task.UpdatedAt,
	}
	r.writeEnvelopeLocked(env)
	return nil
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
