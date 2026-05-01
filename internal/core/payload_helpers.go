package core

// payload_helpers.go re-exports the projection-internal payload helpers
// that are still needed from package core (e.g., in tests and event writers).

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
