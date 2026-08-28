package projection

import (
	"encoding/json"
	"maps"
	"time"

	"github.com/Viking602/venat/api"
)

func runFromPayload(value any) api.Run {
	payload := mapFromPayload(value)
	if len(payload) == 0 {
		return api.Run{}
	}
	return api.Run{
		ID:           stringFromPayload(payload["id"]),
		Status:       api.RunStatus(stringFromPayload(payload["status"])),
		Request:      stringFromPayload(payload["request"]),
		RootTaskID:   stringFromPayload(payload["rootTaskId"]),
		AgentVersion: stringFromPayload(payload["agentVersion"]),
		Metadata:     stringMapFromPayload(payload["metadata"]),
		CreatedAt:    timeFromPayload(payload["createdAt"]),
		UpdatedAt:    timeFromPayload(payload["updatedAt"]),
	}
}

func taskFromPayload(payload map[string]any) api.Task {
	if len(payload) == 0 {
		return api.Task{}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return api.Task{}
	}
	var task api.Task
	if err := json.Unmarshal(raw, &task); err != nil {
		return api.Task{}
	}
	return task
}

func userMessageFromPayload(payload map[string]any) api.UserMessage {
	return api.UserMessage{
		ID:             stringFromPayload(payload["messageId"]),
		RunID:          stringFromPayload(payload["runId"]),
		TaskID:         stringFromPayload(payload["taskId"]),
		Type:           api.UserMessageType(stringFromPayload(payload["type"])),
		Title:          stringFromPayload(payload["title"]),
		Payload:        stringFromPayload(payload["payload"]),
		Status:         api.UserMessageStatus(stringFromPayload(payload["status"])),
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

// MapFromPayload and StringFromPayload are the exported forms of
// mapFromPayload/stringFromPayload, shared with package core to avoid
// duplicating this decoding logic (internal package sharing, not public API).
func MapFromPayload(value any) map[string]any {
	return mapFromPayload(value)
}

func StringFromPayload(value any) string {
	return stringFromPayload(value)
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
