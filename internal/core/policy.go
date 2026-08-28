package core

import (
	"context"
	"maps"
	"math"
	"reflect"
	"slices"
	"time"

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

// combinedPolicyEngine layers a message-scoped checker on top of a general
// policy engine. Non-message operations are decided by the engine alone; a
// message request must clear both, so either side can deny it.
type combinedPolicyEngine struct {
	engine  PolicyEngine
	message api.MessagePolicyChecker
}

func (p combinedPolicyEngine) Authorize(ctx context.Context, request api.PolicyRequest) (api.PolicyDecision, error) {
	decision, err := p.engine.Authorize(ctx, request)
	if err != nil {
		return decision, err
	}
	if p.message == nil || request.Message == nil {
		return decision, nil
	}
	return strictestPolicyDecision(decision, p.message(*request.Message)), nil
}

// strictestPolicyDecision keeps the higher-precedence effect and unions the
// constraints of both decisions, so nothing a side asked for is dropped just
// because that side lost the effect comparison. Only DecisionID and Reason are
// taken from the winner alone: they identify one decision rather than constrain
// the operation.
func strictestPolicyDecision(engine, message api.PolicyDecision) api.PolicyDecision {
	combined, loser := engine, message
	if policyDecisionStrictness(message.Effect) > policyDecisionStrictness(engine.Effect) {
		combined, loser = message, engine
	}
	combined.ApprovalRequired = engine.ApprovalRequired || message.ApprovalRequired
	combined.ExpiresAt = earliestPolicyExpiry(engine.ExpiresAt, message.ExpiresAt)
	combined.Metadata = mergePolicyMetadata(loser.Metadata, combined.Metadata)
	combined.Obligations = mergePolicyObligations(engine.Obligations, message.Obligations)
	combined.Redactions = mergePolicyRedactions(engine.Redactions, message.Redactions)
	return combined
}

// earliestPolicyExpiry keeps the tighter of two expiries. ExpiresAt is a
// fail-closed control — normalizePolicyDecision denies an expired decision — so
// a zero "never expires" value from the winning side must not erase a real
// expiry set by the other.
func earliestPolicyExpiry(engine, message time.Time) time.Time {
	switch {
	case engine.IsZero():
		return message
	case message.IsZero():
		return engine
	case message.Before(engine):
		return message
	default:
		return engine
	}
}

// mergePolicyMetadata unions both sides so the context a policy attached still
// reaches the approval and resume token minted by an approval effect. The
// winning decision owns any key both sides set.
func mergePolicyMetadata(loser, winner map[string]string) map[string]string {
	if len(loser) == 0 {
		return maps.Clone(winner)
	}
	merged := maps.Clone(loser)
	maps.Copy(merged, winner)
	return merged
}

func policyDecisionStrictness(effect api.PolicyEffect) int {
	precedence := policyEffectPrecedence(effect)
	if precedence < 0 {
		// An unrecognized effect fails closed in normalizePolicyDecision, so it
		// must outrank every known effect instead of being swallowed here.
		return math.MaxInt
	}
	return precedence
}

func mergePolicyObligations(engine, message []api.PolicyObligation) []api.PolicyObligation {
	merged := slices.Clone(engine)
	for _, obligation := range message {
		if !slices.ContainsFunc(merged, func(existing api.PolicyObligation) bool {
			return samePolicyObligation(existing, obligation)
		}) {
			merged = append(merged, obligation)
		}
	}
	return merged
}

// samePolicyObligation compares obligations by value. Selector is a pointer, so
// == would keep two structurally identical selectors as separate obligations.
func samePolicyObligation(a, b api.PolicyObligation) bool {
	if a.Kind != b.Kind || a.Target != b.Target {
		return false
	}
	if a.Selector == nil || b.Selector == nil {
		return a.Selector == b.Selector
	}
	return reflect.DeepEqual(*a.Selector, *b.Selector)
}

func mergePolicyRedactions(engine, message []string) []string {
	merged := slices.Clone(engine)
	for _, redaction := range message {
		if !slices.Contains(merged, redaction) {
			merged = append(merged, redaction)
		}
	}
	return merged
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
