package ports

import (
	"context"
	"encoding/json"

	"github.com/Viking602/venat/internal/core/model"
)

type PolicyEngine interface {
	Authorize(context.Context, model.PolicyRequest) (model.PolicyDecision, error)
}

type PolicyObligationEnforcer interface {
	EnforceBlackboardRead(context.Context, model.PolicyDecision, model.BlackboardSelector, []model.BlackboardItem) (model.BlackboardSelector, []model.BlackboardItem, error)
	EnforceBlackboardWrite(context.Context, model.PolicyDecision, model.BlackboardItem) (model.BlackboardItem, error)
	EnforceToolResult(context.Context, model.PolicyDecision, json.RawMessage) (json.RawMessage, error)
	EnforceHandoff(context.Context, model.PolicyDecision, model.HandoffRequest) (model.HandoffRequest, error)
	EnforceResponse(context.Context, model.PolicyDecision, model.UserMessage) (model.UserMessage, error)
	EnforceTrace(context.Context, model.PolicyDecision, model.TraceSpan) (model.TraceSpan, bool, error)
}
