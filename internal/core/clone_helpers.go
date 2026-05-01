package core

import (
	"maps"
	"slices"
)

func taskEventPayload(task Task) map[string]any {
	return map[string]any{
		"taskId":             task.ID,
		"runId":              task.RunID,
		"parentTaskId":       task.ParentTaskID,
		"type":               string(task.Type),
		"goal":               task.Goal,
		"status":             string(task.Status),
		"version":            task.Version,
		"attempts":           task.Attempts,
		"handoffCount":       task.HandoffCount,
		"assignedAgentId":    task.AssignedAgentID,
		"ownerAgentId":       task.OwnerAgentID,
		"ownerComponent":     task.OwnerComponent,
		"completionCriteria": slices.Clone(task.CompletionCriteria),
		"dependsOn":          slices.Clone(task.DependsOn),
		"retryPolicy":        retryPolicyPayload(task.RetryPolicy),
		"createdAt":          task.CreatedAt,
		"updatedAt":          task.UpdatedAt,
	}
}

func runPayload(run Run) map[string]any {
	return map[string]any{
		"id":         run.ID,
		"status":     string(run.Status),
		"request":    run.Request,
		"rootTaskId": run.RootTaskID,
		"metadata":   maps.Clone(run.Metadata),
		"createdAt":  run.CreatedAt,
		"updatedAt":  run.UpdatedAt,
	}
}

func envPayload(env TaskEnvelope) map[string]any {
	return map[string]any{
		"envelopeId":      env.ID,
		"runId":           env.RunID,
		"taskId":          env.TaskID,
		"targetAgentId":   env.TargetAgentID,
		"targetComponent": env.TargetComponent,
		"type":            env.Type,
		"status":          env.Status,
		"taskVersion":     env.TaskVersion,
		"attempts":        env.Attempts,
		"createdAt":       env.CreatedAt,
		"deliveredAt":     env.DeliveredAt,
	}
}

func retryPolicyPayload(policy RetryPolicy) map[string]any {
	if policy.MaxAttempts == 0 && policy.Backoff == 0 {
		return nil
	}
	return map[string]any{
		"maxAttempts": policy.MaxAttempts,
		"backoff":     policy.Backoff,
	}
}

func cloneAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
