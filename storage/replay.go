package storage

import (
	"github.com/Viking602/go-hydaelyn/internal/blackboard"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/team"
)

func ReplayTeam(events []Event) team.RunState {
	state := team.RunState{}
	ctx := replayContext{state: &state, tasks: map[string]int{}}
	handlers := replayEventHandlers(ctx)
	for _, event := range events {
		if state.ID == "" {
			state.ID = event.TeamID
		}
		if handler := handlers[event.Type]; handler != nil {
			handler(event)
		}
	}
	return state
}

type replayContext struct {
	state *team.RunState
	tasks map[string]int
}

func replayEventHandlers(ctx replayContext) map[EventType]func(Event) {
	return map[EventType]func(Event){
		EventTeamStarted: func(event Event) {
			replayTeamStarted(ctx.state, event)
		},
		EventTodoPlanned: func(event Event) {
			replayTodoPlanned(ctx.state, event)
		},
		EventTodoClaimed: func(event Event) {
			ensureTaskBoard(ctx.state)
			upsertTodo(ctx.state.TaskBoard, todoValue(event))
		},
		EventTaskScheduled: func(event Event) {
			replayTaskScheduled(ctx.state, ctx.tasks, event)
		},
		EventTaskStarted: func(event Event) {
			replayTaskStarted(ctx.state, ctx.tasks, event)
		},
		EventTaskInputsMaterialized: func(event Event) {
			replayTaskInputsMaterialized(ctx.state, ctx.tasks, event)
		},
		EventTaskCompleted: func(event Event) {
			replayTaskCompleted(ctx.state, ctx.tasks, event)
		},
		EventTaskOutputsPublished: func(event Event) {
			replayTaskOutputsPublished(ctx.state, event)
		},
		EventTaskFailed: func(event Event) {
			replayTaskFailed(ctx.state, ctx.tasks, event)
		},
		EventApprovalRequested: func(event Event) {
			replayApprovalRequested(ctx.state, event)
		},
		EventCheckpointSaved: func(event Event) {
			replayCheckpointSaved(ctx.state, event)
		},
		EventTeamCompleted: func(event Event) {
			replayTeamCompleted(ctx.state, event)
		},
	}
}

func replayTeamStarted(state *team.RunState, event Event) {
	state.Pattern, _ = event.Payload["pattern"].(string)
	state.Status = team.StatusRunning
	state.AgentOptions = agentOptionsValue(event.Payload["agentOptions"])
	state.InteractionMode = team.InteractionMode(stringValue(event.Payload["interactionMode"]))
	if phase, ok := event.Payload["phase"].(string); ok && phase != "" {
		state.Phase = team.Phase(phase)
	}
	if supervisor := memberValue(event.Payload["supervisor"]); supervisor.ID != "" {
		state.Supervisor = supervisor
	}
	if workers := membersValue(event.Payload["workers"]); len(workers) > 0 {
		state.Workers = append([]team.Member{}, workers...)
	}
}

func replayTodoPlanned(state *team.RunState, event Event) {
	ensureTaskBoard(state)
	state.TaskBoard.Plan.ID = stringValue(event.Payload["planId"])
	state.TaskBoard.Plan.Goal = stringValue(event.Payload["goal"])
	state.TaskBoard.Plan.CreatedAt = event.RecordedAt
	state.TaskBoard.Plan.UpdatedAt = event.RecordedAt
	if mode := team.InteractionMode(stringValue(event.Payload["mode"])); mode != "" {
		state.InteractionMode = mode
	}
}

func replayTaskScheduled(state *team.RunState, tasks map[string]int, event Event) {
	task := team.Task{
		ID:                  event.TaskID,
		Title:               stringValue(event.Payload["title"]),
		Input:               stringValue(event.Payload["input"]),
		Status:              team.TaskStatus(statusValue(event.Payload["status"], string(team.TaskStatusPending))),
		Kind:                team.TaskKind(stringValue(event.Payload["kind"])),
		Stage:               team.TaskStage(stringValue(event.Payload["stage"])),
		TodoID:              stringValue(event.Payload["todoId"]),
		RequiredRole:        team.Role(stringValue(event.Payload["requiredRole"])),
		AssigneeAgentID:     stringValue(event.Payload["assigneeAgent"]),
		FailurePolicy:       team.FailurePolicy(stringValue(event.Payload["failurePolicy"])),
		DependsOn:           stringSlice(event.Payload["dependsOn"]),
		Reads:               stringSlice(event.Payload["reads"]),
		ReadSelectors:       exchangeSelectorsValue(event.Payload["readSelectors"]),
		Writes:              stringSlice(event.Payload["writes"]),
		Publish:             outputVisibilitySlice(event.Payload["publish"]),
		Namespace:           stringValue(event.Payload["namespace"]),
		VerifierRequired:    boolValue(event.Payload["verifierRequired"]),
		ExpectedReportKind:  team.ReportKind(stringValue(event.Payload["expectedReportKind"])),
		AssistantOutputMode: team.AssistantOutputMode(stringValue(event.Payload["assistantOutputMode"])),
		IdempotencyKey:      stringValue(event.Payload["idempotencyKey"]),
		Version:             intValue(event.Payload["taskVersion"]),
		Budget:              budgetValue(event.Payload["budget"]),
	}
	tasks[event.TaskID] = len(state.Tasks)
	state.Tasks = append(state.Tasks, task)
	applyScheduledTaskToBoard(state, task)
}

func replayTaskInputsMaterialized(state *team.RunState, tasks map[string]int, event Event) {
	idx, ok := tasks[event.TaskID]
	if ok && state.Tasks[idx].Result == nil {
		state.Tasks[idx].Result = &team.Result{}
	}
}

func replayTaskStarted(state *team.RunState, tasks map[string]int, event Event) {
	idx, ok := tasks[event.TaskID]
	if !ok {
		return
	}
	state.Tasks[idx].Status = team.TaskStatus(statusValue(event.Payload["statusAfter"], string(team.TaskStatusRunning)))
	applyTaskLifecycleMetadata(&state.Tasks[idx], event)
	applyStartedTaskToBoard(state, state.Tasks[idx])
}

func replayTaskCompleted(state *team.RunState, tasks map[string]int, event Event) {
	idx, ok := tasks[event.TaskID]
	if !ok {
		return
	}
	state.Tasks[idx].Status = team.TaskStatus(statusValue(event.Payload["statusAfter"], statusValue(event.Payload["status"], string(team.TaskStatusCompleted))))
	applyTaskLifecycleMetadata(&state.Tasks[idx], event)
	state.Tasks[idx].CompletedBy = stringValue(event.Payload["workerId"])
	state.Tasks[idx].Result = &team.Result{
		Summary:       stringValue(event.Payload["summary"]),
		Structured:    structuredMap(event.Payload["structured"]),
		ArtifactIDs:   stringSlice(event.Payload["artifactIds"]),
		Usage:         usageValue(event.Payload["usage"]),
		ToolCallCount: intValue(event.Payload["toolCallCount"]),
	}
	applyCompletedTaskToBoard(state, state.Tasks[idx])
}

func replayTaskOutputsPublished(state *team.RunState, event Event) {
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
}

func replayTaskFailed(state *team.RunState, tasks map[string]int, event Event) {
	idx, ok := tasks[event.TaskID]
	if !ok {
		return
	}
	state.Tasks[idx].Status = team.TaskStatus(statusValue(event.Payload["statusAfter"], string(team.TaskStatusFailed)))
	applyTaskLifecycleMetadata(&state.Tasks[idx], event)
	if errorText := stringValue(event.Payload["error"]); errorText != "" {
		state.Tasks[idx].Error = errorText
		state.Tasks[idx].Result = &team.Result{Error: errorText}
	}
}

func replayApprovalRequested(state *team.RunState, event Event) {
	state.Status = team.StatusPaused
	state.Result = &team.Result{Error: stringValue(event.Payload["reason"])}
}

func applyTaskLifecycleMetadata(task *team.Task, event Event) {
	if version := intValue(event.Payload["taskVersionAfter"]); version > 0 {
		task.Version = version
	}
	if key := stringValue(event.Payload["idempotencyKey"]); key != "" {
		task.IdempotencyKey = key
	}
	task.Attempts = intValue(event.Payload["attempts"])
}

func replayCheckpointSaved(state *team.RunState, event Event) {
	state.Status = team.Status(statusValue(event.Payload["status"], string(state.Status)))
	if state.Result == nil {
		state.Result = &team.Result{}
	}
	state.Result.Summary = stringValue(event.Payload["summary"])
	state.Result.Error = stringValue(event.Payload["error"])
	state.Result.Structured = structuredMap(event.Payload["structured"])
	state.Result.ArtifactIDs = stringSlice(event.Payload["artifactIds"])
}

func replayTeamCompleted(state *team.RunState, event Event) {
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

func agentOptionsValue(value any) team.AgentOptions {
	switch current := value.(type) {
	case map[string]any:
		return team.AgentOptions{
			MaxIterations:        intValue(current["maxIterations"]),
			StopSequences:        stringSlice(current["stopSequences"]),
			ThinkingBudget:       intValue(current["thinkingBudget"]),
			ExtraBody:            structuredMap(current["extraBody"]),
			OutputGuardrails:     stringSlice(current["outputGuardrails"]),
			TeamOutputGuardrails: stringSlice(current["teamOutputGuardrails"]),
			AssistantOutputMode:  team.AssistantOutputMode(stringValue(current["assistantOutputMode"])),
		}
	default:
		return team.AgentOptions{}
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

func ensureTaskBoard(state *team.RunState) {
	if state.TaskBoard != nil {
		return
	}
	state.TaskBoard = &team.TaskBoard{Plan: team.TodoPlan{ID: state.ID + "-todo-plan"}}
}

func todoValue(event Event) team.TodoItem {
	status := team.TodoStatus(statusValue(event.Payload["status"], string(team.TodoStatusClaimed)))
	return team.TodoItem{
		ID:                   stringValue(event.Payload["todoId"]),
		Title:                stringValue(event.Payload["title"]),
		Domain:               stringValue(event.Payload["domain"]),
		RequiredCapabilities: stringSlice(event.Payload["requiredCapabilities"]),
		Priority:             team.TodoPriority(stringValue(event.Payload["priority"])),
		ExpectedReportKind:   team.ReportKind(stringValue(event.Payload["expectedReportKind"])),
		VerificationPolicy:   todoVerificationPolicyValue(event.Payload["verificationPolicy"]),
		Status:               status,
		PrimaryAgentID:       stringValue(event.Payload["agentId"]),
		TaskID:               event.TaskID,
		ClaimedAt:            event.RecordedAt,
		UpdatedAt:            event.RecordedAt,
	}
}

func upsertTodo(board *team.TaskBoard, todo team.TodoItem) {
	if board == nil || todo.ID == "" {
		return
	}
	for idx := range board.Plan.Items {
		if board.Plan.Items[idx].ID == todo.ID {
			mergeTodo(&board.Plan.Items[idx], todo)
			board.Plan.UpdatedAt = todo.UpdatedAt
			return
		}
	}
	board.Plan.Items = append(board.Plan.Items, todo)
	board.Plan.UpdatedAt = todo.UpdatedAt
}

func mergeTodo(existing *team.TodoItem, incoming team.TodoItem) {
	if incoming.Title != "" {
		existing.Title = incoming.Title
	}
	if incoming.Input != "" {
		existing.Input = incoming.Input
	}
	if incoming.Domain != "" {
		existing.Domain = incoming.Domain
	}
	if len(incoming.RequiredCapabilities) > 0 {
		existing.RequiredCapabilities = append([]string{}, incoming.RequiredCapabilities...)
	}
	if incoming.Priority != "" {
		existing.Priority = incoming.Priority
	}
	if incoming.ExpectedReportKind != "" {
		existing.ExpectedReportKind = incoming.ExpectedReportKind
	}
	if incoming.VerificationPolicy != (team.TodoVerificationPolicy{}) {
		existing.VerificationPolicy = incoming.VerificationPolicy
	}
	if incoming.Status != "" {
		existing.Status = incoming.Status
	}
	if incoming.PrimaryAgentID != "" {
		existing.PrimaryAgentID = incoming.PrimaryAgentID
	}
	if incoming.TaskID != "" {
		existing.TaskID = incoming.TaskID
	}
	if !incoming.ClaimedAt.IsZero() {
		existing.ClaimedAt = incoming.ClaimedAt
	}
	if !incoming.UpdatedAt.IsZero() {
		existing.UpdatedAt = incoming.UpdatedAt
	}
}

func applyScheduledTaskToBoard(state *team.RunState, task team.Task) {
	if state.TaskBoard == nil || task.TodoID == "" {
		return
	}
	idx := todoIndex(state.TaskBoard, task.TodoID)
	if idx < 0 {
		return
	}
	todo := &state.TaskBoard.Plan.Items[idx]
	switch {
	case task.Kind == team.TaskKindVerify || task.Stage == team.TaskStageReview || task.Stage == team.TaskStageVerify:
		todo.ReviewerAgentIDs = appendUniqueString(todo.ReviewerAgentIDs, task.EffectiveAssigneeAgentID())
		todo.ReviewTaskIDs = appendUniqueString(todo.ReviewTaskIDs, task.ID)
		if todo.Status != team.TodoStatusVerified {
			todo.Status = team.TodoStatusReviewing
		}
	default:
		if todo.TaskID == "" {
			todo.TaskID = task.ID
		}
	}
	todo.UpdatedAt = state.TaskBoard.Plan.UpdatedAt
}

func applyStartedTaskToBoard(state *team.RunState, task team.Task) {
	if state.TaskBoard == nil || task.TodoID == "" || task.Kind == team.TaskKindVerify {
		return
	}
	idx := todoIndex(state.TaskBoard, task.TodoID)
	if idx < 0 {
		return
	}
	state.TaskBoard.Plan.Items[idx].Status = team.TodoStatusRunning
}

func applyCompletedTaskToBoard(state *team.RunState, task team.Task) {
	if state.TaskBoard == nil || task.TodoID == "" {
		return
	}
	idx := todoIndex(state.TaskBoard, task.TodoID)
	if idx < 0 {
		return
	}
	switch {
	case task.Kind == team.TaskKindVerify || task.Stage == team.TaskStageReview || task.Stage == team.TaskStageVerify:
		state.TaskBoard.Plan.Items[idx].Status = team.TodoStatusVerified
	default:
		state.TaskBoard.Plan.Items[idx].Status = team.TodoStatusCompleted
	}
}

func todoIndex(board *team.TaskBoard, todoID string) int {
	for idx, todo := range board.Plan.Items {
		if todo.ID == todoID {
			return idx
		}
	}
	return -1
}

func appendUniqueString(items []string, value string) []string {
	if value == "" {
		return items
	}
	for _, item := range items {
		if item == value {
			return items
		}
	}
	return append(items, value)
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

func exchangeSelectorsValue(value any) []blackboard.ExchangeSelector {
	switch current := value.(type) {
	case []blackboard.ExchangeSelector:
		return append([]blackboard.ExchangeSelector{}, current...)
	case []map[string]any:
		result := make([]blackboard.ExchangeSelector, 0, len(current))
		for _, item := range current {
			result = append(result, exchangeSelectorValue(item))
		}
		return result
	case []any:
		result := make([]blackboard.ExchangeSelector, 0, len(current))
		for _, item := range current {
			if selector, ok := item.(blackboard.ExchangeSelector); ok {
				result = append(result, selector)
				continue
			}
			if payload, ok := item.(map[string]any); ok {
				result = append(result, exchangeSelectorValue(payload))
			}
		}
		return result
	default:
		return nil
	}
}

func exchangeSelectorValue(payload map[string]any) blackboard.ExchangeSelector {
	return blackboard.ExchangeSelector{
		Keys:              stringSlice(payload["keys"]),
		Namespaces:        stringSlice(payload["namespaces"]),
		TaskIDs:           stringSlice(payload["taskIds"]),
		ValueTypes:        exchangeValueTypeSlice(payload["valueTypes"]),
		ClaimIDs:          stringSlice(payload["claimIds"]),
		FindingIDs:        stringSlice(payload["findingIds"]),
		ArtifactIDs:       stringSlice(payload["artifactIds"]),
		RequireVerified:   boolValue(payload["requireVerified"]),
		MinConfidence:     floatValue(payload["minConfidence"]),
		Limit:             intValue(payload["limit"]),
		IncludeText:       boolValue(payload["includeText"]),
		IncludeStructured: boolValue(payload["includeStructured"]),
		IncludeArtifacts:  boolValue(payload["includeArtifacts"]),
		Required:          boolValue(payload["required"]),
		Label:             stringValue(payload["label"]),
	}
}

func exchangeValueTypeSlice(value any) []blackboard.ExchangeValueType {
	items := stringSlice(value)
	if len(items) == 0 {
		return nil
	}
	result := make([]blackboard.ExchangeValueType, 0, len(items))
	for _, item := range items {
		result = append(result, blackboard.ExchangeValueType(item))
	}
	return result
}

func todoVerificationPolicyValue(value any) team.TodoVerificationPolicy {
	payload := structuredMap(value)
	if len(payload) == 0 {
		return team.TodoVerificationPolicy{}
	}
	return team.TodoVerificationPolicy{
		Required:      boolValue(payload["required"]),
		Mode:          stringValue(payload["mode"]),
		MinConfidence: floatValue(payload["minConfidence"]),
		Reviewers:     intValue(payload["reviewers"]),
	}
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
