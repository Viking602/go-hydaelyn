package api

import "context"

type PolicyEngine interface {
	Authorize(context.Context, PolicyRequest) (PolicyDecision, error)
}
