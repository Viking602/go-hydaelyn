package ports

import (
	"context"

	"github.com/Viking602/venat/internal/core/model"
)

type Projector interface {
	Project(context.Context, []model.Event) (model.Projection, error)
}
