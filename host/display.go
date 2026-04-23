package host

import (
	"encoding/json"
	"strings"

	"github.com/Viking602/go-hydaelyn/agent"
	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/team"
)

type DisplayEventKind string

const (
	DisplayEventKindDelta      DisplayEventKind = "delta"
	DisplayEventKindFinal      DisplayEventKind = "final"
	DisplayEventKindTaskOutput DisplayEventKind = "task_output"
)

type DisplayEvent struct {
	Kind       DisplayEventKind   `json:"kind"`
	Text       string             `json:"text"`
	TeamID     string             `json:"teamId,omitempty"`
	TaskID     string             `json:"taskId,omitempty"`
	AgentID    string             `json:"agentId,omitempty"`
	Visibility message.Visibility `json:"visibility,omitempty"`
	Source     string             `json:"source,omitempty"`
}

type DisplayResult struct {
	UserFacingAnswer string         `json:"userFacingAnswer,omitempty"`
	Display          []DisplayEvent `json:"display,omitempty"`
}

type AgentRunSummary struct {
	Usage         provider.Usage      `json:"usage"`
	StopReason    provider.StopReason `json:"stopReason"`
	Iterations    int                 `json:"iterations"`
	Display       string              `json:"display,omitempty"`
	DisplayKind   DisplayEventKind    `json:"displayKind,omitempty"`
	DisplaySource string              `json:"displaySource,omitempty"`
}

func promptDisplayResult(generated []message.Message) DisplayResult {
	text := displayTextFromMessages(generated)
	if strings.TrimSpace(text) == "" {
		return DisplayResult{}
	}
	return DisplayResult{
		UserFacingAnswer: text,
		Display: []DisplayEvent{{
			Kind:   DisplayEventKindFinal,
			Text:   text,
			Source: "prompt_result",
		}},
	}
}

func displayTextForTask(task team.Task, result *team.Result) (string, bool) {
	if result == nil || strings.TrimSpace(result.Error) != "" {
		return "", false
	}
	if report, ok := team.ExtractSynthesisReport(result.Structured); ok {
		answer := strings.TrimSpace(report.Answer)
		return answer, answer != ""
	}
	switch task.Kind {
	case team.TaskKindResearch, team.TaskKindVerify:
		return "", false
	}
	text := strings.TrimSpace(result.Summary)
	if text == "" || looksLikeJSON(text) {
		return "", false
	}
	return text, true
}

func displayTextFromMessages(messages []message.Message) string {
	for idx := len(messages) - 1; idx >= 0; idx-- {
		if text := displayTextFromToolResult(messages[idx]); text != "" {
			return text
		}
		if text := displayTextFromAssistantMessage(messages[idx]); text != "" {
			return text
		}
	}
	return ""
}

func displayTextFromToolResult(msg message.Message) string {
	if msg.ToolResult == nil {
		return ""
	}
	report, ok := team.ExtractSynthesisReport(structuredPayload(msg.ToolResult.Structured))
	if !ok {
		return ""
	}
	return strings.TrimSpace(report.Answer)
}

func displayTextFromAssistantMessage(msg message.Message) string {
	if msg.Role != message.RoleAssistant {
		return ""
	}
	if report, ok := team.ExtractSynthesisReport(parseStructuredText(msg.Text)); ok {
		return strings.TrimSpace(report.Answer)
	}
	text := strings.TrimSpace(msg.Text)
	if text == "" || looksLikeJSON(text) {
		return ""
	}
	return text
}

func agentRunSummaryForPrompt(result agent.Result) AgentRunSummary {
	display := promptDisplayResult(result.Messages)
	return AgentRunSummary{
		Usage:         result.Usage,
		StopReason:    result.StopReason,
		Iterations:    result.Iterations,
		Display:       display.UserFacingAnswer,
		DisplayKind:   DisplayEventKindFinal,
		DisplaySource: "prompt_result",
	}
}

func agentRunSummaryForTask(task team.Task, result agent.Result) AgentRunSummary {
	display := ""
	if task.Kind == team.TaskKindSynthesize {
		display = displayTextFromMessages(result.Messages)
	}
	return AgentRunSummary{
		Usage:         result.Usage,
		StopReason:    result.StopReason,
		Iterations:    result.Iterations,
		Display:       display,
		DisplayKind:   DisplayEventKindFinal,
		DisplaySource: "task_result",
	}
}

func looksLikeJSON(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	if !(strings.HasPrefix(text, "{") || strings.HasPrefix(text, "[")) {
		return false
	}
	var payload any
	return json.Unmarshal([]byte(text), &payload) == nil
}
