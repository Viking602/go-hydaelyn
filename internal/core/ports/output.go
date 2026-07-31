package ports

import (
	"context"

	"github.com/Viking602/venat/internal/core/model"
)

type OutputGateway interface {
	Publish(context.Context, model.UserMessage) error
}
