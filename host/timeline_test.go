package host

import (
	"context"
	"testing"
	"time"

	"github.com/Viking602/go-hydaelyn/storage"
)

func TestTeamTimelineProjectsPanelEventsForUsers(t *testing.T) {
	driver := storage.NewMemoryDriver()
	runner := New(Config{Storage: driver})
	now := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)
	events := []storage.Event{
		{RunID: "team-1", Sequence: 1, RecordedAt: now, TeamID: "team-1", Type: storage.EventTodoPlanned, Payload: map[string]any{"goal": "launch auth", "todoCount": 2}},
		{RunID: "team-1", Sequence: 2, RecordedAt: now.Add(time.Second), TeamID: "team-1", TaskID: "security-review", Type: storage.EventTodoClaimed, Payload: map[string]any{"todoId": "security-review", "title": "review auth threat model", "agentId": "worker-1"}},
		{RunID: "team-1", Sequence: 3, RecordedAt: now.Add(2 * time.Second), TeamID: "team-1", TaskID: "security-review-review", Type: storage.EventMailboxSent, Payload: map[string]any{"intent": "challenge", "fromAgentId": "worker-2", "toAgentId": "worker-1", "body": "claim-2 lacks evidence", "structured": map[string]any{"references": []map[string]string{{"kind": "claim", "id": "claim-2"}}}}},
		{RunID: "team-1", Sequence: 4, RecordedAt: now.Add(3 * time.Second), TeamID: "team-1", TaskID: "security-review-review", Type: storage.EventVerifierBlocked, Payload: map[string]any{"verificationStatus": "insufficient", "summary": "claim-2 lacks evidence"}},
		{RunID: "team-1", Sequence: 5, RecordedAt: now.Add(4 * time.Second), TeamID: "team-1", TaskID: "task-synthesize", Type: storage.EventSynthesisCommitted, Payload: map[string]any{"summary": "final"}},
	}
	for _, event := range events {
		if err := driver.Events().Append(context.Background(), event); err != nil {
			t.Fatalf("Append() error = %v", err)
		}
	}

	timeline, err := runner.TeamTimeline(context.Background(), "team-1")
	if err != nil {
		t.Fatalf("TeamTimeline() error = %v", err)
	}
	if len(timeline) != 5 {
		t.Fatalf("expected 5 projected items, got %#v", timeline)
	}
	if timeline[0].Kind != TimelineKindControl || timeline[1].Kind != TimelineKindWork || timeline[2].Kind != TimelineKindConversation || timeline[3].Kind != TimelineKindEvidence {
		t.Fatalf("unexpected timeline kinds: %#v", timeline)
	}
	if timeline[2].References[0].ID != "claim-2" || timeline[2].Text == "" {
		t.Fatalf("expected challenge reference and visible text, got %#v", timeline[2])
	}
}
