package projection

import (
	"maps"
	"time"

	"github.com/Viking602/go-hydaelyn/internal/core/model"
)

func runFromPayload(value any) model.Run {
	payload := mapFromPayload(value)
	if len(payload) == 0 {
		return model.Run{}
	}
	return model.Run{
		ID:         stringFromPayload(payload["id"]),
		Status:     model.RunStatus(stringFromPayload(payload["status"])),
		Request:    stringFromPayload(payload["request"]),
		RootTaskID: stringFromPayload(payload["rootTaskId"]),
		Metadata:   stringMapFromPayload(payload["metadata"]),
		CreatedAt:  timeFromPayload(payload["createdAt"]),
		UpdatedAt:  timeFromPayload(payload["updatedAt"]),
	}
}

func taskFromPayload(payload map[string]any) model.Task {
	if len(payload) == 0 {
		return model.Task{}
	}
	return model.Task{
		ID:              stringFromPayload(payload["taskId"]),
		RunID:           stringFromPayload(payload["runId"]),
		ParentTaskID:    stringFromPayload(payload["parentTaskId"]),
		Type:            model.TaskType(stringFromPayload(payload["type"])),
		Goal:            stringFromPayload(payload["goal"]),
		AssignedAgentID: stringFromPayload(payload["assignedAgentId"]),
		OwnerAgentID:    stringFromPayload(payload["ownerAgentId"]),
		OwnerComponent:  stringFromPayload(payload["ownerComponent"]),
		Status:          model.TaskStatus(stringFromPayload(payload["status"])),
		Version:         intFromPayload(payload["version"]),
		Attempts:        intFromPayload(payload["attempts"]),
		HandoffCount:    intFromPayload(payload["handoffCount"]),
		CreatedAt:       timeFromPayload(payload["createdAt"]),
		UpdatedAt:       timeFromPayload(payload["updatedAt"]),
	}
}

func userMessageFromPayload(payload map[string]any) model.UserMessage {
	return model.UserMessage{
		ID:             stringFromPayload(payload["messageId"]),
		RunID:          stringFromPayload(payload["runId"]),
		TaskID:         stringFromPayload(payload["taskId"]),
		Type:           model.UserMessageType(stringFromPayload(payload["type"])),
		Title:          stringFromPayload(payload["title"]),
		Payload:        stringFromPayload(payload["payload"]),
		Status:         model.UserMessageStatus(stringFromPayload(payload["status"])),
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
	if raw, ok := value.(map[string]string); ok {
		return maps.Clone(raw)
	}
	return nil
}
