package runtime

import (
	"context"
	"time"
)

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
	r.appendEventLocked(task.RunID, task.ID, EventHandoffRequested, map[string]any{
		"fromAgentId": fromAgentID,
		"toAgentId":   request.ToAgentID,
		"reason":      request.Reason,
	})
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
	task.OwnerAgentID = request.ToAgentID
	task.OwnerComponent = ""
	task.HandoffCount++
	task.OwnerHistory = append(task.OwnerHistory, request.ToAgentID)
	next, err := r.transitionTaskLocked(*task, TaskStatusDispatched)
	if err != nil {
		return err
	}
	*task = r.saveTaskLocked(next)
	r.appendEventLocked(task.RunID, task.ID, EventTaskOwnerChanged, map[string]any{
		"ownerAgentId": request.ToAgentID,
		"version":      task.Version,
		"task":         taskEventPayload(*task),
	})
	r.appendEventLocked(task.RunID, task.ID, EventHandoffApplied, map[string]any{
		"fromAgentId": fromAgentID,
		"toAgentId":   request.ToAgentID,
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
	r.writeHandoffEnvelopeLocked(env)
	return nil
}

func (r *Runtime) writeHandoffEnvelopeLocked(env TaskEnvelope) TaskEnvelope {
	if env.ID == "" {
		env.ID = r.newID("env")
	}
	if env.CreatedAt.IsZero() {
		env.CreatedAt = time.Now().UTC()
	}
	if env.Status == "" {
		env.Status = "pending"
	}
	if env.Type == "" {
		env.Type = "HandoffEnvelope"
	}
	if env.TaskVersion == 0 {
		if task, ok := r.tasks[env.RunID][env.TaskID]; ok {
			env.TaskVersion = task.Version
		}
	}
	r.envelopes[env.ID] = env
	r.envelopesByRun[env.RunID] = append(r.envelopesByRun[env.RunID], env.ID)
	r.appendEventLocked(env.RunID, env.TaskID, EventHandoffEnvelopeQueued, map[string]any{
		"envelope": envPayload(env),
	})
	return env
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
