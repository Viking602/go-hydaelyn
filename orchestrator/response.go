package orchestrator

import (
	"context"
	"regexp"
	"strings"
	"time"
)

type SubmitResponseOutputCommand struct {
	RunID          string
	TaskID         string
	LeaseID        string
	HolderType     HolderType
	HolderID       string
	TaskVersion    int
	Type           UserMessageType
	Title          string
	Payload        string
	IdempotencyKey string
}

type PublishResponseCommand struct {
	RunID     string
	MessageID string
}

func (r *Runtime) SubmitResponseOutput(_ context.Context, cmd SubmitResponseOutputCommand) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	task, ok := r.tasks[cmd.RunID][cmd.TaskID]
	if !ok {
		return ErrNotFound
	}
	if task.Type != TaskTypeResponse {
		return ErrResponseTaskRequired
	}
	if _, _, err := r.validateSubmissionLocked(cmd.RunID, cmd.TaskID, cmd.LeaseID, cmd.HolderType, cmd.HolderID, cmd.TaskVersion); err != nil {
		return err
	}
	now := time.Now().UTC()
	message := UserMessage{
		ID:             r.newID("msg"),
		RunID:          cmd.RunID,
		TaskID:         cmd.TaskID,
		Type:           cmd.Type,
		Title:          cmd.Title,
		Payload:        cmd.Payload,
		Status:         UserMessageComposed,
		IdempotencyKey: cmd.IdempotencyKey,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if message.Type == "" {
		message.Type = UserMessageTypeFinalAnswer
	}
	if message.IdempotencyKey == "" {
		message.IdempotencyKey = cmd.RunID + ":" + cmd.TaskID + ":" + string(message.Type)
	}
	decision := PolicyDecision{Effect: PolicyEffectAllow}
	if r.messagePolicy != nil {
		decision = r.messagePolicy(message)
		if decision.Effect == "" {
			decision.Effect = PolicyEffectAllow
		}
	}
	if decision.Effect == PolicyEffectDeny || decision.Effect == PolicyEffectAbort {
		return ErrPolicyDenied
	}
	sanitized, err := r.applyResponseObligationsLocked(message, decision)
	if err != nil {
		return err
	}
	r.appendEventLocked(cmd.RunID, cmd.TaskID, EventUserMessageComposed, map[string]any{
		"message": userMessagePayload(sanitized),
	})
	r.appendEventLocked(cmd.RunID, cmd.TaskID, EventUserMessagePolicyChecked, map[string]any{
		"decisionId": decision.DecisionID,
		"effect":     string(decision.Effect),
	})
	sanitized.Status = UserMessageQueued
	sanitized.UpdatedAt = time.Now().UTC()
	r.messages[sanitized.ID] = sanitized
	r.messagesByRun[cmd.RunID] = append(r.messagesByRun[cmd.RunID], sanitized.ID)
	task.Status = TaskStatusCompleted
	task.Version++
	task.Result = &TypedReport{Status: ReportStatusSuccess, Summary: "response queued"}
	r.saveTaskLocked(task)
	r.releaseLeaseLocked(cmd.LeaseID)
	r.appendEventLocked(cmd.RunID, cmd.TaskID, EventUserMessageQueued, map[string]any{
		"messageId": sanitized.ID,
		"message":   userMessagePayload(sanitized),
		"task":      taskEventPayload(task),
	})
	return nil
}

func (r *Runtime) PublishResponse(_ context.Context, cmd PublishResponseCommand) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	message, ok := r.messages[cmd.MessageID]
	if !ok || message.RunID != cmd.RunID {
		return ErrNotFound
	}
	if message.Status == UserMessagePublished {
		return nil
	}
	if message.Status != UserMessageQueued {
		return ErrInvalidCommand
	}
	message.Status = UserMessagePublished
	message.PublishedAt = time.Now().UTC()
	message.UpdatedAt = time.Now().UTC()
	r.messages[message.ID] = message
	r.appendEventLocked(cmd.RunID, message.TaskID, EventResponsePublished, map[string]any{
		"messageId": message.ID,
		"message":   userMessagePayload(message),
	})
	return nil
}

func (r *Runtime) ResponseOutbox(runID string) []UserMessage {
	r.mu.Lock()
	defer r.mu.Unlock()
	ids := r.messagesByRun[runID]
	out := make([]UserMessage, 0, len(ids))
	for _, id := range ids {
		out = append(out, r.messages[id])
	}
	return out
}

func (r *Runtime) queueSystemResponseLocked(runID, sourceTaskID string, messageType UserMessageType, title, payload string) UserMessage {
	task := Task{
		ID:             r.newID("response"),
		RunID:          runID,
		ParentTaskID:   sourceTaskID,
		Type:           TaskTypeResponse,
		Goal:           string(messageType),
		OwnerComponent: "response_composer",
		Status:         TaskStatusCompleted,
		Version:        1,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
		Result:         &TypedReport{Status: ReportStatusSuccess, Summary: payload},
	}
	if r.tasks[runID] == nil {
		r.tasks[runID] = map[string]Task{}
	}
	r.tasks[runID][task.ID] = task
	r.appendEventLocked(runID, task.ID, EventResponseTaskCreated, taskEventPayload(task))
	now := time.Now().UTC()
	message := UserMessage{
		ID:             r.newID("msg"),
		RunID:          runID,
		TaskID:         task.ID,
		Type:           messageType,
		Title:          title,
		Payload:        redactUserPayload(payload),
		Status:         UserMessageQueued,
		IdempotencyKey: runID + ":" + sourceTaskID + ":" + string(messageType),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	r.messages[message.ID] = message
	r.messagesByRun[runID] = append(r.messagesByRun[runID], message.ID)
	r.appendEventLocked(runID, task.ID, EventUserMessageQueued, map[string]any{
		"messageId": message.ID,
		"message":   userMessagePayload(message),
		"task":      taskEventPayload(task),
	})
	return message
}

func userMessagePayload(message UserMessage) map[string]any {
	return map[string]any{
		"messageId":      message.ID,
		"runId":          message.RunID,
		"taskId":         message.TaskID,
		"type":           string(message.Type),
		"title":          message.Title,
		"payload":        message.Payload,
		"status":         string(message.Status),
		"idempotencyKey": message.IdempotencyKey,
		"createdAt":      message.CreatedAt,
		"updatedAt":      message.UpdatedAt,
		"publishedAt":    message.PublishedAt,
	}
}

func (r *Runtime) applyResponseObligationsLocked(message UserMessage, decision PolicyDecision) (UserMessage, error) {
	out := message
	for _, obligation := range decision.Obligations {
		switch obligation.Kind {
		case ObligationRedactFields:
			out.Payload = redactUserPayload(out.Payload)
		case ObligationHideInternalTrace:
			out.Payload = hideInternalTrace(out.Payload)
		case ObligationMaskToolOutput:
			out.Payload = strings.ReplaceAll(out.Payload, "tool output:", "tool output: [masked]")
		case ObligationSelectorOnly, ObligationRequireHumanApproval, ObligationRestrictHandoffContext:
			// These obligations are enforceable at other runtime boundaries.
		default:
			r.appendEventLocked(message.RunID, message.TaskID, EventPolicyObligationFailed, map[string]any{
				"decisionId":      decision.DecisionID,
				"obligation":      string(obligation.Kind),
				"target":          obligation.Target,
				"reason":          "unsupported obligation",
				"effectiveEffect": string(PolicyEffectDeny),
			})
			return UserMessage{}, ErrPolicyObligationFailed
		}
	}
	if containsString(decision.Redactions, "email") {
		out.Payload = redactEmail(out.Payload)
	}
	return out, nil
}

var emailRE = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)

func redactUserPayload(payload string) string {
	return redactEmail(payload)
}

func redactEmail(payload string) string {
	return emailRE.ReplaceAllString(payload, "[redacted-email]")
}

func hideInternalTrace(payload string) string {
	lines := strings.Split(payload, "\n")
	kept := lines[:0]
	for _, line := range lines {
		if strings.Contains(strings.ToLower(line), "internal trace") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}
