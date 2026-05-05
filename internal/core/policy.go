package core

import (
	"context"

	"github.com/Viking602/go-hydaelyn/internal/core/model"
)

type allowPolicyEngine struct{}

func (allowPolicyEngine) Authorize(context.Context, model.PolicyRequest) (model.PolicyDecision, error) {
	return model.PolicyDecision{Effect: model.PolicyEffectAllow}, nil
}

type messagePolicyAdapter struct {
	check model.MessagePolicyChecker
}

func (p messagePolicyAdapter) Authorize(_ context.Context, request model.PolicyRequest) (model.PolicyDecision, error) {
	if p.check == nil || request.Message == nil {
		return model.PolicyDecision{Effect: model.PolicyEffectAllow}, nil
	}
	decision := p.check(*request.Message)
	if decision.Effect == "" {
		decision.Effect = model.PolicyEffectAllow
	}
	return decision, nil
}

func requestRunID(request model.PolicyRequest) string {
	if request.Message != nil {
		return request.Message.RunID
	}
	if request.Handoff != nil {
		return request.Handoff.RunID
	}
	if request.Item != nil {
		return request.Item.RunID
	}
	if request.Action != nil {
		return request.Action.RunID
	}
	return ""
}

func requestTaskID(request model.PolicyRequest) string {
	if request.Message != nil {
		return request.Message.TaskID
	}
	if request.Handoff != nil {
		return request.Handoff.TaskID
	}
	if request.Item != nil {
		return request.Item.TaskID
	}
	if request.Action != nil {
		return request.Action.TaskID
	}
	return ""
}
