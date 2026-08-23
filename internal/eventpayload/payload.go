package eventpayload

import (
	"maps"
	"slices"

	"github.com/Viking602/venat/api"
)

func Task(task api.Task) map[string]any {
	return map[string]any{
		"taskId":             task.ID,
		"runId":              task.RunID,
		"parentTaskId":       task.ParentTaskID,
		"type":               task.Type,
		"goal":               task.Goal,
		"input":              slices.Clone(task.Input),
		"assignedAgentId":    task.AssignedAgentID,
		"ownerAgentId":       task.OwnerAgentID,
		"ownerComponent":     task.OwnerComponent,
		"status":             task.Status,
		"version":            task.Version,
		"attempts":           task.Attempts,
		"handoffCount":       task.HandoffCount,
		"ownerHistory":       slices.Clone(task.OwnerHistory),
		"allowsAction":       task.AllowsAction,
		"tags":               slices.Clone(task.Tags),
		"completionCriteria": slices.Clone(task.CompletionCriteria),
		"dependsOn":          slices.Clone(task.DependsOn),
		"awaitMode":          task.AwaitMode,
		"awaitQuorum":        task.AwaitQuorum,
		"onDependencyFailed": task.OnDependencyFailed,
		"readSelectors":      slices.Clone(task.ReadSelectors),
		"writeTargets":       slices.Clone(task.WriteTargets),
		"retryPolicy":        task.RetryPolicy,
		"policyDecisions":    slices.Clone(task.PolicyDecisions),
		"result":             task.Result,
		"error":              task.Error,
		"budget":             task.Budget,
		"inputSchema":        slices.Clone(task.InputSchema),
		"outputSchema":       slices.Clone(task.OutputSchema),
		"resourceClaims":     slices.Clone(task.ResourceClaims),
		"createdAt":          task.CreatedAt,
		"updatedAt":          task.UpdatedAt,
	}
}

func Run(run api.Run) map[string]any {
	return map[string]any{
		"id":           run.ID,
		"status":       string(run.Status),
		"request":      run.Request,
		"rootTaskId":   run.RootTaskID,
		"agentVersion": run.AgentVersion,
		"metadata":     maps.Clone(run.Metadata),
		"createdAt":    run.CreatedAt,
		"updatedAt":    run.UpdatedAt,
	}
}

func Envelope(env api.TaskEnvelope) map[string]any {
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

func RetryPolicy(policy api.RetryPolicy) map[string]any {
	if policy.MaxAttempts == 0 && policy.Backoff == 0 && policy.MaxBackoff == 0 {
		return nil
	}
	return map[string]any{
		"maxAttempts": policy.MaxAttempts,
		"backoff":     policy.Backoff,
		"maxBackoff":  policy.MaxBackoff,
	}
}

func CloneAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
