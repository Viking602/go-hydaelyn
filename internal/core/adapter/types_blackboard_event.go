package adapter

import (
	"slices"

	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/internal/core/model"
)

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
		Name:            in.Name,
		PlannerPreset:   in.PlannerPreset,
		RouterPreset:    in.RouterPreset,
		PolicyPreset:    in.PolicyPreset,
		ProjectorPreset: in.ProjectorPreset,
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
		Version:     in.Version,
		Expiry:      in.Expiry,
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
		Version:     in.Version,
		Expiry:      in.Expiry,
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
