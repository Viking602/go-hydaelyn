package core

import (
	"github.com/Viking602/venat/internal/core/model"
	"github.com/Viking602/venat/internal/projection"
)

func replayProjection(events []model.Event) (model.Projection, error) {
	return projection.Project(events)
}
