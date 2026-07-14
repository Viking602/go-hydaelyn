package adapter

import (
	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/internal/core/model"
)

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

func TaskToModel(in api.Task) model.Task {
	return model.Task{
		ID:                 in.ID,
		RunID:              in.RunID,
		ParentTaskID:       in.ParentTaskID,
		Type:               model.TaskType(in.Type),
		Goal:               in.Goal,
		Input:              cloneBytes(in.Input),
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
		Budget:             TaskBudgetPtrToModel(in.Budget),
		InputSchema:        cloneBytes(in.InputSchema),
		OutputSchema:       cloneBytes(in.OutputSchema),
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
		Input:              cloneBytes(in.Input),
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
		Budget:             TaskBudgetPtrFromModel(in.Budget),
		InputSchema:        cloneBytes(in.InputSchema),
		OutputSchema:       cloneBytes(in.OutputSchema),
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

func TaskBudgetPtrToModel(in *api.TaskBudget) *model.TaskBudget {
	if in == nil {
		return nil
	}
	return &model.TaskBudget{
		MaxTokens:    in.MaxTokens,
		MaxWallClock: in.MaxWallClock,
		MaxToolCalls: in.MaxToolCalls,
		MaxSteps:     in.MaxSteps,
	}
}

func TaskBudgetPtrFromModel(in *model.TaskBudget) *api.TaskBudget {
	if in == nil {
		return nil
	}
	return &api.TaskBudget{
		MaxTokens:    in.MaxTokens,
		MaxWallClock: in.MaxWallClock,
		MaxToolCalls: in.MaxToolCalls,
		MaxSteps:     in.MaxSteps,
	}
}
