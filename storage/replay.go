package storage

import (
	"github.com/Viking602/go-hydaelyn/blackboard"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/team"
)

func ReplayTeam(events []Event) team.RunState {
	state := team.RunState{}
	tasks := map[string]int{}
	for _, event := range events {
		if state.ID == "" {
			state.ID = event.TeamID
		}
		switch event.Type {
		case EventTeamStarted:
			state.Pattern, _ = event.Payload["pattern"].(string)
			state.Status = team.StatusRunning
			if phase, ok := event.Payload["phase"].(string); ok && phase != "" {
				state.Phase = team.Phase(phase)
			}
			if sup, ok := event.Payload["supervisor"].(map[string]string); ok {
				state.Supervisor = team.Member{
					ID:          sup["id"],
					Role:        team.Role(sup["role"]),
					ProfileName: sup["profileName"],
				}
			}
			if workers, ok := event.Payload["workers"].([]map[string]string); ok {
				for _, w := range workers {
					state.Workers = append(state.Workers, team.Member{
						ID:          w["id"],
						Role:        team.Role(w["role"]),
						ProfileName: w["profileName"],
					})
				}
			}
		case EventTaskScheduled:
			task := team.Task{
				ID:              event.TaskID,
				Title:           stringValue(event.Payload["title"]),
				Input:           stringValue(event.Payload["input"]),
				Status:          team.TaskStatus(statusValue(event.Payload["status"], string(team.TaskStatusPending))),
				Kind:            team.TaskKind(stringValue(event.Payload["kind"])),
				RequiredRole:    team.Role(stringValue(event.Payload["requiredRole"])),
				AssigneeAgentID: stringValue(event.Payload["assigneeAgent"]),
				FailurePolicy:   team.FailurePolicy(stringValue(event.Payload["failurePolicy"])),
				DependsOn:       stringSlice(event.Payload["dependsOn"]),
				Reads:           stringSlice(event.Payload["reads"]),
				Writes:          stringSlice(event.Payload["writes"]),
				Publish:         outputVisibilitySlice(event.Payload["publish"]),
				Budget:          budgetValue(event.Payload["budget"]),
			}
			tasks[event.TaskID] = len(state.Tasks)
			state.Tasks = append(state.Tasks, task)
		case EventTaskStarted:
			if idx, ok := tasks[event.TaskID]; ok {
				state.Tasks[idx].Status = team.TaskStatusRunning
				state.Tasks[idx].Attempts = intValue(event.Payload["attempts"])
			}
		case EventTaskInputsMaterialized:
			if idx, ok := tasks[event.TaskID]; ok && state.Tasks[idx].Result == nil {
				state.Tasks[idx].Result = &team.Result{}
			}
		case EventTaskCompleted:
			if idx, ok := tasks[event.TaskID]; ok {
				state.Tasks[idx].Status = team.TaskStatus(statusValue(event.Payload["status"], string(team.TaskStatusCompleted)))
				state.Tasks[idx].Result = &team.Result{
					Summary:       stringValue(event.Payload["summary"]),
					Structured:    structuredMap(event.Payload["structured"]),
					ArtifactIDs:   stringSlice(event.Payload["artifactIds"]),
					Usage:         usageValue(event.Payload["usage"]),
					ToolCallCount: intValue(event.Payload["toolCallCount"]),
				}
				state.Tasks[idx].Attempts = intValue(event.Payload["attempts"])
			}
		case EventTaskOutputsPublished:
			if state.Blackboard == nil {
				state.Blackboard = &blackboard.State{}
			}
			for _, exchange := range exchangesValue(event.Payload["exchanges"]) {
				state.Blackboard.UpsertExchange(exchange)
			}
			for _, verification := range verificationsValue(event.Payload["verifications"]) {
				state.Blackboard.UpsertVerification(verification)
			}
		case EventTaskFailed:
			if idx, ok := tasks[event.TaskID]; ok {
				state.Tasks[idx].Status = team.TaskStatusFailed
			}
		case EventApprovalRequested:
			state.Status = team.StatusPaused
			state.Result = &team.Result{Error: stringValue(event.Payload["reason"])}
		case EventCheckpointSaved:
			state.Status = team.Status(statusValue(event.Payload["status"], string(state.Status)))
			if state.Result == nil {
				state.Result = &team.Result{}
			}
			state.Result.Summary = stringValue(event.Payload["summary"])
			state.Result.Error = stringValue(event.Payload["error"])
			state.Result.Structured = structuredMap(event.Payload["structured"])
			state.Result.ArtifactIDs = stringSlice(event.Payload["artifactIds"])
		case EventTeamCompleted:
			state.Status = team.StatusCompleted
			state.Phase = team.PhaseComplete
			state.Result = &team.Result{
				Summary:       stringValue(event.Payload["summary"]),
				Structured:    structuredMap(event.Payload["structured"]),
				ArtifactIDs:   stringSlice(event.Payload["artifactIds"]),
				Usage:         usageValue(event.Payload["usage"]),
				ToolCallCount: intValue(event.Payload["toolCallCount"]),
			}
		}
	}
	return state
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func statusValue(value any, fallback string) string {
	if text, ok := value.(string); ok && text != "" {
		return text
	}
	return fallback
}

func stringSlice(value any) []string {
	if value == nil {
		return nil
	}
	switch items := value.(type) {
	case []string:
		return append([]string{}, items...)
	case []any:
		result := make([]string, 0, len(items))
		for _, item := range items {
			if text, ok := item.(string); ok {
				result = append(result, text)
			}
		}
		return result
	default:
		return nil
	}
}

func outputVisibilitySlice(value any) []team.OutputVisibility {
	items := stringSlice(value)
	if len(items) == 0 {
		return nil
	}
	result := make([]team.OutputVisibility, 0, len(items))
	for _, item := range items {
		result = append(result, team.OutputVisibility(item))
	}
	return result
}

func structuredMap(value any) map[string]any {
	if value == nil {
		return nil
	}
	items, ok := value.(map[string]any)
	if !ok || len(items) == 0 {
		return nil
	}
	result := make(map[string]any, len(items))
	for key, item := range items {
		result[key] = item
	}
	return result
}

func exchangesValue(value any) []blackboard.Exchange {
	result := []blackboard.Exchange{}
	for _, payload := range payloadMaps(value) {
		result = append(result, blackboard.Exchange{
			ID:          stringValue(payload["id"]),
			Key:         stringValue(payload["key"]),
			TaskID:      stringValue(payload["taskId"]),
			ValueType:   blackboard.ExchangeValueType(stringValue(payload["valueType"])),
			Text:        stringValue(payload["text"]),
			Structured:  structuredMap(payload["structured"]),
			ArtifactIDs: stringSlice(payload["artifactIds"]),
			ClaimIDs:    stringSlice(payload["claimIds"]),
			FindingIDs:  stringSlice(payload["findingIds"]),
		})
	}
	return result
}

func verificationsValue(value any) []blackboard.VerificationResult {
	result := []blackboard.VerificationResult{}
	for _, payload := range payloadMaps(value) {
		result = append(result, blackboard.VerificationResult{
			ClaimID:     stringValue(payload["claimId"]),
			Status:      blackboard.VerificationStatus(stringValue(payload["status"])),
			Confidence:  floatValue(payload["confidence"]),
			EvidenceIDs: stringSlice(payload["evidenceIds"]),
			Rationale:   stringValue(payload["rationale"]),
		})
	}
	return result
}

func payloadMaps(value any) []map[string]any {
	switch items := value.(type) {
	case []map[string]any:
		return append([]map[string]any{}, items...)
	case []any:
		result := make([]map[string]any, 0, len(items))
		for _, item := range items {
			payload, ok := item.(map[string]any)
			if ok {
				result = append(result, payload)
			}
		}
		return result
	default:
		return nil
	}
}

func floatValue(value any) float64 {
	switch current := value.(type) {
	case float64:
		return current
	case float32:
		return float64(current)
	case int:
		return float64(current)
	default:
		return 0
	}
}

func intValue(value any) int {
	switch current := value.(type) {
	case int:
		return current
	case int64:
		return int(current)
	case float64:
		return int(current)
	case float32:
		return int(current)
	default:
		return 0
	}
}

func budgetValue(value any) team.Budget {
	items := structuredMap(value)
	if len(items) == 0 {
		return team.Budget{}
	}
	return team.Budget{
		Tokens:    intValue(items["tokens"]),
		ToolCalls: intValue(items["toolCalls"]),
	}
}

func usageValue(value any) provider.Usage {
	items := structuredMap(value)
	if len(items) == 0 {
		return provider.Usage{}
	}
	return provider.Usage{
		InputTokens:  intValue(items["inputTokens"]),
		OutputTokens: intValue(items["outputTokens"]),
		TotalTokens:  intValue(items["totalTokens"]),
	}
}
