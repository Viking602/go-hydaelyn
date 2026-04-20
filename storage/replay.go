package storage

import (
	"github.com/Viking602/go-hydaelyn/internal/blackboard"
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
			if supervisor := memberValue(event.Payload["supervisor"]); supervisor.ID != "" {
				state.Supervisor = supervisor
			}
			if workers := membersValue(event.Payload["workers"]); len(workers) > 0 {
				state.Workers = append([]team.Member{}, workers...)
			}
		case EventTaskScheduled:
			task := team.Task{
				ID:               event.TaskID,
				Title:            stringValue(event.Payload["title"]),
				Input:            stringValue(event.Payload["input"]),
				Status:           team.TaskStatus(statusValue(event.Payload["status"], string(team.TaskStatusPending))),
				Kind:             team.TaskKind(stringValue(event.Payload["kind"])),
				RequiredRole:     team.Role(stringValue(event.Payload["requiredRole"])),
				AssigneeAgentID:  stringValue(event.Payload["assigneeAgent"]),
				FailurePolicy:    team.FailurePolicy(stringValue(event.Payload["failurePolicy"])),
				DependsOn:        stringSlice(event.Payload["dependsOn"]),
				Reads:            stringSlice(event.Payload["reads"]),
				Writes:           stringSlice(event.Payload["writes"]),
				Publish:          outputVisibilitySlice(event.Payload["publish"]),
				Namespace:        stringValue(event.Payload["namespace"]),
				VerifierRequired: boolValue(event.Payload["verifierRequired"]),
				IdempotencyKey:   stringValue(event.Payload["idempotencyKey"]),
				Version:          intValue(event.Payload["taskVersion"]),
				Budget:           budgetValue(event.Payload["budget"]),
			}
			tasks[event.TaskID] = len(state.Tasks)
			state.Tasks = append(state.Tasks, task)
		case EventTaskStarted:
			if idx, ok := tasks[event.TaskID]; ok {
				state.Tasks[idx].Status = team.TaskStatus(statusValue(event.Payload["statusAfter"], string(team.TaskStatusRunning)))
				if version := intValue(event.Payload["taskVersionAfter"]); version > 0 {
					state.Tasks[idx].Version = version
				}
				if key := stringValue(event.Payload["idempotencyKey"]); key != "" {
					state.Tasks[idx].IdempotencyKey = key
				}
				state.Tasks[idx].Attempts = intValue(event.Payload["attempts"])
			}
		case EventTaskInputsMaterialized:
			if idx, ok := tasks[event.TaskID]; ok && state.Tasks[idx].Result == nil {
				state.Tasks[idx].Result = &team.Result{}
			}
		case EventTaskCompleted:
			if idx, ok := tasks[event.TaskID]; ok {
				state.Tasks[idx].Status = team.TaskStatus(statusValue(event.Payload["statusAfter"], statusValue(event.Payload["status"], string(team.TaskStatusCompleted))))
				if version := intValue(event.Payload["taskVersionAfter"]); version > 0 {
					state.Tasks[idx].Version = version
				}
				if key := stringValue(event.Payload["idempotencyKey"]); key != "" {
					state.Tasks[idx].IdempotencyKey = key
				}
				state.Tasks[idx].CompletedBy = stringValue(event.Payload["workerId"])
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
			for _, source := range sourcesValue(event.Payload["sources"]) {
				upsertSource(state.Blackboard, source)
			}
			for _, artifact := range artifactsValue(event.Payload["artifacts"]) {
				upsertArtifact(state.Blackboard, artifact)
			}
			for _, evidence := range evidenceValue(event.Payload["evidence"]) {
				upsertEvidence(state.Blackboard, evidence)
			}
			for _, claim := range claimsValue(event.Payload["claims"]) {
				upsertClaim(state.Blackboard, claim)
			}
			for _, finding := range findingsValue(event.Payload["findings"]) {
				upsertFinding(state.Blackboard, finding)
			}
			for _, exchange := range exchangesValue(event.Payload["exchanges"]) {
				state.Blackboard.UpsertExchange(exchange)
			}
			for _, verification := range verificationsValue(event.Payload["verifications"]) {
				state.Blackboard.UpsertVerification(verification)
			}
		case EventTaskFailed:
			if idx, ok := tasks[event.TaskID]; ok {
				state.Tasks[idx].Status = team.TaskStatus(statusValue(event.Payload["statusAfter"], string(team.TaskStatusFailed)))
				if version := intValue(event.Payload["taskVersionAfter"]); version > 0 {
					state.Tasks[idx].Version = version
				}
				if key := stringValue(event.Payload["idempotencyKey"]); key != "" {
					state.Tasks[idx].IdempotencyKey = key
				}
				state.Tasks[idx].Attempts = intValue(event.Payload["attempts"])
				if errorText := stringValue(event.Payload["error"]); errorText != "" {
					state.Tasks[idx].Error = errorText
					state.Tasks[idx].Result = &team.Result{Error: errorText}
				}
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

func stringMapValue(value any) map[string]string {
	switch current := value.(type) {
	case map[string]string:
		items := make(map[string]string, len(current))
		for key, entry := range current {
			items[key] = entry
		}
		return items
	case map[string]any:
		items := make(map[string]string, len(current))
		for key, entry := range current {
			if text, ok := entry.(string); ok {
				items[key] = text
			}
		}
		if len(items) == 0 {
			return nil
		}
		return items
	default:
		return nil
	}
}

func memberValue(value any) team.Member {
	switch current := value.(type) {
	case map[string]string:
		return team.Member{
			ID:          current["id"],
			Role:        team.Role(current["role"]),
			ProfileName: current["profileName"],
			Metadata:    nil,
		}
	case map[string]any:
		return team.Member{
			ID:          stringValue(current["id"]),
			Role:        team.Role(stringValue(current["role"])),
			ProfileName: stringValue(current["profileName"]),
			Metadata:    stringMapValue(current["metadata"]),
		}
	default:
		return team.Member{}
	}
}

func membersValue(value any) []team.Member {
	switch current := value.(type) {
	case []map[string]string:
		items := make([]team.Member, 0, len(current))
		for _, entry := range current {
			items = append(items, memberValue(entry))
		}
		return items
	case []any:
		items := make([]team.Member, 0, len(current))
		for _, entry := range current {
			member := memberValue(entry)
			if member.ID != "" {
				items = append(items, member)
			}
		}
		return items
	default:
		return nil
	}
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

func sourcesValue(value any) []blackboard.Source {
	items := make([]blackboard.Source, 0)
	for _, payload := range payloadMaps(value) {
		items = append(items, blackboard.Source{
			ID:       stringValue(payload["id"]),
			TaskID:   stringValue(payload["taskId"]),
			Title:    stringValue(payload["title"]),
			Metadata: stringMapValue(payload["metadata"]),
		})
	}
	return items
}

func artifactsValue(value any) []blackboard.Artifact {
	items := make([]blackboard.Artifact, 0)
	for _, payload := range payloadMaps(value) {
		items = append(items, blackboard.Artifact{
			ID:       stringValue(payload["id"]),
			TaskID:   stringValue(payload["taskId"]),
			Name:     stringValue(payload["name"]),
			Content:  stringValue(payload["content"]),
			Metadata: stringMapValue(payload["metadata"]),
		})
	}
	return items
}

func evidenceValue(value any) []blackboard.Evidence {
	items := make([]blackboard.Evidence, 0)
	for _, payload := range payloadMaps(value) {
		items = append(items, blackboard.Evidence{
			ID:         stringValue(payload["id"]),
			TaskID:     stringValue(payload["taskId"]),
			SourceID:   stringValue(payload["sourceId"]),
			ArtifactID: stringValue(payload["artifactId"]),
			Summary:    stringValue(payload["summary"]),
			Snippet:    stringValue(payload["snippet"]),
			Score:      floatValue(payload["score"]),
		})
	}
	return items
}

func claimsValue(value any) []blackboard.Claim {
	items := make([]blackboard.Claim, 0)
	for _, payload := range payloadMaps(value) {
		items = append(items, blackboard.Claim{
			ID:          stringValue(payload["id"]),
			TaskID:      stringValue(payload["taskId"]),
			Summary:     stringValue(payload["summary"]),
			EvidenceIDs: stringSlice(payload["evidenceIds"]),
			Confidence:  floatValue(payload["confidence"]),
		})
	}
	return items
}

func findingsValue(value any) []blackboard.Finding {
	items := make([]blackboard.Finding, 0)
	for _, payload := range payloadMaps(value) {
		items = append(items, blackboard.Finding{
			ID:          stringValue(payload["id"]),
			TaskID:      stringValue(payload["taskId"]),
			Summary:     stringValue(payload["summary"]),
			ClaimIDs:    stringSlice(payload["claimIds"]),
			EvidenceIDs: stringSlice(payload["evidenceIds"]),
			Confidence:  floatValue(payload["confidence"]),
		})
	}
	return items
}

func exchangesValue(value any) []blackboard.Exchange {
	result := []blackboard.Exchange{}
	for _, payload := range payloadMaps(value) {
		result = append(result, blackboard.Exchange{
			ID:          stringValue(payload["id"]),
			Key:         stringValue(payload["key"]),
			Namespace:   stringValue(payload["namespace"]),
			TaskID:      stringValue(payload["taskId"]),
			Version:     intValue(payload["version"]),
			ETag:        stringValue(payload["etag"]),
			ValueType:   blackboard.ExchangeValueType(stringValue(payload["valueType"])),
			Text:        stringValue(payload["text"]),
			Structured:  structuredMap(payload["structured"]),
			ArtifactIDs: stringSlice(payload["artifactIds"]),
			ClaimIDs:    stringSlice(payload["claimIds"]),
			FindingIDs:  stringSlice(payload["findingIds"]),
			Metadata:    stringMapValue(payload["metadata"]),
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

func boolValue(value any) bool {
	current, _ := value.(bool)
	return current
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

func upsertSource(state *blackboard.State, source blackboard.Source) {
	for idx := range state.Sources {
		if state.Sources[idx].ID == source.ID {
			state.Sources[idx] = source
			return
		}
	}
	state.Sources = append(state.Sources, source)
}

func upsertArtifact(state *blackboard.State, artifact blackboard.Artifact) {
	for idx := range state.Artifacts {
		if state.Artifacts[idx].ID == artifact.ID {
			state.Artifacts[idx] = artifact
			return
		}
	}
	state.Artifacts = append(state.Artifacts, artifact)
}

func upsertEvidence(state *blackboard.State, evidence blackboard.Evidence) {
	for idx := range state.Evidence {
		if state.Evidence[idx].ID == evidence.ID {
			state.Evidence[idx] = evidence
			return
		}
	}
	state.Evidence = append(state.Evidence, evidence)
}

func upsertClaim(state *blackboard.State, claim blackboard.Claim) {
	for idx := range state.Claims {
		if state.Claims[idx].ID == claim.ID {
			state.Claims[idx] = claim
			return
		}
	}
	state.Claims = append(state.Claims, claim)
}

func upsertFinding(state *blackboard.State, finding blackboard.Finding) {
	for idx := range state.Findings {
		if state.Findings[idx].ID == finding.ID {
			state.Findings[idx] = finding
			return
		}
	}
	state.Findings = append(state.Findings, finding)
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
