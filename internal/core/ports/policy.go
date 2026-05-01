package ports

import (
	"context"

	"github.com/Viking602/go-hydaelyn/internal/core/model"
)

type PolicyEngine interface {
	Authorize(context.Context, model.PolicyRequest) (model.PolicyDecision, error)
}
