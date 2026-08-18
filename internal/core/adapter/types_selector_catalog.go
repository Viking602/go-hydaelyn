package adapter

import (
	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/internal/core/model"
)

func RunStatusesToModel(in []api.RunStatus) []model.RunStatus {
	if in == nil {
		return nil
	}
	out := make([]model.RunStatus, len(in))
	for i, item := range in {
		out[i] = model.RunStatus(item)
	}
	return out
}

func RunStatusesFromModel(in []model.RunStatus) []api.RunStatus {
	if in == nil {
		return nil
	}
	out := make([]api.RunStatus, len(in))
	for i, item := range in {
		out[i] = api.RunStatus(item)
	}
	return out
}

func RunSelectorToModel(in api.RunSelector) model.RunSelector {
	return model.RunSelector{
		IDs:          cloneStrings(in.IDs),
		AgentID:      in.AgentID,
		AgentVersion: in.AgentVersion,
		Statuses:     RunStatusesToModel(in.Statuses),
		Since:        in.Since,
		Until:        in.Until,
		Limit:        in.Limit,
	}
}

func RunSelectorFromModel(in model.RunSelector) api.RunSelector {
	return api.RunSelector{
		IDs:          cloneStrings(in.IDs),
		AgentID:      in.AgentID,
		AgentVersion: in.AgentVersion,
		Statuses:     RunStatusesFromModel(in.Statuses),
		Since:        in.Since,
		Until:        in.Until,
		Limit:        in.Limit,
	}
}

func UserMessageSelectorToModel(in api.UserMessageSelector) model.UserMessageSelector {
	return model.UserMessageSelector{
		RunID:     in.RunID,
		Recipient: in.Recipient,
		Statuses:  cloneStrings(in.Statuses),
		Since:     in.Since,
		Until:     in.Until,
		Limit:     in.Limit,
	}
}

func UserMessageSelectorFromModel(in model.UserMessageSelector) api.UserMessageSelector {
	return api.UserMessageSelector{
		RunID:     in.RunID,
		Recipient: in.Recipient,
		Statuses:  cloneStrings(in.Statuses),
		Since:     in.Since,
		Until:     in.Until,
		Limit:     in.Limit,
	}
}

func ResumeTokenSelectorToModel(in api.ResumeTokenSelector) model.ResumeTokenSelector {
	return model.ResumeTokenSelector{
		RunID:    in.RunID,
		TaskID:   in.TaskID,
		Statuses: cloneStrings(in.Statuses),
		Since:    in.Since,
		Until:    in.Until,
		Limit:    in.Limit,
		Cursor:   in.Cursor,
	}
}

func ResumeTokenSelectorFromModel(in model.ResumeTokenSelector) api.ResumeTokenSelector {
	return api.ResumeTokenSelector{
		RunID:    in.RunID,
		TaskID:   in.TaskID,
		Statuses: cloneStrings(in.Statuses),
		Since:    in.Since,
		Until:    in.Until,
		Limit:    in.Limit,
		Cursor:   in.Cursor,
	}
}

func AgentSelectorToModel(in api.AgentSelector) model.AgentSelector {
	return model.AgentSelector{
		IDs:      cloneStrings(in.IDs),
		Roles:    cloneStrings(in.Roles),
		Groups:   cloneStrings(in.Groups),
		Statuses: cloneStrings(in.Statuses),
		Limit:    in.Limit,
	}
}

func AgentSelectorFromModel(in model.AgentSelector) api.AgentSelector {
	return api.AgentSelector{
		IDs:      cloneStrings(in.IDs),
		Roles:    cloneStrings(in.Roles),
		Groups:   cloneStrings(in.Groups),
		Statuses: cloneStrings(in.Statuses),
		Limit:    in.Limit,
	}
}

func CapabilitySelectorToModel(in api.CapabilitySelector) model.CapabilitySelector {
	return model.CapabilitySelector{
		Names:    cloneStrings(in.Names),
		AgentIDs: cloneStrings(in.AgentIDs),
		Tags:     cloneStrings(in.Tags),
		Limit:    in.Limit,
	}
}

func CapabilitySelectorFromModel(in model.CapabilitySelector) api.CapabilitySelector {
	return api.CapabilitySelector{
		Names:    cloneStrings(in.Names),
		AgentIDs: cloneStrings(in.AgentIDs),
		Tags:     cloneStrings(in.Tags),
		Limit:    in.Limit,
	}
}

func UsageSelectorToModel(in api.UsageSelector) model.UsageSelector {
	return model.UsageSelector{
		RunID:    in.RunID,
		TaskID:   in.TaskID,
		AgentID:  in.AgentID,
		Kind:     model.UsageKind(in.Kind),
		ToolName: in.ToolName,
		Provider: in.Provider,
		Since:    in.Since,
		Until:    in.Until,
		Limit:    in.Limit,
	}
}

func UsageSelectorFromModel(in model.UsageSelector) api.UsageSelector {
	return api.UsageSelector{
		RunID:    in.RunID,
		TaskID:   in.TaskID,
		AgentID:  in.AgentID,
		Kind:     api.UsageKind(in.Kind),
		ToolName: in.ToolName,
		Provider: in.Provider,
		Since:    in.Since,
		Until:    in.Until,
		Limit:    in.Limit,
	}
}

func DeadLetterSelectorToModel(in api.DeadLetterSelector) model.DeadLetterSelector {
	return model.DeadLetterSelector{
		RunID:  in.RunID,
		TaskID: in.TaskID,
		Since:  in.Since,
		Until:  in.Until,
		Limit:  in.Limit,
	}
}

func DeadLetterSelectorFromModel(in model.DeadLetterSelector) api.DeadLetterSelector {
	return api.DeadLetterSelector{
		RunID:  in.RunID,
		TaskID: in.TaskID,
		Since:  in.Since,
		Until:  in.Until,
		Limit:  in.Limit,
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

func RunsToModel(in []api.Run) []model.Run {
	if in == nil {
		return nil
	}
	out := make([]model.Run, len(in))
	for i, item := range in {
		out[i] = RunToModel(item)
	}
	return out
}

func ResumeTokensFromModel(in []model.ResumeToken) []api.ResumeToken {
	if in == nil {
		return nil
	}
	out := make([]api.ResumeToken, len(in))
	for i, item := range in {
		out[i] = ResumeTokenFromModel(item)
	}
	return out
}

func ResumeTokensToModel(in []api.ResumeToken) []model.ResumeToken {
	if in == nil {
		return nil
	}
	out := make([]model.ResumeToken, len(in))
	for i, item := range in {
		out[i] = ResumeTokenToModel(item)
	}
	return out
}

func UsageRecordToModel(in api.UsageRecord) model.UsageRecord {
	return model.UsageRecord{
		PricingState:          model.UsagePricingState(in.PricingState),
		ID:                    in.ID,
		RunID:                 in.RunID,
		TaskID:                in.TaskID,
		AgentID:               in.AgentID,
		Kind:                  model.UsageKind(in.Kind),
		Provider:              in.Provider,
		Model:                 in.Model,
		ToolName:              in.ToolName,
		InputTokens:           in.InputTokens,
		OutputTokens:          in.OutputTokens,
		CachedInputTokens:     in.CachedInputTokens,
		CacheWriteInputTokens: in.CacheWriteInputTokens,
		TotalTokens:           in.TotalTokens,
		ToolCalls:             in.ToolCalls,
		Steps:                 in.Steps,
		DurationMS:            in.DurationMS,
		Credits:               in.Credits,
		CreditsKind:           in.CreditsKind,
		Metadata:              stringMapToModel(in.Metadata),
		CreatedAt:             in.CreatedAt,
	}
}

func UsageRecordFromModel(in model.UsageRecord) api.UsageRecord {
	return api.UsageRecord{
		PricingState:          api.UsagePricingState(in.PricingState),
		ID:                    in.ID,
		RunID:                 in.RunID,
		TaskID:                in.TaskID,
		AgentID:               in.AgentID,
		Kind:                  api.UsageKind(in.Kind),
		Provider:              in.Provider,
		Model:                 in.Model,
		ToolName:              in.ToolName,
		InputTokens:           in.InputTokens,
		OutputTokens:          in.OutputTokens,
		CachedInputTokens:     in.CachedInputTokens,
		CacheWriteInputTokens: in.CacheWriteInputTokens,
		TotalTokens:           in.TotalTokens,
		ToolCalls:             in.ToolCalls,
		Steps:                 in.Steps,
		DurationMS:            in.DurationMS,
		Credits:               in.Credits,
		CreditsKind:           in.CreditsKind,
		Metadata:              stringMapFromModel(in.Metadata),
		CreatedAt:             in.CreatedAt,
	}
}

func UsageRecordsToModel(in []api.UsageRecord) []model.UsageRecord {
	if in == nil {
		return nil
	}
	out := make([]model.UsageRecord, len(in))
	for i, item := range in {
		out[i] = UsageRecordToModel(item)
	}
	return out
}

func UsageRecordsFromModel(in []model.UsageRecord) []api.UsageRecord {
	if in == nil {
		return nil
	}
	out := make([]api.UsageRecord, len(in))
	for i, item := range in {
		out[i] = UsageRecordFromModel(item)
	}
	return out
}

func DeadLetterEntryToModel(in api.DeadLetterEntry) model.DeadLetterEntry {
	return model.DeadLetterEntry{
		ID:         in.ID,
		EnvelopeID: in.EnvelopeID,
		RunID:      in.RunID,
		TaskID:     in.TaskID,
		Reason:     in.Reason,
		Attempts:   in.Attempts,
		Envelope:   TaskEnvelopeToModel(in.Envelope),
		Payload:    anyMapToModel(in.Payload),
		CreatedAt:  in.CreatedAt,
	}
}

func DeadLetterEntryFromModel(in model.DeadLetterEntry) api.DeadLetterEntry {
	return api.DeadLetterEntry{
		ID:         in.ID,
		EnvelopeID: in.EnvelopeID,
		RunID:      in.RunID,
		TaskID:     in.TaskID,
		Reason:     in.Reason,
		Attempts:   in.Attempts,
		Envelope:   TaskEnvelopeFromModel(in.Envelope),
		Payload:    anyMapFromModel(in.Payload),
		CreatedAt:  in.CreatedAt,
	}
}

func DeadLetterEntriesToModel(in []api.DeadLetterEntry) []model.DeadLetterEntry {
	if in == nil {
		return nil
	}
	out := make([]model.DeadLetterEntry, len(in))
	for i, item := range in {
		out[i] = DeadLetterEntryToModel(item)
	}
	return out
}

func DeadLetterEntriesFromModel(in []model.DeadLetterEntry) []api.DeadLetterEntry {
	if in == nil {
		return nil
	}
	out := make([]api.DeadLetterEntry, len(in))
	for i, item := range in {
		out[i] = DeadLetterEntryFromModel(item)
	}
	return out
}

func CapabilityToModel(in api.Capability) model.Capability {
	return model.Capability{
		Name:             in.Name,
		Version:          in.Version,
		Description:      in.Description,
		AgentID:          in.AgentID,
		InputSchema:      anyMapToModel(in.InputSchema),
		OutputSchema:     anyMapToModel(in.OutputSchema),
		EffectType:       model.ToolEffectType(in.EffectType),
		RiskLevel:        in.RiskLevel,
		Idempotent:       in.Idempotent,
		RequiresApproval: in.RequiresApproval,
		RequiresLease:    in.RequiresLease,
		RequiresPolicy:   in.RequiresPolicy,
		Tags:             cloneStrings(in.Tags),
		Metadata:         stringMapToModel(in.Metadata),
	}
}

func CapabilityFromModel(in model.Capability) api.Capability {
	return api.Capability{
		Name:             in.Name,
		Version:          in.Version,
		Description:      in.Description,
		AgentID:          in.AgentID,
		InputSchema:      anyMapFromModel(in.InputSchema),
		OutputSchema:     anyMapFromModel(in.OutputSchema),
		EffectType:       api.ToolEffectType(in.EffectType),
		RiskLevel:        in.RiskLevel,
		Idempotent:       in.Idempotent,
		RequiresApproval: in.RequiresApproval,
		RequiresLease:    in.RequiresLease,
		RequiresPolicy:   in.RequiresPolicy,
		Tags:             cloneStrings(in.Tags),
		Metadata:         stringMapFromModel(in.Metadata),
	}
}

func CapabilitiesToModel(in []api.Capability) []model.Capability {
	if in == nil {
		return nil
	}
	out := make([]model.Capability, len(in))
	for i, item := range in {
		out[i] = CapabilityToModel(item)
	}
	return out
}

func CapabilitiesFromModel(in []model.Capability) []api.Capability {
	if in == nil {
		return nil
	}
	out := make([]api.Capability, len(in))
	for i, item := range in {
		out[i] = CapabilityFromModel(item)
	}
	return out
}

func AgentProfilesToModel(in []api.AgentProfile) []model.AgentProfile {
	if in == nil {
		return nil
	}
	out := make([]model.AgentProfile, len(in))
	for i, item := range in {
		out[i] = AgentProfileToModel(item)
	}
	return out
}
