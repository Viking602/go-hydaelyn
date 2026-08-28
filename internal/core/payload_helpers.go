package core

import "github.com/Viking602/venat/internal/projection"

// payload_helpers.go re-exports the projection-internal payload helpers
// that are still needed from package core (e.g., in tests and event writers).

func mapFromPayload(value any) map[string]any {
	return projection.MapFromPayload(value)
}

func stringFromPayload(value any) string {
	return projection.StringFromPayload(value)
}
