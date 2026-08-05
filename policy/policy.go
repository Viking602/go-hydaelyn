// Package policy exposes the stable authorization contract used by the
// Venat runner.
package policy

import (
	"context"
	"fmt"
	"reflect"

	"github.com/Viking602/venat/api"
)

type (
	Engine           = api.PolicyEngine
	Enforcer         = api.PolicyObligationEnforcer
	Request          = api.PolicyRequest
	Decision         = api.PolicyDecision
	Effect           = api.PolicyEffect
	Obligation       = api.PolicyObligation
	ObligationKind   = api.ObligationKind
	ObligationTarget = api.PolicyObligationTarget
	Operation        = api.PolicyOperation
)

const (
	EffectAllow           = api.PolicyEffectAllow
	EffectDeny            = api.PolicyEffectDeny
	EffectRequireApproval = api.PolicyEffectRequireApproval
	EffectPause           = api.PolicyEffectPause
	EffectAbort           = api.PolicyEffectAbort

	ObligationRedactFields           = api.ObligationRedactFields
	ObligationSelectorOnly           = api.ObligationSelectorOnly
	ObligationRequireHumanApproval   = api.ObligationRequireHumanApproval
	ObligationHideInternalTrace      = api.ObligationHideInternalTrace
	ObligationMaskToolOutput         = api.ObligationMaskToolOutput
	ObligationRestrictHandoffContext = api.ObligationRestrictHandoffContext

	TargetBlackboardRead  = api.PolicyTargetBlackboardRead
	TargetBlackboardWrite = api.PolicyTargetBlackboardWrite
	TargetToolResult      = api.PolicyTargetToolResult
	TargetHandoff         = api.PolicyTargetHandoff
	TargetResponse        = api.PolicyTargetResponse
	TargetTrace           = api.PolicyTargetTrace

	OperationDispatch        = api.PolicyOperationDispatch
	OperationBlackboardRead  = api.PolicyOperationBlackboardRead
	OperationBlackboardWrite = api.PolicyOperationBlackboardWrite
	OperationHandoff         = api.PolicyOperationHandoff
	OperationToolCall        = api.PolicyOperationToolCall
	OperationAction          = api.PolicyOperationAction
	OperationResponseCompose = api.PolicyOperationResponseCompose
	OperationResponsePublish = api.PolicyOperationResponsePublish
	OperationTraceRead       = api.PolicyOperationTraceRead
)

type EngineFunc func(context.Context, Request) (Decision, error)

func (f EngineFunc) Authorize(ctx context.Context, request Request) (Decision, error) {
	return f(ctx, request)
}

// Chain evaluates every engine and combines the decisions deterministically.
// Effects use the precedence abort > deny > require_approval > pause > allow.
type Chain struct {
	Engines []Engine
}

func NewChain(engines ...Engine) Chain {
	return Chain{Engines: append([]Engine(nil), engines...)}
}

func (c Chain) Authorize(ctx context.Context, request Request) (Decision, error) {
	combined := Decision{Effect: EffectAllow}
	redactions := make(map[string]struct{})
	for index, engine := range c.Engines {
		decision, err := authorizeEngine(ctx, engine, index, request)
		if err != nil {
			return Decision{Effect: EffectDeny, Reason: err.Error()}, err
		}
		mergePolicyDecision(&combined, decision, redactions)
	}
	if combined.Effect == EffectRequireApproval {
		combined.ApprovalRequired = true
	}
	return combined, nil
}

func authorizeEngine(ctx context.Context, engine Engine, index int, request Request) (Decision, error) {
	if engine == nil {
		return Decision{}, fmt.Errorf("policy: engine %d is nil", index)
	}
	decision, err := engine.Authorize(ctx, request)
	if err != nil {
		return Decision{}, fmt.Errorf("policy: engine %d: %w", index, err)
	}
	if decision.Effect == "" {
		decision.Effect = EffectAllow
	}
	if effectRank(decision.Effect) < 0 {
		return Decision{}, fmt.Errorf("policy: engine %d returned unknown effect %q", index, decision.Effect)
	}
	return decision, nil
}

func mergePolicyDecision(
	combined *Decision,
	decision Decision,
	redactions map[string]struct{},
) {
	mergePolicyEffect(combined, decision)
	for _, obligation := range decision.Obligations {
		duplicate := false
		for _, existing := range combined.Obligations {
			if reflect.DeepEqual(existing, obligation) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			combined.Obligations = append(combined.Obligations, obligation)
		}
	}
	for _, redaction := range decision.Redactions {
		if _, exists := redactions[redaction]; exists {
			continue
		}
		redactions[redaction] = struct{}{}
		combined.Redactions = append(combined.Redactions, redaction)
	}
	combined.ApprovalRequired = combined.ApprovalRequired || decision.ApprovalRequired
	if !decision.ExpiresAt.IsZero() &&
		(combined.ExpiresAt.IsZero() || decision.ExpiresAt.Before(combined.ExpiresAt)) {
		combined.ExpiresAt = decision.ExpiresAt
	}
	if combined.Metadata == nil && len(decision.Metadata) > 0 {
		combined.Metadata = make(map[string]string, len(decision.Metadata))
	}
	for key, value := range decision.Metadata {
		if _, exists := combined.Metadata[key]; !exists {
			combined.Metadata[key] = value
		}
	}
}

func mergePolicyEffect(combined *Decision, decision Decision) {
	if effectRank(decision.Effect) > effectRank(combined.Effect) {
		combined.Effect = decision.Effect
		combined.Reason = decision.Reason
		combined.DecisionID = decision.DecisionID
		return
	}
	if decision.Effect != combined.Effect {
		return
	}
	if combined.Reason == "" {
		combined.Reason = decision.Reason
	}
	if combined.DecisionID == "" {
		combined.DecisionID = decision.DecisionID
	}
}

func effectRank(effect Effect) int {
	switch effect {
	case EffectAllow:
		return 0
	case EffectPause:
		return 1
	case EffectRequireApproval:
		return 2
	case EffectDeny:
		return 3
	case EffectAbort:
		return 4
	default:
		return -1
	}
}

func DevelopmentAllowAll() Engine {
	return EngineFunc(func(context.Context, Request) (Decision, error) {
		return Decision{Effect: EffectAllow}, nil
	})
}

func DenySideEffectsByDefault() Engine {
	return EngineFunc(func(_ context.Context, request Request) (Decision, error) {
		if isSideEffect(request) {
			return Decision{Effect: EffectDeny, Reason: "side-effect operation denied by default"}, nil
		}
		return Decision{Effect: EffectAllow}, nil
	})
}

func RequireApprovalForSideEffects() Engine {
	return EngineFunc(func(_ context.Context, request Request) (Decision, error) {
		if isSideEffect(request) {
			return Decision{Effect: EffectRequireApproval, Reason: "side-effect operation requires approval"}, nil
		}
		return Decision{Effect: EffectAllow}, nil
	})
}

func isSideEffect(request Request) bool {
	if request.Tool != nil {
		return request.Tool.RequiresActionTask ||
			request.Tool.EffectType == api.ToolEffectWrite ||
			request.Tool.EffectType == api.ToolEffectExternalSideEffect
	}
	return request.Operation == OperationAction
}
