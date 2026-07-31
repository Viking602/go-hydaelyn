package response

import (
	"context"
	"strings"
	"time"

	"github.com/Viking602/venat/internal/core/model"
	"github.com/Viking602/venat/internal/core/ports"
)

func ApplyObligations(ctx context.Context, uow ports.UnitOfWork, message model.UserMessage, decision model.PolicyDecision) (model.UserMessage, error) {
	out := message
	for _, obligation := range decision.Obligations {
		switch obligation.Kind {
		case model.ObligationRedactFields:
			out.Payload = RedactUserPayload(out.Payload)
		case model.ObligationHideInternalTrace:
			out.Payload = HideInternalTrace(out.Payload)
		case model.ObligationMaskToolOutput:
			out.Payload = strings.ReplaceAll(out.Payload, "tool output:", "tool output: [masked]")
		case model.ObligationSelectorOnly, model.ObligationRequireHumanApproval, model.ObligationRestrictHandoffContext:
		default:
			if err := uow.Events().AppendEvent(ctx, model.Event{RunID: message.RunID, TaskID: message.TaskID, Type: model.EventPolicyObligationFailed, Payload: map[string]any{"decisionId": decision.DecisionID, "obligation": string(obligation.Kind), "target": obligation.Target, "reason": "unsupported obligation", "effectiveEffect": string(model.PolicyEffectDeny)}, RecordedAt: time.Now().UTC()}); err != nil {
				return model.UserMessage{}, err
			}
			return model.UserMessage{}, model.ErrPolicyObligationFailed
		}
	}
	if containsString(decision.Redactions, "email") {
		out.Payload = RedactEmail(out.Payload)
	}
	return out, nil
}

func CriticalContextItem(id, runID, taskID string, source model.SourceIdentity, key, payload string) model.BlackboardItem {
	if source.Type == "" {
		source = model.SourceIdentity{Type: model.SourceSystem, ID: "orchestrator"}
	}
	return model.BlackboardItem{ID: id, RunID: runID, TaskID: taskID, Type: model.BlackboardItemContext, Source: source, Visibility: model.BlackboardVisibilityAgentVisible, Key: key, Content: payload, Payload: payload, CreatedAt: time.Now().UTC()}
}

func AppendBlackboardWrittenEvent(ctx context.Context, uow ports.UnitOfWork, item model.BlackboardItem) error {
	return uow.Events().AppendEvent(ctx, model.Event{RunID: item.RunID, TaskID: item.TaskID, Type: model.EventBlackboardItemWritten, Payload: map[string]any{"itemId": item.ID, "sourceType": string(item.Source.Type), "sourceId": item.Source.ID, "visibility": string(item.Visibility), "key": item.Key}, RecordedAt: time.Now().UTC()})
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
