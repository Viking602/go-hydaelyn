package core

import (
	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/internal/projection"
)

func replayProjection(events []api.Event) (api.Projection, error) {
	return projection.Project(events)
}
