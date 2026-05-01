// Package policy exposes the stable authorization contract used by the
// Hydaelyn runner.
package policy

import (
	"context"

	"github.com/Viking602/go-hydaelyn/orchestrator"
)

type (
	Engine         = orchestrator.PolicyEngine
	Request        = orchestrator.PolicyRequest
	Decision       = orchestrator.PolicyDecision
	Effect         = orchestrator.PolicyEffect
	Obligation     = orchestrator.PolicyObligation
	ObligationKind = orchestrator.ObligationKind
	Operation      = orchestrator.PolicyOperation
)

const (
	EffectAllow           = orchestrator.PolicyEffectAllow
	EffectDeny            = orchestrator.PolicyEffectDeny
	EffectRequireApproval = orchestrator.PolicyEffectRequireApproval
	EffectPause           = orchestrator.PolicyEffectPause
	EffectAbort           = orchestrator.PolicyEffectAbort

	ObligationRedactFields           = orchestrator.ObligationRedactFields
	ObligationSelectorOnly           = orchestrator.ObligationSelectorOnly
	ObligationRequireHumanApproval   = orchestrator.ObligationRequireHumanApproval
	ObligationHideInternalTrace      = orchestrator.ObligationHideInternalTrace
	ObligationMaskToolOutput         = orchestrator.ObligationMaskToolOutput
	ObligationRestrictHandoffContext = orchestrator.ObligationRestrictHandoffContext

	OperationDispatch        = orchestrator.PolicyOperationDispatch
	OperationBlackboardRead  = orchestrator.PolicyOperationBlackboardRead
	OperationBlackboardWrite = orchestrator.PolicyOperationBlackboardWrite
	OperationHandoff         = orchestrator.PolicyOperationHandoff
	OperationToolCall        = orchestrator.PolicyOperationToolCall
	OperationAction          = orchestrator.PolicyOperationAction
	OperationResponseCompose = orchestrator.PolicyOperationResponseCompose
	OperationResponsePublish = orchestrator.PolicyOperationResponsePublish
)

type EngineFunc func(context.Context, Request) (Decision, error)

func (f EngineFunc) Authorize(ctx context.Context, request Request) (Decision, error) {
	return f(ctx, request)
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
			request.Tool.EffectType == orchestrator.ToolEffectWrite ||
			request.Tool.EffectType == orchestrator.ToolEffectExternalSideEffect
	}
	return request.Operation == OperationAction
}
