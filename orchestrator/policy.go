package orchestrator

import (
	"context"
)

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

func (r *Runtime) authorizeLocked(ctx context.Context, request PolicyRequest) (PolicyDecision, error) {
	if request.RunID == "" {
		request.RunID = requestRunID(request)
	}
	if request.TaskID == "" {
		request.TaskID = requestTaskID(request)
	}
	engine := r.policy
	if engine == nil {
		engine = allowPolicyEngine{}
	}
	decision, err := engine.Authorize(ctx, request)
	if err != nil {
		return PolicyDecision{}, err
	}
	if decision.Effect == "" {
		decision.Effect = PolicyEffectAllow
	}
	r.recordTraceLocked(request.RunID, request.TaskID, "policy.authorize."+string(request.Operation), "policy")
	if decision.Effect == PolicyEffectDeny || decision.Effect == PolicyEffectAbort {
		return decision, ErrPolicyDenied
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

func actorFromHolder(holderType HolderType, holderID string) SourceIdentity {
	switch holderType {
	case HolderAgent:
		return SourceIdentity{Type: SourceAgent, ID: holderID}
	case HolderComponent:
		return SourceIdentity{Type: SourceComponent, ID: holderID}
	default:
		return SourceIdentity{}
	}
}
