package host

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/Viking602/go-hydaelyn/internal/blackboard"
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
	selectors := selectorsForTask(task)
	if len(selectors) == 0 || state.Blackboard == nil {
		return nil, ""
	}
	items := materializeForTask(state.Blackboard, task, selectors)
	return items, formatMaterializedInputs(task, items)
}

// materializeForTask runs the selector pipeline and, for legacy callers that
// declared the "supported_findings" pseudo-key on non-verifier-guarded
// synthesize tasks, falls back to board.SupportedFindings() so existing tests
// and recipes keep working while new code paths move to RequireVerified
// selectors.
//
// Findings are intentionally *not* synthesized into exchanges here. In the new
// model a finding becomes a task input only once the verifier gate has
// published it as a "supported_findings" exchange — that real exchange is what
// selectors match. Surfacing SelectFindings() results as fake exchanges would
// let unpublished findings leak into guarded-synthesis prompts.
func materializeForTask(board *blackboard.State, task team.Task, selectors []blackboard.ExchangeSelector) []blackboard.Exchange {
	ctx := MaterializeSelectors(board, selectors)
	items := ctx.Exchanges
	if shouldApplyLegacySupportedFindings(task, selectors, items) {
		items = append(items, legacySupportedFindings(board)...)
	}
	return items
}

func shouldApplyLegacySupportedFindings(task team.Task, selectors []blackboard.ExchangeSelector, current []blackboard.Exchange) bool {
	if len(current) > 0 {
		return false
	}
	if len(task.ReadSelectors) > 0 {
		return false
	}
	if task.Kind == team.TaskKindSynthesize && task.VerifierRequired {
		return false
	}
	for _, sel := range selectors {
		for _, key := range sel.Keys {
			if key == supportedFindingsReadKey {
				return true
			}
		}
	}
	return false
}

func legacySupportedFindings(board *blackboard.State) []blackboard.Exchange {
	if board == nil {
		return nil
	}
	supported := board.SupportedFindings()
	out := make([]blackboard.Exchange, 0, len(supported))
	for _, finding := range supported {
		out = append(out, blackboard.Exchange{
			Key:        supportedFindingsReadKey,
			TaskID:     finding.TaskID,
			ValueType:  blackboard.ExchangeValueTypeFindingRef,
			Text:       finding.Summary,
			FindingIDs: []string{finding.ID},
			ClaimIDs:   append([]string{}, finding.ClaimIDs...),
		})
	}
	return out
}

func formatMaterializedInputs(task team.Task, items []blackboard.Exchange) string {
	if len(items) == 0 {
		return ""
	}
	byKey := map[string][]blackboard.Exchange{}
	order := make([]string, 0)
	for _, item := range items {
		if _, ok := byKey[item.Key]; !ok {
			order = append(order, item.Key)
		}
		byKey[item.Key] = append(byKey[item.Key], item)
	}
	sections := make([]string, 0, len(order))
	plainTexts := make([]string, 0)
	plainOnly := true
	for _, key := range order {
		keyItems := byKey[key]
		lines := make([]string, 0, len(keyItems))
		for _, item := range keyItems {
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
		if len(order) == 1 && plainOnly {
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

// buildTaskResult reconstructs a team.Result from the messages a worker
// produced. PR 6 removed the old "if the worker wrote any text, confidence
// jumps to 0.85" heuristic — prose alone is not evidence, and treating it
// as such let any chatty worker silently upgrade the confidence field. Now
// confidence only rises when the worker ships a structured report (or at
// minimum a structured "confidence" field). Bare text keeps the
// conservative 0.5 floor so downstream gates (SupportsClaim, verifier
// gate) see honest values.
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

func appendTaskOutputMessage(ctx context.Context, runner *Runtime, sessionID, teamID, agentID string, visibility message.Visibility, text string, metadata map[string]string) {
	if sessionID == "" {
		return
	}
	msg := message.NewText(message.RoleAssistant, text)
	msg.TeamID = teamID
	msg.AgentID = agentID
	msg.Visibility = visibility
	msg.Metadata = cloneStringMap(metadata)
	_, _ = runner.appendSessionMessages(ctx, sessionID, msg)
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
