package host

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Viking602/go-hydaelyn/storage"
	"github.com/Viking602/go-hydaelyn/team"
)

type TimelineKind string

const (
	TimelineKindWork         TimelineKind = "work"
	TimelineKindConversation TimelineKind = "conversation"
	TimelineKindEvidence     TimelineKind = "evidence"
	TimelineKindControl      TimelineKind = "control"
)

type TimelineItem struct {
	Sequence   int              `json:"sequence"`
	RecordedAt time.Time        `json:"recordedAt,omitempty"`
	Kind       TimelineKind     `json:"kind"`
	TeamID     string           `json:"teamId,omitempty"`
	TaskID     string           `json:"taskId,omitempty"`
	AgentID    string           `json:"agentId,omitempty"`
	Title      string           `json:"title,omitempty"`
	Text       string           `json:"text"`
	References []team.Reference `json:"references,omitempty"`
}

func (r *Runtime) TeamTimeline(ctx context.Context, teamID string) ([]TimelineItem, error) {
	events, err := r.listEvents(ctx, teamID)
	if err != nil {
		return nil, err
	}
	return ProjectTimeline(events), nil
}

func ProjectTimeline(events []storage.Event) []TimelineItem {
	items := make([]TimelineItem, 0, len(events))
	for _, event := range events {
		if item, ok := projectTimelineEvent(event); ok {
			items = append(items, item)
		}
	}
	return items
}

func projectTimelineEvent(event storage.Event) (TimelineItem, bool) {
	payload := event.Payload
	item := TimelineItem{
		Sequence:   event.Sequence,
		RecordedAt: event.RecordedAt,
		TeamID:     event.TeamID,
		TaskID:     event.TaskID,
	}
	switch event.Type {
	case storage.EventTodoPlanned:
		count := intFromAny(payload["todoCount"])
		item.Kind = TimelineKindControl
		item.Title = "Panel planned task board"
		item.Text = fmt.Sprintf("Panel planned %d todo(s) for %s", count, stringFromAny(payload["goal"]))
	case storage.EventTodoClaimed:
		item.Kind = TimelineKindWork
		item.AgentID = stringFromAny(payload["agentId"])
		item.Title = "Expert claimed todo"
		item.Text = fmt.Sprintf("%s claimed %s", item.AgentID, firstText(stringFromAny(payload["title"]), stringFromAny(payload["todoId"])))
		item.References = []team.Reference{{Kind: team.ReferenceKindTodo, ID: stringFromAny(payload["todoId"])}}
	case storage.EventTaskStarted:
		item.Kind = TimelineKindWork
		item.AgentID = stringFromAny(payload["workerId"])
		item.Title = "Task started"
		item.Text = fmt.Sprintf("%s started %s", firstText(item.AgentID, "agent"), event.TaskID)
	case storage.EventTaskCompleted:
		item.Kind = TimelineKindWork
		item.AgentID = stringFromAny(payload["workerId"])
		item.Title = "Task completed"
		item.Text = fmt.Sprintf("%s completed %s", firstText(item.AgentID, "agent"), event.TaskID)
	case storage.EventTaskOutputsPublished:
		item.Kind = TimelineKindEvidence
		item.AgentID = stringFromAny(payload["workerId"])
		item.Title = "Evidence published"
		item.Text = outputPublishedText(payload)
		item.References = outputReferences(payload)
	case storage.EventMailboxSent, storage.EventConversationMessage:
		item.Kind = TimelineKindConversation
		item.AgentID = stringFromAny(payload["fromAgentId"])
		item.Title = firstText(stringFromAny(payload["subject"]), stringFromAny(payload["intent"]), "Conversation message")
		item.Text = firstText(stringFromAny(payload["body"]), fmt.Sprintf("%s message", stringFromAny(payload["intent"])))
		item.References = referencesFromAny(payload["references"])
		if len(item.References) == 0 {
			item.References = referencesFromAny(nestedValue(payload["structured"], "references"))
		}
	case storage.EventVerifierPassed:
		item.Kind = TimelineKindEvidence
		item.Title = "Verifier passed"
		item.Text = firstText(stringFromAny(payload["summary"]), "Verifier accepted the referenced claims")
	case storage.EventVerifierBlocked:
		item.Kind = TimelineKindEvidence
		item.Title = "Verifier blocked"
		item.Text = firstText(stringFromAny(payload["summary"]), "Verifier blocked one or more claims")
	case storage.EventSynthesisCommitted:
		item.Kind = TimelineKindControl
		item.Title = "Synthesis committed"
		item.Text = firstText(stringFromAny(payload["summary"]), "Synthesizer committed the final answer")
	default:
		return TimelineItem{}, false
	}
	if item.TeamID == "" {
		item.TeamID = stringFromAny(payload["teamId"])
	}
	return item, true
}

func outputPublishedText(payload map[string]any) string {
	claims := lenSlice(payload["claims"])
	findings := lenSlice(payload["findings"])
	evidence := lenSlice(payload["evidence"])
	if claims == 0 && findings == 0 && evidence == 0 {
		return firstText(stringFromAny(payload["summary"]), "Task output published")
	}
	return fmt.Sprintf("Published %d claim(s), %d finding(s), and %d evidence item(s)", claims, findings, evidence)
}

func outputReferences(payload map[string]any) []team.Reference {
	refs := make([]team.Reference, 0)
	refs = appendRefs(refs, team.ReferenceKindClaim, payload["claims"])
	refs = appendRefs(refs, team.ReferenceKindFinding, payload["findings"])
	refs = appendRefs(refs, team.ReferenceKindEvidence, payload["evidence"])
	return refs
}

func appendRefs(refs []team.Reference, kind team.ReferenceKind, value any) []team.Reference {
	for _, item := range mapSlice(value) {
		id := stringFromAny(item["id"])
		if id == "" {
			continue
		}
		refs = append(refs, team.Reference{Kind: kind, ID: id})
	}
	return refs
}

func referencesFromAny(value any) []team.Reference {
	items := make([]team.Reference, 0)
	for _, item := range mapSlice(value) {
		kind := team.ReferenceKind(stringFromAny(item["kind"]))
		id := stringFromAny(item["id"])
		if kind == "" || id == "" {
			continue
		}
		items = append(items, team.Reference{Kind: kind, ID: id})
	}
	return items
}

func nestedValue(value any, key string) any {
	m, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return m[key]
}

func mapSlice(value any) []map[string]any {
	switch current := value.(type) {
	case []map[string]any:
		return current
	case []map[string]string:
		items := make([]map[string]any, 0, len(current))
		for _, item := range current {
			m := make(map[string]any, len(item))
			for key, value := range item {
				m[key] = value
			}
			items = append(items, m)
		}
		return items
	case []any:
		items := make([]map[string]any, 0, len(current))
		for _, item := range current {
			if m, ok := item.(map[string]any); ok {
				items = append(items, m)
			}
		}
		return items
	default:
		return nil
	}
}

func lenSlice(value any) int {
	switch current := value.(type) {
	case []map[string]any:
		return len(current)
	case []any:
		return len(current)
	default:
		return 0
	}
}

func stringFromAny(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func intFromAny(value any) int {
	switch current := value.(type) {
	case int:
		return current
	case int64:
		return int(current)
	case float64:
		return int(current)
	default:
		return 0
	}
}

func firstText(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
