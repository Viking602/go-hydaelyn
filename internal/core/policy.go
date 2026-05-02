package core

import "context"

type allowPolicyEngine struct{}

func (allowPolicyEngine) Authorize(context.Context, PolicyRequest) (PolicyDecision, error) {
	return PolicyDecision{Effect: PolicyEffectAllow}, nil
}

type messagePolicyAdapter struct {
	check MessagePolicyChecker
}

func (p messagePolicyAdapter) Authorize(_ context.Context, request PolicyRequest) (PolicyDecision, error) {
	if p.check == nil || request.Message == nil {
		return PolicyDecision{Effect: PolicyEffectAllow}, nil
	}
	decision := p.check(*request.Message)
	if decision.Effect == "" {
		decision.Effect = PolicyEffectAllow
	}
	return decision, nil
}

func requestRunID(request PolicyRequest) string {
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

func requestTaskID(request PolicyRequest) string {
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
