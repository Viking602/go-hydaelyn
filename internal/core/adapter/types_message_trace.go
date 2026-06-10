package adapter

import (
	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/internal/core/model"
)

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
