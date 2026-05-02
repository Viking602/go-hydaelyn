package core

import projection "github.com/Viking602/go-hydaelyn/internal/projection"

func replayProjection(events []Event) (Projection, error) {
	return projection.Project(events)
}
