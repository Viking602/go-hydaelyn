package adapter

import (
	"slices"

	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/internal/core/model"
)

func stringMapToModel(in map[string]string) map[string]string   { return cloneStringMap(in) }
func stringMapFromModel(in map[string]string) map[string]string { return cloneStringMap(in) }
func anyMapToModel(in map[string]any) map[string]any            { return cloneAnyMap(in) }
func anyMapFromModel(in map[string]any) map[string]any          { return cloneAnyMap(in) }

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneAnyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneStrings(in []string) []string {
	if in == nil {
		return nil
	}
	return append([]string(nil), in...)
}

func RunToModel(in api.Run) model.Run {
	return model.Run{
		ID:         in.ID,
		Status:     model.RunStatus(in.Status),
		Request:    in.Request,
		RootTaskID: in.RootTaskID,
		Metadata:   stringMapToModel(in.Metadata),
		CreatedAt:  in.CreatedAt,
		UpdatedAt:  in.UpdatedAt,
	}
}

func RunFromModel(in model.Run) api.Run {
	return api.Run{
		ID:         in.ID,
		Status:     api.RunStatus(in.Status),
		Request:    in.Request,
		RootTaskID: in.RootTaskID,
		Metadata:   stringMapFromModel(in.Metadata),
		CreatedAt:  in.CreatedAt,
		UpdatedAt:  in.UpdatedAt,
	}
}

func RunsFromModel(in []model.Run) []api.Run {
	if in == nil {
		return nil
	}
	out := make([]api.Run, len(in))
	for i, item := range in {
		out[i] = RunFromModel(item)
	}
	return out
}

func TaskToModel(in api.Task) model.Task {
	return model.Task{
		ID:                 in.ID,
		RunID:              in.RunID,
		ParentTaskID:       in.ParentTaskID,
		Type:               model.TaskType(in.Type),
		Goal:               in.Goal,
		AssignedAgentID:    in.AssignedAgentID,
		OwnerAgentID:       in.OwnerAgentID,
		OwnerComponent:     in.OwnerComponent,
		Status:             model.TaskStatus(in.Status),
		Version:            in.Version,
		Attempts:           in.Attempts,
		HandoffCount:       in.HandoffCount,
		OwnerHistory:       cloneStrings(in.OwnerHistory),
		AllowsAction:       in.AllowsAction,
		Tags:               cloneStrings(in.Tags),
		CompletionCriteria: cloneStrings(in.CompletionCriteria),
		DependsOn:          cloneStrings(in.DependsOn),
		AwaitMode:          model.AwaitMode(in.AwaitMode),
		AwaitQuorum:        in.AwaitQuorum,
		OnDependencyFailed: model.OnDependencyFailed(in.OnDependencyFailed),
		ReadSelectors:      BlackboardSelectorsToModel(in.ReadSelectors),
		WriteTargets:       cloneStrings(in.WriteTargets),
		RetryPolicy:        RetryPolicyToModel(in.RetryPolicy),
		PolicyDecisions:    PolicyDecisionsToModel(in.PolicyDecisions),
		Result:             TypedReportPtrToModel(in.Result),
		Error:              in.Error,
		CreatedAt:          in.CreatedAt,
		UpdatedAt:          in.UpdatedAt,
	}
}

func TaskFromModel(in model.Task) api.Task {
	return api.Task{
		ID:                 in.ID,
		RunID:              in.RunID,
		ParentTaskID:       in.ParentTaskID,
		Type:               api.TaskType(in.Type),
		Goal:               in.Goal,
		AssignedAgentID:    in.AssignedAgentID,
		OwnerAgentID:       in.OwnerAgentID,
		OwnerComponent:     in.OwnerComponent,
		Status:             api.TaskStatus(in.Status),
		Version:            in.Version,
		Attempts:           in.Attempts,
		HandoffCount:       in.HandoffCount,
		OwnerHistory:       cloneStrings(in.OwnerHistory),
		AllowsAction:       in.AllowsAction,
		Tags:               cloneStrings(in.Tags),
		CompletionCriteria: cloneStrings(in.CompletionCriteria),
		DependsOn:          cloneStrings(in.DependsOn),
		AwaitMode:          api.AwaitMode(in.AwaitMode),
		AwaitQuorum:        in.AwaitQuorum,
		OnDependencyFailed: api.OnDependencyFailed(in.OnDependencyFailed),
		ReadSelectors:      BlackboardSelectorsFromModel(in.ReadSelectors),
		WriteTargets:       cloneStrings(in.WriteTargets),
		RetryPolicy:        RetryPolicyFromModel(in.RetryPolicy),
		PolicyDecisions:    PolicyDecisionsFromModel(in.PolicyDecisions),
		Result:             TypedReportPtrFromModel(in.Result),
		Error:              in.Error,
		CreatedAt:          in.CreatedAt,
		UpdatedAt:          in.UpdatedAt,
	}
}

func TasksToModel(in []api.Task) []model.Task {
	if in == nil {
		return nil
	}
	out := make([]model.Task, len(in))
	for i, item := range in {
		out[i] = TaskToModel(item)
	}
	return out
}

func TasksFromModel(in []model.Task) []api.Task {
	if in == nil {
		return nil
	}
	out := make([]api.Task, len(in))
	for i, item := range in {
		out[i] = TaskFromModel(item)
	}
	return out
}

func TaskEnvelopeToModel(in api.TaskEnvelope) model.TaskEnvelope {
	return model.TaskEnvelope{
		ID:              in.ID,
		RunID:           in.RunID,
		TaskID:          in.TaskID,
		TodoID:          in.TodoID,
		From:            in.From,
		Type:            in.Type,
		TargetAgentID:   in.TargetAgentID,
		TargetComponent: in.TargetComponent,
		Payload:         anyMapToModel(in.Payload),
		ReadSelectors:   BlackboardSelectorsToModel(in.ReadSelectors),
		WriteTargets:    cloneStrings(in.WriteTargets),
		TraceID:         in.TraceID,
		TaskVersion:     in.TaskVersion,
		Deadline:        in.Deadline,
		RetryPolicy:     RetryPolicyToModel(in.RetryPolicy),
		Status:          in.Status,
		Attempts:        in.Attempts,
		NextRetryAt:     in.NextRetryAt,
		CreatedAt:       in.CreatedAt,
		DeliveredAt:     in.DeliveredAt,
	}
}

func TaskEnvelopeFromModel(in model.TaskEnvelope) api.TaskEnvelope {
	return api.TaskEnvelope{
		ID:              in.ID,
		RunID:           in.RunID,
		TaskID:          in.TaskID,
		TodoID:          in.TodoID,
		From:            in.From,
		Type:            in.Type,
		TargetAgentID:   in.TargetAgentID,
		TargetComponent: in.TargetComponent,
		Payload:         anyMapFromModel(in.Payload),
		ReadSelectors:   BlackboardSelectorsFromModel(in.ReadSelectors),
		WriteTargets:    cloneStrings(in.WriteTargets),
		TraceID:         in.TraceID,
		TaskVersion:     in.TaskVersion,
		Deadline:        in.Deadline,
		RetryPolicy:     RetryPolicyFromModel(in.RetryPolicy),
		Status:          in.Status,
		Attempts:        in.Attempts,
		NextRetryAt:     in.NextRetryAt,
		CreatedAt:       in.CreatedAt,
		DeliveredAt:     in.DeliveredAt,
	}
}

func TaskEnvelopesToModel(in []api.TaskEnvelope) []model.TaskEnvelope {
	if in == nil {
		return nil
	}
	out := make([]model.TaskEnvelope, len(in))
	for i, item := range in {
		out[i] = TaskEnvelopeToModel(item)
	}
	return out
}

func TaskEnvelopesFromModel(in []model.TaskEnvelope) []api.TaskEnvelope {
	if in == nil {
		return nil
	}
	out := make([]api.TaskEnvelope, len(in))
	for i, item := range in {
		out[i] = TaskEnvelopeFromModel(item)
	}
	return out
}

func RetryPolicyToModel(in api.RetryPolicy) model.RetryPolicy {
	return model.RetryPolicy{MaxAttempts: in.MaxAttempts, Backoff: in.Backoff}
}

func RetryPolicyFromModel(in model.RetryPolicy) api.RetryPolicy {
	return api.RetryPolicy{MaxAttempts: in.MaxAttempts, Backoff: in.Backoff}
}

func SourceIdentityToModel(in api.SourceIdentity) model.SourceIdentity {
	return model.SourceIdentity{Type: model.SourceType(in.Type), ID: in.ID}
}

func SourceIdentityFromModel(in model.SourceIdentity) api.SourceIdentity {
	return api.SourceIdentity{Type: api.SourceType(in.Type), ID: in.ID}
}

func BlackboardItemToModel(in api.BlackboardItem) model.BlackboardItem {
	return model.BlackboardItem{
		ID:           in.ID,
		RunID:        in.RunID,
		TaskID:       in.TaskID,
		Type:         model.BlackboardItemType(in.Type),
		Source:       SourceIdentityToModel(in.Source),
		Content:      in.Content,
		Confidence:   in.Confidence,
		EvidenceRefs: cloneStrings(in.EvidenceRefs),
		ArtifactRefs: cloneStrings(in.ArtifactRefs),
		Visibility:   model.BlackboardVisibility(in.Visibility),
		Version:      in.Version,
		Key:          in.Key,
		Payload:      in.Payload,
		CreatedAt:    in.CreatedAt,
	}
}

func BlackboardItemFromModel(in model.BlackboardItem) api.BlackboardItem {
	return api.BlackboardItem{
		ID:           in.ID,
		RunID:        in.RunID,
		TaskID:       in.TaskID,
		Type:         api.BlackboardItemType(in.Type),
		Source:       SourceIdentityFromModel(in.Source),
		Content:      in.Content,
		Confidence:   in.Confidence,
		EvidenceRefs: cloneStrings(in.EvidenceRefs),
		ArtifactRefs: cloneStrings(in.ArtifactRefs),
		Visibility:   api.BlackboardVisibility(in.Visibility),
		Version:      in.Version,
		Key:          in.Key,
		Payload:      in.Payload,
		CreatedAt:    in.CreatedAt,
	}
}

func BlackboardItemsToModel(in []api.BlackboardItem) []model.BlackboardItem {
	if in == nil {
		return nil
	}
	out := make([]model.BlackboardItem, len(in))
	for i, item := range in {
		out[i] = BlackboardItemToModel(item)
	}
	return out
}

func BlackboardItemsFromModel(in []model.BlackboardItem) []api.BlackboardItem {
	if in == nil {
		return nil
	}
	out := make([]api.BlackboardItem, len(in))
	for i, item := range in {
		out[i] = BlackboardItemFromModel(item)
	}
	return out
}

func BlackboardSelectorToModel(in api.BlackboardSelector) model.BlackboardSelector {
	sourceTypes, sourceIDs := selectorSourceToModel(in)
	return model.BlackboardSelector{
		RunID:        in.RunID,
		TaskID:       in.TaskID,
		ItemTypes:    BlackboardItemTypesToModel(in.ItemTypes),
		SourceTypes:  sourceTypes,
		SourceIDs:    sourceIDs,
		Visibility:   model.BlackboardVisibility(in.Visibility),
		Tags:         cloneStrings(in.Tags),
		SinceVersion: in.SinceVersion,
		Limit:        in.Limit,
		Keys:         cloneStrings(in.Keys),
	}
}

func BlackboardSelectorFromModel(in model.BlackboardSelector) api.BlackboardSelector {
	return api.BlackboardSelector{
		RunID:        in.RunID,
		TaskID:       in.TaskID,
		ItemTypes:    BlackboardItemTypesFromModel(in.ItemTypes),
		SourceTypes:  SourceTypesFromModel(in.SourceTypes),
		SourceIDs:    cloneStrings(in.SourceIDs),
		Visibility:   api.BlackboardVisibility(in.Visibility),
		Tags:         cloneStrings(in.Tags),
		SinceVersion: in.SinceVersion,
		Limit:        in.Limit,
		Keys:         cloneStrings(in.Keys),
	}
}

func selectorSourceToModel(in api.BlackboardSelector) ([]model.SourceType, []string) {
	sourceTypes := SourceTypesToModel(in.SourceTypes)
	sourceIDs := cloneStrings(in.SourceIDs)
	legacyAgentIDs := deprecatedSelectorSourceAgentIDs(in)
	if len(legacyAgentIDs) == 0 {
		return sourceTypes, sourceIDs
	}
	if !slices.Contains(sourceTypes, model.SourceAgent) {
		sourceTypes = append(sourceTypes, model.SourceAgent)
	}
	for _, id := range legacyAgentIDs {
		if !slices.Contains(sourceIDs, id) {
			sourceIDs = append(sourceIDs, id)
		}
	}
	return sourceTypes, sourceIDs
}

func deprecatedSelectorSourceAgentIDs(in api.BlackboardSelector) []string {
	//lint:ignore SA1019 SourceAgentIDs is accepted at the public boundary for backward compatibility.
	return cloneStrings(in.SourceAgentIDs)
}

func BlackboardSelectorsToModel(in []api.BlackboardSelector) []model.BlackboardSelector {
	if in == nil {
		return nil
	}
	out := make([]model.BlackboardSelector, len(in))
	for i, item := range in {
		out[i] = BlackboardSelectorToModel(item)
	}
	return out
}

func BlackboardSelectorsFromModel(in []model.BlackboardSelector) []api.BlackboardSelector {
	if in == nil {
		return nil
	}
	out := make([]api.BlackboardSelector, len(in))
	for i, item := range in {
		out[i] = BlackboardSelectorFromModel(item)
	}
	return out
}

func BlackboardItemTypesToModel(in []api.BlackboardItemType) []model.BlackboardItemType {
	if in == nil {
		return nil
	}
	out := make([]model.BlackboardItemType, len(in))
	for i, item := range in {
		out[i] = model.BlackboardItemType(item)
	}
	return out
}

func BlackboardItemTypesFromModel(in []model.BlackboardItemType) []api.BlackboardItemType {
	if in == nil {
		return nil
	}
	out := make([]api.BlackboardItemType, len(in))
	for i, item := range in {
		out[i] = api.BlackboardItemType(item)
	}
	return out
}

func SourceTypesToModel(in []api.SourceType) []model.SourceType {
	if in == nil {
		return nil
	}
	out := make([]model.SourceType, len(in))
	for i, item := range in {
		out[i] = model.SourceType(item)
	}
	return out
}

func SourceTypesFromModel(in []model.SourceType) []api.SourceType {
	if in == nil {
		return nil
	}
	out := make([]api.SourceType, len(in))
	for i, item := range in {
		out[i] = api.SourceType(item)
	}
	return out
}

func EventToModel(in api.Event) model.Event {
	return model.Event{
		RunID:      in.RunID,
		TaskID:     in.TaskID,
		Sequence:   in.Sequence,
		Type:       model.EventType(in.Type),
		Payload:    anyMapToModel(in.Payload),
		RecordedAt: in.RecordedAt,
	}
}

func EventFromModel(in model.Event) api.Event {
	return api.Event{
		RunID:      in.RunID,
		TaskID:     in.TaskID,
		Sequence:   in.Sequence,
		Type:       api.EventType(in.Type),
		Payload:    anyMapFromModel(in.Payload),
		RecordedAt: in.RecordedAt,
	}
}

func EventsToModel(in []api.Event) []model.Event {
	if in == nil {
		return nil
	}
	out := make([]model.Event, len(in))
	for i, item := range in {
		out[i] = EventToModel(item)
	}
	return out
}

func EventsFromModel(in []model.Event) []api.Event {
	if in == nil {
		return nil
	}
	out := make([]api.Event, len(in))
	for i, item := range in {
		out[i] = EventFromModel(item)
	}
	return out
}

func FlowToModel(in api.Flow) model.Flow {
	return model.Flow{
		Name:                     in.Name,
		PlannerPreset:            in.PlannerPreset,
		RouterPreset:             in.RouterPreset,
		PolicyPreset:             in.PolicyPreset,
		ProjectorPreset:          in.ProjectorPreset,
		BypassTaskStore:          in.BypassTaskStore,
		BypassPolicyEngine:       in.BypassPolicyEngine,
		BypassTaskExecutionLease: in.BypassTaskExecutionLease,
		BypassHandoff:            in.BypassHandoff,
		BypassResponseLayer:      in.BypassResponseLayer,
		BypassOutputGateway:      in.BypassOutputGateway,
	}
}

func FlowFromModel(in model.Flow) api.Flow {
	return api.Flow{
		Name:                     in.Name,
		PlannerPreset:            in.PlannerPreset,
		RouterPreset:             in.RouterPreset,
		PolicyPreset:             in.PolicyPreset,
		ProjectorPreset:          in.ProjectorPreset,
		BypassTaskStore:          in.BypassTaskStore,
		BypassPolicyEngine:       in.BypassPolicyEngine,
		BypassTaskExecutionLease: in.BypassTaskExecutionLease,
		BypassHandoff:            in.BypassHandoff,
		BypassResponseLayer:      in.BypassResponseLayer,
		BypassOutputGateway:      in.BypassOutputGateway,
	}
}

func TaskExecutionLeaseToModel(in api.TaskExecutionLease) model.TaskExecutionLease {
	return model.TaskExecutionLease{
		ID:          in.ID,
		RunID:       in.RunID,
		TaskID:      in.TaskID,
		EnvelopeID:  in.EnvelopeID,
		HolderType:  model.HolderType(in.HolderType),
		HolderID:    in.HolderID,
		TaskVersion: in.TaskVersion,
		AcquiredAt:  in.AcquiredAt,
		ExpiresAt:   in.ExpiresAt,
		HeartbeatAt: in.HeartbeatAt,
		Status:      model.LeaseStatus(in.Status),
	}
}

func TaskExecutionLeaseFromModel(in model.TaskExecutionLease) api.TaskExecutionLease {
	return api.TaskExecutionLease{
		ID:          in.ID,
		RunID:       in.RunID,
		TaskID:      in.TaskID,
		EnvelopeID:  in.EnvelopeID,
		HolderType:  api.HolderType(in.HolderType),
		HolderID:    in.HolderID,
		TaskVersion: in.TaskVersion,
		AcquiredAt:  in.AcquiredAt,
		ExpiresAt:   in.ExpiresAt,
		HeartbeatAt: in.HeartbeatAt,
		Status:      api.LeaseStatus(in.Status),
	}
}

func ResumeTokenToModel(in api.ResumeToken) model.ResumeToken {
	return model.ResumeToken{
		TokenID:          in.TokenID,
		RunID:            in.RunID,
		TaskID:           in.TaskID,
		ApprovalID:       in.ApprovalID,
		ExpiresAt:        in.ExpiresAt,
		ResumeCommand:    in.ResumeCommand,
		ResumeRunState:   model.RunStatus(in.ResumeRunState),
		ResumeTaskState:  model.TaskStatus(in.ResumeTaskState),
		ResumePayloadRef: in.ResumePayloadRef,
		Metadata:         stringMapToModel(in.Metadata),
	}
}

func ResumeTokenFromModel(in model.ResumeToken) api.ResumeToken {
	return api.ResumeToken{
		TokenID:          in.TokenID,
		RunID:            in.RunID,
		TaskID:           in.TaskID,
		ApprovalID:       in.ApprovalID,
		ExpiresAt:        in.ExpiresAt,
		ResumeCommand:    in.ResumeCommand,
		ResumeRunState:   api.RunStatus(in.ResumeRunState),
		ResumeTaskState:  api.TaskStatus(in.ResumeTaskState),
		ResumePayloadRef: in.ResumePayloadRef,
		Metadata:         stringMapFromModel(in.Metadata),
	}
}

func ResumeTokensFromModelMap(in map[string]model.ResumeToken) map[string]api.ResumeToken {
	if in == nil {
		return nil
	}
	out := make(map[string]api.ResumeToken, len(in))
	for k, v := range in {
		out[k] = ResumeTokenFromModel(v)
	}
	return out
}

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

func ApprovalDecisionToModel(in api.ApprovalDecision) model.ApprovalDecision {
	return model.ApprovalDecision{ApprovalID: in.ApprovalID, DecidedBy: in.DecidedBy, Decision: in.Decision, Reason: in.Reason, DecidedAt: in.DecidedAt}
}

func ApprovalDecisionFromModel(in model.ApprovalDecision) api.ApprovalDecision {
	return api.ApprovalDecision{ApprovalID: in.ApprovalID, DecidedBy: in.DecidedBy, Decision: in.Decision, Reason: in.Reason, DecidedAt: in.DecidedAt}
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

func ProjectionToModel(in api.Projection) model.Projection {
	return model.Projection{Run: RunToModel(in.Run), Tasks: TasksToModelMap(in.Tasks), Messages: UserMessagesToModel(in.Messages), SideEffects: ReplaySideEffectsToModel(in.SideEffects)}
}

func ProjectionFromModel(in model.Projection) api.Projection {
	return api.Projection{Run: RunFromModel(in.Run), Tasks: TasksFromModelMap(in.Tasks), Messages: UserMessagesFromModel(in.Messages), SideEffects: ReplaySideEffectsFromModel(in.SideEffects)}
}

func TasksToModelMap(in map[string]api.Task) map[string]model.Task {
	if in == nil {
		return nil
	}
	out := make(map[string]model.Task, len(in))
	for k, v := range in {
		out[k] = TaskToModel(v)
	}
	return out
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

func ReplaySideEffectsToModel(in api.ReplaySideEffects) model.ReplaySideEffects {
	return model.ReplaySideEffects{MailboxDeliveries: in.MailboxDeliveries, UserMessagePublications: in.UserMessagePublications, ActionExecutions: in.ActionExecutions}
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
	return model.TypedReport{Status: model.ReportStatus(in.Status), Summary: in.Summary, Structured: anyMapToModel(in.Structured), ActionOutcome: ActionOutcomePtrToModel(in.ActionOutcome), Handoff: HandoffRequestPtrToModel(in.Handoff)}
}

func TypedReportFromModel(in model.TypedReport) api.TypedReport {
	return api.TypedReport{Status: api.ReportStatus(in.Status), Summary: in.Summary, Structured: anyMapFromModel(in.Structured), ActionOutcome: ActionOutcomePtrFromModel(in.ActionOutcome), Handoff: HandoffRequestPtrFromModel(in.Handoff)}
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

func UserMessageToModel(in api.UserMessage) model.UserMessage {
	return model.UserMessage{
		ID:             in.ID,
		RunID:          in.RunID,
		TaskID:         in.TaskID,
		Type:           model.UserMessageType(in.Type),
		Title:          in.Title,
		Payload:        in.Payload,
		Status:         model.UserMessageStatus(in.Status),
		IdempotencyKey: in.IdempotencyKey,
		PublishedAt:    in.PublishedAt,
		CreatedAt:      in.CreatedAt,
		UpdatedAt:      in.UpdatedAt,
	}
}

func UserMessageFromModel(in model.UserMessage) api.UserMessage {
	return api.UserMessage{
		ID:             in.ID,
		RunID:          in.RunID,
		TaskID:         in.TaskID,
		Type:           api.UserMessageType(in.Type),
		Title:          in.Title,
		Payload:        in.Payload,
		Status:         api.UserMessageStatus(in.Status),
		IdempotencyKey: in.IdempotencyKey,
		PublishedAt:    in.PublishedAt,
		CreatedAt:      in.CreatedAt,
		UpdatedAt:      in.UpdatedAt,
	}
}

func UserMessagesToModel(in []api.UserMessage) []model.UserMessage {
	if in == nil {
		return nil
	}
	out := make([]model.UserMessage, len(in))
	for i, item := range in {
		out[i] = UserMessageToModel(item)
	}
	return out
}

func UserMessagesFromModel(in []model.UserMessage) []api.UserMessage {
	if in == nil {
		return nil
	}
	out := make([]api.UserMessage, len(in))
	for i, item := range in {
		out[i] = UserMessageFromModel(item)
	}
	return out
}

func ActionOutcomeToModel(in api.ActionOutcome) model.ActionOutcome {
	return model.ActionOutcome{
		AttemptID:         in.AttemptID,
		ResultID:          in.ResultID,
		ActionID:          in.ActionID,
		RunID:             in.RunID,
		TaskID:            in.TaskID,
		Status:            model.ActionAttemptStatus(in.Status),
		Summary:           in.Summary,
		Output:            in.Output,
		ArtifactRefs:      cloneStrings(in.ArtifactRefs),
		RollbackAvailable: in.RollbackAvailable,
		ExternalResultRef: in.ExternalResultRef,
		CreatedAt:         in.CreatedAt,
		Error:             in.Error,
	}
}

func ActionOutcomeFromModel(in model.ActionOutcome) api.ActionOutcome {
	return api.ActionOutcome{
		AttemptID:         in.AttemptID,
		ResultID:          in.ResultID,
		ActionID:          in.ActionID,
		RunID:             in.RunID,
		TaskID:            in.TaskID,
		Status:            api.ActionAttemptStatus(in.Status),
		Summary:           in.Summary,
		Output:            in.Output,
		ArtifactRefs:      cloneStrings(in.ArtifactRefs),
		RollbackAvailable: in.RollbackAvailable,
		ExternalResultRef: in.ExternalResultRef,
		CreatedAt:         in.CreatedAt,
		Error:             in.Error,
	}
}

func ActionOutcomePtrToModel(in *api.ActionOutcome) *model.ActionOutcome {
	if in == nil {
		return nil
	}
	out := ActionOutcomeToModel(*in)
	return &out
}

func ActionOutcomePtrFromModel(in *model.ActionOutcome) *api.ActionOutcome {
	if in == nil {
		return nil
	}
	out := ActionOutcomeFromModel(*in)
	return &out
}

func ToolToModel(in api.Tool) model.Tool {
	return model.Tool{
		Name:               in.Name,
		EffectType:         model.ToolEffectType(in.EffectType),
		RequiresActionTask: in.RequiresActionTask,
		RiskLevel:          in.RiskLevel,
		Idempotent:         in.Idempotent,
		Timeout:            in.Timeout,
		RetryPolicy:        RetryPolicyToModel(in.RetryPolicy),
		PolicyTags:         cloneStrings(in.PolicyTags),
		Metadata:           stringMapToModel(in.Metadata),
	}
}

func ToolFromModel(in model.Tool) api.Tool {
	return api.Tool{
		Name:               in.Name,
		EffectType:         api.ToolEffectType(in.EffectType),
		RequiresActionTask: in.RequiresActionTask,
		RiskLevel:          in.RiskLevel,
		Idempotent:         in.Idempotent,
		Timeout:            in.Timeout,
		RetryPolicy:        RetryPolicyFromModel(in.RetryPolicy),
		PolicyTags:         cloneStrings(in.PolicyTags),
		Metadata:           stringMapFromModel(in.Metadata),
	}
}

func ToolPtrToModel(in *api.Tool) *model.Tool {
	if in == nil {
		return nil
	}
	out := ToolToModel(*in)
	return &out
}

func ToolPtrFromModel(in *model.Tool) *api.Tool {
	if in == nil {
		return nil
	}
	out := ToolFromModel(*in)
	return &out
}

func ActionAttemptToModel(in api.ActionAttempt) model.ActionAttempt {
	return model.ActionAttempt{
		AttemptID:         in.AttemptID,
		ActionID:          in.ActionID,
		RunID:             in.RunID,
		TaskID:            in.TaskID,
		ToolName:          in.ToolName,
		Status:            model.ActionAttemptStatus(in.Status),
		IdempotencyKey:    in.IdempotencyKey,
		InputHash:         in.InputHash,
		ExternalRequestID: in.ExternalRequestID,
		ExternalResultRef: in.ExternalResultRef,
		RequiresReconcile: in.RequiresReconcile,
	}
}

func ActionAttemptFromModel(in model.ActionAttempt) api.ActionAttempt {
	return api.ActionAttempt{
		AttemptID:         in.AttemptID,
		ActionID:          in.ActionID,
		RunID:             in.RunID,
		TaskID:            in.TaskID,
		ToolName:          in.ToolName,
		Status:            api.ActionAttemptStatus(in.Status),
		IdempotencyKey:    in.IdempotencyKey,
		InputHash:         in.InputHash,
		ExternalRequestID: in.ExternalRequestID,
		ExternalResultRef: in.ExternalResultRef,
		RequiresReconcile: in.RequiresReconcile,
	}
}

func ActionAttemptPtrToModel(in *api.ActionAttempt) *model.ActionAttempt {
	if in == nil {
		return nil
	}
	out := ActionAttemptToModel(*in)
	return &out
}

func ActionAttemptPtrFromModel(in *model.ActionAttempt) *api.ActionAttempt {
	if in == nil {
		return nil
	}
	out := ActionAttemptFromModel(*in)
	return &out
}

func AgentProfileToModel(in api.AgentProfile) model.AgentProfile {
	return model.AgentProfile{ID: in.ID, Role: in.Role, Groups: cloneStrings(in.Groups), Metadata: stringMapToModel(in.Metadata)}
}

func AgentProfileFromModel(in model.AgentProfile) api.AgentProfile {
	return api.AgentProfile{ID: in.ID, Role: in.Role, Groups: cloneStrings(in.Groups), Metadata: stringMapFromModel(in.Metadata)}
}

func AgentProfilesFromModel(in []model.AgentProfile) []api.AgentProfile {
	if in == nil {
		return nil
	}
	out := make([]api.AgentProfile, len(in))
	for i, item := range in {
		out[i] = AgentProfileFromModel(item)
	}
	return out
}

func AddressToModel(in api.Address) model.Address {
	return model.Address{Kind: model.AddressKind(in.Kind), AgentID: in.AgentID, Role: in.Role, Group: in.Group}
}

func AddressFromModel(in model.Address) api.Address {
	return api.Address{Kind: api.AddressKind(in.Kind), AgentID: in.AgentID, Role: in.Role, Group: in.Group}
}

func TraceSpanToModel(in api.TraceSpan) model.TraceSpan {
	return model.TraceSpan{
		ID:        in.ID,
		RunID:     in.RunID,
		TaskID:    in.TaskID,
		TraceID:   in.TraceID,
		ParentID:  in.ParentID,
		Name:      in.Name,
		Component: in.Component,
		Status:    model.TraceSpanStatus(in.Status),
		StartedAt: in.StartedAt,
		EndedAt:   in.EndedAt,
		Error:     in.Error,
		Metadata:  stringMapToModel(in.Metadata),
	}
}

func TraceSpanFromModel(in model.TraceSpan) api.TraceSpan {
	return api.TraceSpan{
		ID:        in.ID,
		RunID:     in.RunID,
		TaskID:    in.TaskID,
		TraceID:   in.TraceID,
		ParentID:  in.ParentID,
		Name:      in.Name,
		Component: in.Component,
		Status:    api.TraceSpanStatus(in.Status),
		StartedAt: in.StartedAt,
		EndedAt:   in.EndedAt,
		Error:     in.Error,
		Metadata:  stringMapFromModel(in.Metadata),
	}
}

func TraceSpansToModel(in []api.TraceSpan) []model.TraceSpan {
	if in == nil {
		return nil
	}
	out := make([]model.TraceSpan, len(in))
	for i, item := range in {
		out[i] = TraceSpanToModel(item)
	}
	return out
}

func TraceSpansFromModel(in []model.TraceSpan) []api.TraceSpan {
	if in == nil {
		return nil
	}
	out := make([]api.TraceSpan, len(in))
	for i, item := range in {
		out[i] = TraceSpanFromModel(item)
	}
	return out
}

func UserMessagePtrToModel(in *api.UserMessage) *model.UserMessage {
	if in == nil {
		return nil
	}
	out := UserMessageToModel(*in)
	return &out
}

func UserMessagePtrFromModel(in *model.UserMessage) *api.UserMessage {
	if in == nil {
		return nil
	}
	out := UserMessageFromModel(*in)
	return &out
}

func HandoffRequestPtrToModel(in *api.HandoffRequest) *model.HandoffRequest {
	if in == nil {
		return nil
	}
	out := HandoffRequestToModel(*in)
	return &out
}

func HandoffRequestPtrFromModel(in *model.HandoffRequest) *api.HandoffRequest {
	if in == nil {
		return nil
	}
	out := HandoffRequestFromModel(*in)
	return &out
}

func BlackboardSelectorPtrToModel(in *api.BlackboardSelector) *model.BlackboardSelector {
	if in == nil {
		return nil
	}
	out := BlackboardSelectorToModel(*in)
	return &out
}

func BlackboardSelectorPtrFromModel(in *model.BlackboardSelector) *api.BlackboardSelector {
	if in == nil {
		return nil
	}
	out := BlackboardSelectorFromModel(*in)
	return &out
}

func BlackboardItemPtrToModel(in *api.BlackboardItem) *model.BlackboardItem {
	if in == nil {
		return nil
	}
	out := BlackboardItemToModel(*in)
	return &out
}

func BlackboardItemPtrFromModel(in *model.BlackboardItem) *api.BlackboardItem {
	if in == nil {
		return nil
	}
	out := BlackboardItemFromModel(*in)
	return &out
}
