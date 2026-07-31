// Package policy exposes the stable authorization contract used by the
// Venat runner.
package policy

import (
	"context"

	"github.com/Viking602/venat/api"
)

type (
	Engine         = api.PolicyEngine
	Request        = api.PolicyRequest
	Decision       = api.PolicyDecision
	Effect         = api.PolicyEffect
	Obligation     = api.PolicyObligation
	ObligationKind = api.ObligationKind
	Operation      = api.PolicyOperation
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

	OperationDispatch        = api.PolicyOperationDispatch
	OperationBlackboardRead  = api.PolicyOperationBlackboardRead
	OperationBlackboardWrite = api.PolicyOperationBlackboardWrite
	OperationHandoff         = api.PolicyOperationHandoff
	OperationToolCall        = api.PolicyOperationToolCall
	OperationAction          = api.PolicyOperationAction
	OperationResponseCompose = api.PolicyOperationResponseCompose
	OperationResponsePublish = api.PolicyOperationResponsePublish
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
			request.Tool.EffectType == api.ToolEffectWrite ||
			request.Tool.EffectType == api.ToolEffectExternalSideEffect
	}
	return request.Operation == OperationAction
}
