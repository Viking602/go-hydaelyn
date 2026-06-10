package adapter

import (
	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/internal/core/model"
)

func HandoffRequestToModel(in api.HandoffRequest) model.HandoffRequest {
	return model.HandoffRequest{
		HandoffID:         in.HandoffID,
		RunID:             in.RunID,
		TaskID:            in.TaskID,
		FromAgentID:       in.FromAgentID,
		ToAgentID:         in.ToAgentID,
		Reason:            in.Reason,
		ContextSummary:    in.ContextSummary,
		ContextReferences: cloneStrings(in.ContextReferences),
		ContextSelectors:  BlackboardSelectorsToModel(in.ContextSelectors),
		TaskVersion:       in.TaskVersion,
		Metadata:          stringMapToModel(in.Metadata),
	}
}

func HandoffRequestFromModel(in model.HandoffRequest) api.HandoffRequest {
	return api.HandoffRequest{
		HandoffID:         in.HandoffID,
		RunID:             in.RunID,
		TaskID:            in.TaskID,
		FromAgentID:       in.FromAgentID,
		ToAgentID:         in.ToAgentID,
		Reason:            in.Reason,
		ContextSummary:    in.ContextSummary,
		ContextReferences: cloneStrings(in.ContextReferences),
		ContextSelectors:  BlackboardSelectorsFromModel(in.ContextSelectors),
		TaskVersion:       in.TaskVersion,
		Metadata:          stringMapFromModel(in.Metadata),
	}
}

func ApprovalRequestToModel(in api.ApprovalRequest) model.ApprovalRequest {
	return model.ApprovalRequest{
		ApprovalID:       in.ApprovalID,
		RunID:            in.RunID,
		TaskID:           in.TaskID,
		ActionID:         in.ActionID,
		RequesterAgentID: in.RequesterAgentID,
		Reason:           in.Reason,
		RiskSummary:      in.RiskSummary,
		RequestedAction:  in.RequestedAction,
		ExpiresAt:        in.ExpiresAt,
		Status:           in.Status,
		PayloadRef:       in.PayloadRef,
		Metadata:         stringMapToModel(in.Metadata),
	}
}

func ApprovalRequestFromModel(in model.ApprovalRequest) api.ApprovalRequest {
	return api.ApprovalRequest{
		ApprovalID:       in.ApprovalID,
		RunID:            in.RunID,
		TaskID:           in.TaskID,
		ActionID:         in.ActionID,
		RequesterAgentID: in.RequesterAgentID,
		Reason:           in.Reason,
		RiskSummary:      in.RiskSummary,
		RequestedAction:  in.RequestedAction,
		ExpiresAt:        in.ExpiresAt,
		Status:           in.Status,
		PayloadRef:       in.PayloadRef,
		Metadata:         stringMapFromModel(in.Metadata),
	}
}

func IntentToModel(in api.Intent) model.Intent {
	return model.Intent{RunID: in.RunID, Summary: in.Summary, Fields: anyMapToModel(in.Fields)}
}

func IntentFromModel(in model.Intent) api.Intent {
	return api.Intent{RunID: in.RunID, Summary: in.Summary, Fields: anyMapFromModel(in.Fields)}
}

func TodoPlanToModel(in api.TodoPlan) model.TodoPlan {
	return model.TodoPlan{RunID: in.RunID, Tasks: TasksToModel(in.Tasks)}
}

func TodoPlanFromModel(in model.TodoPlan) api.TodoPlan {
	return api.TodoPlan{RunID: in.RunID, Tasks: TasksFromModel(in.Tasks)}
}

func RoutingPlanToModel(in api.RoutingPlan) model.RoutingPlan {
	return model.RoutingPlan{RunID: in.RunID, Routes: TaskRoutesToModel(in.Routes)}
}

func RoutingPlanFromModel(in model.RoutingPlan) api.RoutingPlan {
	return api.RoutingPlan{RunID: in.RunID, Routes: TaskRoutesFromModel(in.Routes)}
}

func TaskRouteToModel(in api.TaskRoute) model.TaskRoute {
	return model.TaskRoute{TaskID: in.TaskID, TargetAgentID: in.TargetAgentID, TargetComponent: in.TargetComponent}
}

func TaskRouteFromModel(in model.TaskRoute) api.TaskRoute {
	return api.TaskRoute{TaskID: in.TaskID, TargetAgentID: in.TargetAgentID, TargetComponent: in.TargetComponent}
}

func TaskRoutesToModel(in []api.TaskRoute) []model.TaskRoute {
	if in == nil {
		return nil
	}
	out := make([]model.TaskRoute, len(in))
	for i, item := range in {
		out[i] = TaskRouteToModel(item)
	}
	return out
}

func TaskRoutesFromModel(in []model.TaskRoute) []api.TaskRoute {
	if in == nil {
		return nil
	}
	out := make([]api.TaskRoute, len(in))
	for i, item := range in {
		out[i] = TaskRouteFromModel(item)
	}
	return out
}

func TaskMonitorDecisionToModel(in api.TaskMonitorDecision) model.TaskMonitorDecision {
	return model.TaskMonitorDecision{Decision: in.Decision, Reason: in.Reason, Retry: in.Retry}
}

func TaskMonitorDecisionFromModel(in model.TaskMonitorDecision) api.TaskMonitorDecision {
	return api.TaskMonitorDecision{Decision: in.Decision, Reason: in.Reason, Retry: in.Retry}
}

func PolicyDecisionToModel(in api.PolicyDecision) model.PolicyDecision {
	return model.PolicyDecision{
		DecisionID:       in.DecisionID,
		Effect:           model.PolicyEffect(in.Effect),
		Reason:           in.Reason,
		Obligations:      PolicyObligationsToModel(in.Obligations),
		Redactions:       cloneStrings(in.Redactions),
		ApprovalRequired: in.ApprovalRequired,
		ExpiresAt:        in.ExpiresAt,
		Metadata:         stringMapToModel(in.Metadata),
	}
}

func PolicyDecisionFromModel(in model.PolicyDecision) api.PolicyDecision {
	return api.PolicyDecision{
		DecisionID:       in.DecisionID,
		Effect:           api.PolicyEffect(in.Effect),
		Reason:           in.Reason,
		Obligations:      PolicyObligationsFromModel(in.Obligations),
		Redactions:       cloneStrings(in.Redactions),
		ApprovalRequired: in.ApprovalRequired,
		ExpiresAt:        in.ExpiresAt,
		Metadata:         stringMapFromModel(in.Metadata),
	}
}

func PolicyDecisionsToModel(in []api.PolicyDecision) []model.PolicyDecision {
	if in == nil {
		return nil
	}
	out := make([]model.PolicyDecision, len(in))
	for i, item := range in {
		out[i] = PolicyDecisionToModel(item)
	}
	return out
}

func PolicyDecisionsFromModel(in []model.PolicyDecision) []api.PolicyDecision {
	if in == nil {
		return nil
	}
	out := make([]api.PolicyDecision, len(in))
	for i, item := range in {
		out[i] = PolicyDecisionFromModel(item)
	}
	return out
}

func PolicyObligationToModel(in api.PolicyObligation) model.PolicyObligation {
	return model.PolicyObligation{Kind: model.ObligationKind(in.Kind), Target: in.Target}
}

func PolicyObligationFromModel(in model.PolicyObligation) api.PolicyObligation {
	return api.PolicyObligation{Kind: api.ObligationKind(in.Kind), Target: in.Target}
}

func PolicyObligationsToModel(in []api.PolicyObligation) []model.PolicyObligation {
	if in == nil {
		return nil
	}
	out := make([]model.PolicyObligation, len(in))
	for i, item := range in {
		out[i] = PolicyObligationToModel(item)
	}
	return out
}

func PolicyObligationsFromModel(in []model.PolicyObligation) []api.PolicyObligation {
	if in == nil {
		return nil
	}
	out := make([]api.PolicyObligation, len(in))
	for i, item := range in {
		out[i] = PolicyObligationFromModel(item)
	}
	return out
}

func PolicyRequestToModel(in api.PolicyRequest) model.PolicyRequest {
	return model.PolicyRequest{
		Operation: model.PolicyOperation(in.Operation),
		RunID:     in.RunID,
		TaskID:    in.TaskID,
		Actor:     SourceIdentityToModel(in.Actor),
		Tool:      ToolPtrToModel(in.Tool),
		Message:   UserMessagePtrToModel(in.Message),
		Handoff:   HandoffRequestPtrToModel(in.Handoff),
		Selector:  BlackboardSelectorPtrToModel(in.Selector),
		Item:      BlackboardItemPtrToModel(in.Item),
		Action:    ActionAttemptPtrToModel(in.Action),
		Metadata:  stringMapToModel(in.Metadata),
	}
}

func PolicyRequestFromModel(in model.PolicyRequest) api.PolicyRequest {
	return api.PolicyRequest{
		Operation: api.PolicyOperation(in.Operation),
		RunID:     in.RunID,
		TaskID:    in.TaskID,
		Actor:     SourceIdentityFromModel(in.Actor),
		Tool:      ToolPtrFromModel(in.Tool),
		Message:   UserMessagePtrFromModel(in.Message),
		Handoff:   HandoffRequestPtrFromModel(in.Handoff),
		Selector:  BlackboardSelectorPtrFromModel(in.Selector),
		Item:      BlackboardItemPtrFromModel(in.Item),
		Action:    ActionAttemptPtrFromModel(in.Action),
		Metadata:  stringMapFromModel(in.Metadata),
	}
}

func ProjectionFromModel(in model.Projection) api.Projection {
	return api.Projection{Run: RunFromModel(in.Run), Tasks: TasksFromModelMap(in.Tasks), Messages: UserMessagesFromModel(in.Messages), SideEffects: ReplaySideEffectsFromModel(in.SideEffects)}
}

func TasksFromModelMap(in map[string]model.Task) map[string]api.Task {
	if in == nil {
		return nil
	}
	out := make(map[string]api.Task, len(in))
	for k, v := range in {
		out[k] = TaskFromModel(v)
	}
	return out
}

func ReplaySideEffectsFromModel(in model.ReplaySideEffects) api.ReplaySideEffects {
	return api.ReplaySideEffects{MailboxDeliveries: in.MailboxDeliveries, UserMessagePublications: in.UserMessagePublications, ActionExecutions: in.ActionExecutions}
}

func RunTimelineItemFromModel(in model.RunTimelineItem) api.RunTimelineItem {
	return api.RunTimelineItem{Sequence: in.Sequence, RecordedAt: in.RecordedAt, Kind: api.RunTimelineKind(in.Kind), RunID: in.RunID, TaskID: in.TaskID, Title: in.Title, Text: in.Text}
}

func RunTimelineItemsFromModel(in []model.RunTimelineItem) []api.RunTimelineItem {
	if in == nil {
		return nil
	}
	out := make([]api.RunTimelineItem, len(in))
	for i, item := range in {
		out[i] = RunTimelineItemFromModel(item)
	}
	return out
}

func TypedReportToModel(in api.TypedReport) model.TypedReport {
	return model.TypedReport{Status: model.ReportStatus(in.Status), Summary: in.Summary, Kind: in.Kind, Retryable: in.Retryable, Escalatable: in.Escalatable, Structured: anyMapToModel(in.Structured), ActionOutcome: ActionOutcomePtrToModel(in.ActionOutcome), Handoff: HandoffRequestPtrToModel(in.Handoff)}
}

func TypedReportFromModel(in model.TypedReport) api.TypedReport {
	return api.TypedReport{Status: api.ReportStatus(in.Status), Summary: in.Summary, Kind: in.Kind, Retryable: in.Retryable, Escalatable: in.Escalatable, Structured: anyMapFromModel(in.Structured), ActionOutcome: ActionOutcomePtrFromModel(in.ActionOutcome), Handoff: HandoffRequestPtrFromModel(in.Handoff)}
}

func TypedReportPtrToModel(in *api.TypedReport) *model.TypedReport {
	if in == nil {
		return nil
	}
	out := TypedReportToModel(*in)
	return &out
}

func TypedReportPtrFromModel(in *model.TypedReport) *api.TypedReport {
	if in == nil {
		return nil
	}
	out := TypedReportFromModel(*in)
	return &out
}
