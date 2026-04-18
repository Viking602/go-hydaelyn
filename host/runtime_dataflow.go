package host

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/Viking602/go-hydaelyn/blackboard"
	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/team"
)

type taskExecution struct {
	Messages    []message.Message
	Usage       provider.Usage
	ToolResults []message.ToolResult
}

func (r *Runtime) materializeTaskInputs(state team.RunState, task team.Task) ([]blackboard.Exchange, string) {
	if len(task.Reads) == 0 || state.Blackboard == nil {
		return nil, ""
	}
	collected := make([]blackboard.Exchange, 0, len(task.Reads))
	for _, key := range task.Reads {
		collected = append(collected, materializeReadKey(state.Blackboard, task, key)...)
	}
	return collected, formatMaterializedInputs(task, state.Blackboard)
}

func materializeReadKey(board *blackboard.State, task team.Task, key string) []blackboard.Exchange {
	if board == nil {
		return nil
	}
	items := filterMaterializedExchanges(task, board.ExchangesForKey(key))
	if len(items) > 0 {
		return items
	}
	if key != "supported_findings" {
		return nil
	}
	if task.Kind == team.TaskKindSynthesize && task.VerifierRequired {
		return nil
	}
	supported := board.SupportedFindings()
	result := make([]blackboard.Exchange, 0, len(supported))
	for _, finding := range supported {
		result = append(result, blackboard.Exchange{
			Key:        key,
			TaskID:     finding.TaskID,
			ValueType:  blackboard.ExchangeValueTypeFindingRef,
			Text:       finding.Summary,
			FindingIDs: []string{finding.ID},
			ClaimIDs:   append([]string{}, finding.ClaimIDs...),
		})
	}
	return result
}

func formatMaterializedInputs(task team.Task, board *blackboard.State) string {
	if board == nil || len(task.Reads) == 0 {
		return ""
	}
	sections := make([]string, 0, len(task.Reads))
	plainTexts := make([]string, 0, len(task.Reads))
	plainOnly := true
	for _, key := range task.Reads {
		items := materializeReadKey(board, task, key)
		if len(items) == 0 {
			continue
		}
		lines := make([]string, 0, len(items))
		for _, item := range items {
			line, plain := renderExchange(item)
			if strings.TrimSpace(line) == "" {
				continue
			}
			lines = append(lines, line)
			if !plain {
				plainOnly = false
			}
		}
		if len(lines) == 0 {
			continue
		}
		if len(task.Reads) == 1 && plainOnly {
			plainTexts = append(plainTexts, lines...)
			continue
		}
		section := key + ":\n" + strings.Join(lines, "\n")
		sections = append(sections, section)
	}
	if len(sections) == 0 {
		return strings.Join(plainTexts, "\n")
	}
	return strings.Join(sections, "\n\n")
}

func filterMaterializedExchanges(task team.Task, items []blackboard.Exchange) []blackboard.Exchange {
	if len(items) == 0 {
		return nil
	}
	if task.Kind != team.TaskKindSynthesize || !task.VerifierRequired {
		return items
	}
	filtered := make([]blackboard.Exchange, 0, len(items))
	for _, item := range items {
		if strings.HasPrefix(item.Namespace, "verify.") {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func renderExchange(exchange blackboard.Exchange) (string, bool) {
	if strings.TrimSpace(exchange.Text) != "" && len(exchange.Structured) == 0 && len(exchange.ArtifactIDs) == 0 {
		return exchange.Text, true
	}
	if len(exchange.Structured) > 0 {
		payload, err := json.Marshal(exchange.Structured)
		if err == nil {
			return string(payload), false
		}
	}
	if len(exchange.ArtifactIDs) > 0 {
		return "artifacts: " + strings.Join(exchange.ArtifactIDs, ", "), false
	}
	if strings.TrimSpace(exchange.Text) != "" {
		return exchange.Text, false
	}
	return "", false
}

func (r *Runtime) buildTaskResult(task team.Task, generated []message.Message) *team.Result {
	summary := strings.TrimSpace(task.Input)
	confidence := 0.5
	structured := map[string]any{}
	artifactSet := map[string]struct{}{}
	for _, msg := range generated {
		if msg.ToolResult != nil {
			mergeStructured(structured, structuredPayload(msg.ToolResult.Structured))
			collectArtifactIDs(artifactSet, msg.ToolResult.Structured)
			if text := strings.TrimSpace(msg.ToolResult.Content); text != "" && summary == "" {
				summary = text
			}
			continue
		}
		if text := strings.TrimSpace(msg.Text); text != "" {
			summary = text
			confidence = 0.85
			mergeStructured(structured, parseStructuredText(text))
		}
	}
	if candidate, ok := structured["summary"].(string); ok && strings.TrimSpace(candidate) != "" {
		summary = strings.TrimSpace(candidate)
	}
	artifactIDs := sortKeys(artifactSet)
	if len(artifactIDs) == 0 {
		artifactIDs = artifactIDsFromStructured(structured)
	}
	if value, ok := structured["confidence"].(float64); ok && value > 0 {
		confidence = value
	}
	result := &team.Result{
		Summary:     summary,
		ArtifactIDs: artifactIDs,
		Findings: []team.Finding{
			{Summary: summary, Confidence: confidence},
		},
		Evidence: []team.Evidence{
			{Source: task.Title, Snippet: summary},
		},
		Confidence: confidence,
	}
	if len(structured) > 0 {
		result.Structured = structured
	}
	return result
}

func mergeStructured(target map[string]any, source map[string]any) {
	for key, value := range source {
		target[key] = value
	}
}

func structuredPayload(payload json.RawMessage) map[string]any {
	if len(payload) == 0 {
		return nil
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return nil
	}
	return decoded
}

func parseStructuredText(text string) map[string]any {
	text = strings.TrimSpace(text)
	if text == "" || !strings.HasPrefix(text, "{") {
		return nil
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		return nil
	}
	return decoded
}

func collectArtifactIDs(target map[string]struct{}, payload json.RawMessage) {
	for _, item := range artifactIDsFromStructured(structuredPayload(payload)) {
		target[item] = struct{}{}
	}
}

func artifactIDsFromStructured(payload map[string]any) []string {
	if len(payload) == 0 {
		return nil
	}
	items, ok := payload["artifactIds"]
	if !ok {
		return nil
	}
	switch current := items.(type) {
	case []string:
		return append([]string{}, current...)
	case []any:
		result := make([]string, 0, len(current))
		for _, item := range current {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				result = append(result, text)
			}
		}
		return result
	default:
		return nil
	}
}

func sortKeys(values map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}
	items := make([]string, 0, len(values))
	for key := range values {
		items = append(items, key)
	}
	sort.Strings(items)
	return items
}

func (r *Runtime) publishTaskOutputMessages(ctx context.Context, state team.RunState, task team.Task, agentInstance team.AgentInstance) {
	if task.Result == nil {
		return
	}
	text := strings.TrimSpace(task.Result.Summary)
	if text == "" && len(task.Result.Structured) > 0 {
		if payload, err := json.Marshal(task.Result.Structured); err == nil {
			text = string(payload)
		}
	}
	if text == "" {
		return
	}
	metadata := map[string]string{
		"taskId":     task.ID,
		"taskOutput": "true",
	}
	if len(task.Writes) > 0 {
		metadata["outputKeys"] = strings.Join(task.Writes, ",")
	}
	if len(task.Publish) == 0 {
		appendTaskOutputMessage(ctx, r, state.SessionID, state.ID, agentInstance.ID, message.VisibilityShared, text, metadata)
		return
	}
	if task.PublishesTo(team.OutputVisibilityPrivate) {
		appendTaskOutputMessage(ctx, r, task.SessionID, state.ID, agentInstance.ID, message.VisibilityPrivate, text, metadata)
	}
	if task.PublishesTo(team.OutputVisibilityShared) {
		appendTaskOutputMessage(ctx, r, state.SessionID, state.ID, agentInstance.ID, message.VisibilityShared, text, metadata)
	}
}

func appendTaskOutputMessage(ctx context.Context, runtime *Runtime, sessionID, teamID, agentID string, visibility message.Visibility, text string, metadata map[string]string) {
	if sessionID == "" {
		return
	}
	msg := message.NewText(message.RoleAssistant, text)
	msg.TeamID = teamID
	msg.AgentID = agentID
	msg.Visibility = visibility
	msg.Metadata = cloneStringMap(metadata)
	_, _ = runtime.appendSessionMessages(ctx, sessionID, msg)
}

func toolResultsFromMessages(messages []message.Message) []message.ToolResult {
	results := make([]message.ToolResult, 0, len(messages))
	for _, msg := range messages {
		if msg.ToolResult != nil {
			results = append(results, *msg.ToolResult)
		}
	}
	return results
}
