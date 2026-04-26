package orchestrator

import (
	"context"
	"time"
)

func (r *Runtime) RegisterFlow(flow Flow) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if flow.BypassTaskStore ||
		flow.BypassPolicyEngine ||
		flow.BypassTaskExecutionLease ||
		flow.BypassHandoff ||
		flow.BypassResponseLayer ||
		flow.BypassOutputGateway {
		return ErrFlowBypass
	}
	r.flows[flow.Name] = flow
	return nil
}

func (r *Runtime) Replay(runID string, mode ReplayMode) (Projection, error) {
	r.mu.Lock()
	events := append([]Event{}, r.events[runID]...)
	r.mu.Unlock()
	return replayProjection(events)
}

func (r *Runtime) ReplayRunState(runID string) (Projection, error) {
	return r.Replay(runID, ReplayModeAudit)
}

func (r *Runtime) Recover(_ context.Context, runID string) (Projection, error) {
	return r.Replay(runID, ReplayModeRecovery)
}

func replayProjection(events []Event) (Projection, error) {
	if len(events) == 0 {
		return Projection{}, ErrNotFound
	}
	projection := Projection{
		Tasks: map[string]Task{},
		SideEffects: ReplaySideEffects{
			MailboxDeliveries:       0,
			UserMessagePublications: 0,
			ActionExecutions:        0,
		},
	}
	messages := map[string]UserMessage{}
	messageOrder := []string{}
	for _, event := range events {
		switch event.Type {
		case EventRunStarted:
			projection.Run = runFromPayload(event.Payload["run"])
		case EventRunStatusChanged:
			run := runFromPayload(event.Payload["run"])
			if run.ID != "" {
				projection.Run = run
			} else if to := stringFromPayload(event.Payload["to"]); to != "" {
				projection.Run.Status = RunStatus(to)
			}
		case EventTaskCreated, EventResponseTaskCreated:
			task := taskFromPayload(event.Payload)
			if task.ID != "" {
				projection.Tasks[task.ID] = task
			}
		case EventTaskDispatched:
			env := mapFromPayload(event.Payload["envelope"])
			taskID := stringFromPayload(env["taskId"])
			task := projection.Tasks[taskID]
			if task.ID == "" {
				task = Task{ID: taskID, RunID: stringFromPayload(env["runId"])}
			}
			task.Status = TaskStatusDispatched
			task.Version = intFromPayload(env["taskVersion"])
			projection.Tasks[task.ID] = task
		case EventTaskExecutionAcquired:
			taskID := event.TaskID
			task := projection.Tasks[taskID]
			if task.ID != "" {
				task.Status = TaskStatusRunning
				task.Attempts++
				projection.Tasks[task.ID] = task
			}
		case EventTaskCompleted, EventTaskFailed, EventTaskBlocked, EventTaskPaused, EventTaskOwnerChanged, EventUserMessageQueued:
			if taskPayload := mapFromPayload(event.Payload["task"]); len(taskPayload) > 0 {
				task := taskFromPayload(taskPayload)
				if task.ID != "" {
					projection.Tasks[task.ID] = task
				}
			}
			if messagePayload := mapFromPayload(event.Payload["message"]); len(messagePayload) > 0 {
				message := userMessageFromPayload(messagePayload)
				if message.ID != "" {
					if _, exists := messages[message.ID]; !exists {
						messageOrder = append(messageOrder, message.ID)
					}
					messages[message.ID] = message
				}
			}
		case EventResponsePublished:
			message := userMessageFromPayload(mapFromPayload(event.Payload["message"]))
			if message.ID != "" {
				if _, exists := messages[message.ID]; !exists {
					messageOrder = append(messageOrder, message.ID)
				}
				messages[message.ID] = message
			}
		}
	}
	if projection.Run.ID == "" {
		return Projection{}, ErrNotFound
	}
	for _, id := range messageOrder {
		projection.Messages = append(projection.Messages, messages[id])
	}
	return projection, nil
}

func runFromPayload(value any) Run {
	payload := mapFromPayload(value)
	if len(payload) == 0 {
		return Run{}
	}
	return Run{
		ID:         stringFromPayload(payload["id"]),
		Status:     RunStatus(stringFromPayload(payload["status"])),
		Request:    stringFromPayload(payload["request"]),
		RootTaskID: stringFromPayload(payload["rootTaskId"]),
		Metadata:   stringMapFromPayload(payload["metadata"]),
		CreatedAt:  timeFromPayload(payload["createdAt"]),
		UpdatedAt:  timeFromPayload(payload["updatedAt"]),
	}
}

func taskFromPayload(payload map[string]any) Task {
	if len(payload) == 0 {
		return Task{}
	}
	return Task{
		ID:              stringFromPayload(payload["taskId"]),
		RunID:           stringFromPayload(payload["runId"]),
		ParentTaskID:    stringFromPayload(payload["parentTaskId"]),
		Type:            TaskType(stringFromPayload(payload["type"])),
		Goal:            stringFromPayload(payload["goal"]),
		AssignedAgentID: stringFromPayload(payload["assignedAgentId"]),
		OwnerAgentID:    stringFromPayload(payload["ownerAgentId"]),
		OwnerComponent:  stringFromPayload(payload["ownerComponent"]),
		Status:          TaskStatus(stringFromPayload(payload["status"])),
		Version:         intFromPayload(payload["version"]),
		Attempts:        intFromPayload(payload["attempts"]),
		HandoffCount:    intFromPayload(payload["handoffCount"]),
	}
}

func userMessageFromPayload(payload map[string]any) UserMessage {
	return UserMessage{
		ID:             stringFromPayload(payload["messageId"]),
		RunID:          stringFromPayload(payload["runId"]),
		TaskID:         stringFromPayload(payload["taskId"]),
		Type:           UserMessageType(stringFromPayload(payload["type"])),
		Title:          stringFromPayload(payload["title"]),
		Payload:        stringFromPayload(payload["payload"]),
		Status:         UserMessageStatus(stringFromPayload(payload["status"])),
		IdempotencyKey: stringFromPayload(payload["idempotencyKey"]),
		CreatedAt:      timeFromPayload(payload["createdAt"]),
		UpdatedAt:      timeFromPayload(payload["updatedAt"]),
		PublishedAt:    timeFromPayload(payload["publishedAt"]),
	}
}

func mapFromPayload(value any) map[string]any {
	if value == nil {
		return nil
	}
	if payload, ok := value.(map[string]any); ok {
		return payload
	}
	return nil
}

func stringFromPayload(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func intFromPayload(value any) int {
	switch current := value.(type) {
	case int:
		return current
	case int64:
		return int(current)
	case float64:
		return int(current)
	default:
		return 0
	}
}

func timeFromPayload(value any) time.Time {
	if current, ok := value.(time.Time); ok {
		return current
	}
	return time.Time{}
}

func stringMapFromPayload(value any) map[string]string {
	raw, ok := value.(map[string]string)
	if ok {
		out := make(map[string]string, len(raw))
		for key, item := range raw {
			out[key] = item
		}
		return out
	}
	return nil
}
