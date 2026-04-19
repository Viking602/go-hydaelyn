package evaluation

import (
	"testing"
	"time"

	"github.com/Viking602/go-hydaelyn/storage"
)

func TestEvaluateComputesRuntimeMetricsFromEvents(t *testing.T) {
	start := time.Date(2026, 4, 18, 8, 0, 0, 0, time.UTC)
	events := []storage.Event{
		{RunID: "team-1", TeamID: "team-1", Type: storage.EventTeamStarted, RecordedAt: start},
		{RunID: "team-1", TeamID: "team-1", TaskID: "task-1", Type: storage.EventTaskScheduled, RecordedAt: start.Add(1 * time.Second), Payload: map[string]any{
			"title":         "research",
			"kind":          "research",
			"status":        "pending",
			"requiredRole":  "researcher",
			"assigneeAgent": "worker-1",
			"failurePolicy": "retry",
			"writes":        []string{"branch.alpha"},
			"publish":       []string{"shared", "blackboard"},
			"budget":        map[string]any{"tokens": 10},
		}},
		{RunID: "team-1", TeamID: "team-1", TaskID: "task-1", Type: storage.EventTaskCompleted, RecordedAt: start.Add(2 * time.Second), Payload: map[string]any{
			"status":        "completed",
			"summary":       "alpha",
			"attempts":      2,
			"toolCallCount": 1,
			"usage":         map[string]any{"totalTokens": 10},
		}},
		{RunID: "team-1", TeamID: "team-1", TaskID: "task-1", Type: storage.EventTaskOutputsPublished, RecordedAt: start.Add(3 * time.Second), Payload: map[string]any{
			"exchanges": []map[string]any{
				{"id": "exchange-1", "key": "branch.alpha", "taskId": "task-1", "valueType": "text", "text": "alpha"},
			},
			"verifications": []map[string]any{
				{"claimId": "claim-1", "status": "supported", "confidence": 0.9, "evidenceIds": []string{"exchange-1"}},
			},
		}},
		{RunID: "team-1", TeamID: "team-1", TaskID: "task-2", Type: storage.EventTaskScheduled, RecordedAt: start.Add(4 * time.Second), Payload: map[string]any{
			"title":         "synth",
			"kind":          "synthesize",
			"status":        "pending",
			"requiredRole":  "supervisor",
			"assigneeAgent": "supervisor",
			"failurePolicy": "fail_fast",
			"dependsOn":     []string{"task-1"},
			"reads":         []string{"branch.alpha"},
		}},
		{RunID: "team-1", TeamID: "team-1", TaskID: "task-2", Type: storage.EventTaskCompleted, RecordedAt: start.Add(5 * time.Second), Payload: map[string]any{
			"status":   "completed",
			"summary":  "alpha",
			"attempts": 1,
			"usage":    map[string]any{"totalTokens": 6},
		}},
		{RunID: "team-1", TeamID: "team-1", Type: storage.EventTeamCompleted, RecordedAt: start.Add(6 * time.Second), Payload: map[string]any{
			"summary": "alpha",
		}},
	}

	report := Evaluate(events)
	if report.TeamID != "team-1" {
		t.Fatalf("expected team id, got %#v", report)
	}
	if report.TaskCompletionRate != 1 {
		t.Fatalf("expected full completion rate, got %#v", report)
	}
	if report.RetrySuccessRate != 1 {
		t.Fatalf("expected retry success rate of 1, got %#v", report)
	}
	if report.SupportedClaimRatio != 1 {
		t.Fatalf("expected supported claim ratio of 1, got %#v", report)
	}
	if report.SynthesisInputCoverage != 1 {
		t.Fatalf("expected full synthesis input coverage, got %#v", report)
	}
	if report.ToolCallCount != 1 {
		t.Fatalf("expected tool call count from task payload, got %#v", report)
	}
	if report.TokenBudgetHitRate != 1 {
		t.Fatalf("expected token budget hit rate of 1, got %#v", report)
	}
	if report.EndToEndLatency != 6*time.Second {
		t.Fatalf("expected end-to-end latency from event times, got %#v", report)
	}
}
