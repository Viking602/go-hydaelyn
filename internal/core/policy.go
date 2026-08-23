package core

import (
	"context"

	"github.com/Viking602/venat/api"
)

type allowPolicyEngine struct{}

func (allowPolicyEngine) Authorize(context.Context, api.PolicyRequest) (api.PolicyDecision, error) {
	return api.PolicyDecision{Effect: api.PolicyEffectAllow}, nil
}

type messagePolicyAdapter struct {
	check api.MessagePolicyChecker
}

func (p messagePolicyAdapter) Authorize(_ context.Context, request api.PolicyRequest) (api.PolicyDecision, error) {
	if p.check == nil || request.Message == nil {
		return api.PolicyDecision{Effect: api.PolicyEffectAllow}, nil
	}
	return p.check(*request.Message), nil
}

func requestRunID(request api.PolicyRequest) string {
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

func requestTaskID(request api.PolicyRequest) string {
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
