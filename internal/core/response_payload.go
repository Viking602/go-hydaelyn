package core

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
