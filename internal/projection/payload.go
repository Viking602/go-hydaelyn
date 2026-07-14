package projection

import (
	"encoding/json"
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
	raw, err := json.Marshal(payload)
	if err != nil {
		return model.Task{}
	}
	var task model.Task
	if err := json.Unmarshal(raw, &task); err != nil {
		return model.Task{}
	}
	return task
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
	switch current := value.(type) {
	case time.Time:
		return current
	case string:
		parsed, _ := time.Parse(time.RFC3339Nano, current)
		return parsed
	}
	return time.Time{}
}

func stringMapFromPayload(value any) map[string]string {
	if raw, ok := value.(map[string]string); ok {
		return maps.Clone(raw)
	}
	if raw, ok := value.(map[string]any); ok {
		out := make(map[string]string, len(raw))
		for key, value := range raw {
			if text, ok := value.(string); ok {
				out[key] = text
			}
		}
		return out
	}
	return nil
}
