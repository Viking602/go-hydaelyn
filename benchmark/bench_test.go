package benchmark

import (
	"context"
	"testing"
	"time"

	"github.com/Viking602/go-hydaelyn/evaluation"
	"github.com/Viking602/go-hydaelyn/host"
	"github.com/Viking602/go-hydaelyn/patterns/deepsearch"
	"github.com/Viking602/go-hydaelyn/storage"
	"github.com/Viking602/go-hydaelyn/team"
)

func BenchmarkDeepsearch(b *testing.B) {
	runner := newPerfHostRuntime(nil, 2, 0)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := runner.StartTeam(context.Background(), host.StartTeamRequest{
			Pattern:           "deepsearch",
			SupervisorProfile: "supervisor",
			WorkerProfiles:    []string{"researcher", "researcher"},
			Input: map[string]any{
				"query":      "benchmark",
				"subqueries": []string{"a", "b", "c", "d"},
			},
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEval(b *testing.B) {
	events := benchmarkEvalEvents()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = evaluation.Evaluate(events)
	}
}

func BenchmarkParallelEfficiency(b *testing.B) {
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, err := measureToolBatchLatency(ctx, 4, time.Millisecond)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMaxConcurrency(b *testing.B) {
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, err := measureMaxConcurrencyScenario(ctx, 2, 4, time.Millisecond)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkEvalEvents() []storage.Event {
	started := time.Unix(1700000000, 0).UTC()
	return []storage.Event{
		{RunID: "bench", Sequence: 1, RecordedAt: started, Type: storage.EventTeamStarted, TeamID: "bench"},
		{RunID: "bench", Sequence: 2, RecordedAt: started.Add(time.Millisecond), Type: storage.EventTaskScheduled, TeamID: "bench", TaskID: "task-1", Payload: map[string]any{"budget": map[string]any{"tokens": 16}}},
		{RunID: "bench", Sequence: 3, RecordedAt: started.Add(2 * time.Millisecond), Type: storage.EventTaskCompleted, TeamID: "bench", TaskID: "task-1", Payload: map[string]any{"usage": map[string]any{"totalTokens": 12}}},
		{RunID: "bench", Sequence: 4, RecordedAt: started.Add(3 * time.Millisecond), Type: storage.EventTaskScheduled, TeamID: "bench", TaskID: "task-2", Payload: map[string]any{"budget": map[string]any{"tokens": 8}}},
		{RunID: "bench", Sequence: 5, RecordedAt: started.Add(4 * time.Millisecond), Type: storage.EventTaskCompleted, TeamID: "bench", TaskID: "task-2", Payload: map[string]any{"usage": map[string]any{"totalTokens": 8}}},
	}
}

func init() {
	_ = deepsearch.New()
	_ = team.Profile{}
}
