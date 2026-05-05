package core

import (
	"github.com/Viking602/go-hydaelyn/internal/core/model"
	"github.com/Viking602/go-hydaelyn/internal/projection"
)

func replayProjection(events []model.Event) (model.Projection, error) {
	return projection.Project(events)
}
