package evaluation

import (
	"reflect"
	"testing"
	"time"

	"github.com/Viking602/go-hydaelyn/storage"
)

func TestScoreCase(t *testing.T) {
	t.Parallel()

	evalRun := &EvalRun{
		ID:          "run-8",
		CaseID:      "case-8",
		StartedAt:   time.Date(2026, time.April, 19, 12, 0, 0, 0, time.UTC),
		CompletedAt: time.Date(2026, time.April, 19, 12, 0, 2, 0, time.UTC),
		PolicyOutcomes: []EvalRunPolicyOutcome{{
			Policy:   "capability.permission",
			Outcome:  "denied",
			Severity: "warning",
			Blocking: true,
		}},
	}
	caseDef := EvalCase{
		ID:       "case-8",
		Expected: &EvalCaseExpected{RequiredCitations: []string{"doc-1", "doc-2"}},
		Limits:   &EvalCaseLimits{MaxLatencyMs: 5000, MaxToolCalls: 4},
	}
	events := scorePipelineEvents(true, map[string]any{
		"quality": map[string]any{
			"answerCorrectness":      0.92,
			"groundedness":           0.86,
			"citationPrecision":      0.90,
			"toolPrecision":          0.83,
			"toolRecall":             0.80,
			"toolArgAccuracy":        0.79,
			"synthesisInputCoverage": 0.85,
		},
		"citations": []any{"doc-1", "doc-2"},
	})

	score, err := ScoreCase(evalRun, events, caseDef)
	if err != nil {
		t.Fatalf("ScoreCase() error = %v", err)
	}
	if score.Level != ScoreLevelA4 {
		t.Fatalf("expected A4 score level, got %#v", score)
	}
	if score.RuntimeMetrics == nil || score.RuntimeMetrics.EndToEndLatencyMs != 2000 {
		t.Fatalf("expected eval run latency override, got %#v", score)
	}
	if score.QualityMetrics == nil {
		t.Fatalf("expected quality metrics, got %#v", score)
	}
	if score.QualityMetrics.CitationRecall != 1 {
		t.Fatalf("expected citation recall from required citations, got %#v", score.QualityMetrics)
	}
	wantSafety := &ScoreSafetyMetrics{UnauthorizedToolBlocked: true}
	if !reflect.DeepEqual(score.SafetyMetrics, wantSafety) {
		t.Fatalf("unexpected safety metrics\nwant: %#v\ngot:  %#v", wantSafety, score.SafetyMetrics)
	}
	if len(score.Failures) == 0 || len(score.Recommendations) == 0 {
		t.Fatalf("expected extracted failures and recommendations, got %#v", score)
	}
	if score.Recommendations[0].Priority != "P2" {
		t.Fatalf("expected P2 tool recommendation, got %#v", score.Recommendations)
	}
}

func TestSafetyDowngrade(t *testing.T) {
	t.Parallel()

	evalRun := &EvalRun{
		ID:          "run-safety",
		StartedAt:   time.Date(2026, time.April, 19, 12, 0, 0, 0, time.UTC),
		CompletedAt: time.Date(2026, time.April, 19, 12, 0, 1, 0, time.UTC),
		PolicyOutcomes: []EvalRunPolicyOutcome{{
			Policy:   "capability.timeout",
			Outcome:  "timed_out",
			Severity: "critical",
			Blocking: true,
		}},
	}

	score, err := ScoreCase(evalRun, scorePipelineEvents(true, map[string]any{"quality": map[string]any{"groundedness": 0.95, "synthesisInputCoverage": 0.95}}), EvalCase{})
	if err != nil {
		t.Fatalf("ScoreCase() error = %v", err)
	}
	if score.Level != ScoreLevelA1 {
		t.Fatalf("expected safety downgrade to cap at A1, got %#v", score)
	}
}

func TestReplayMismatchDowngrade(t *testing.T) {
	t.Parallel()

	evalRun := &EvalRun{ID: "run-replay", StartedAt: time.Date(2026, time.April, 19, 12, 0, 0, 0, time.UTC), CompletedAt: time.Date(2026, time.April, 19, 12, 0, 1, 0, time.UTC)}
	score, err := ScoreCase(evalRun, scorePipelineEvents(false, map[string]any{"quality": map[string]any{"groundedness": 0.95, "synthesisInputCoverage": 0.95}}), EvalCase{})
	if err != nil {
		t.Fatalf("ScoreCase() error = %v", err)
	}
	if score.Level != ScoreLevelA2 {
		t.Fatalf("expected replay inconsistency to cap at A2, got %#v", score)
	}
}

func TestCapabilityReport(t *testing.T) {
	t.Parallel()

	report := GenerateCapabilityReport(&ScorePayload{
		RunID:            "run-report",
		OverallScore:     0.88,
		Level:            ScoreLevelA3,
		ReplayConsistent: true,
		RuntimeMetrics:   &ScoreRuntimeMetrics{TaskCompletionRate: 1, BlockingFailureRate: 0, RetrySuccessRate: 1, TokenBudgetHitRate: 0},
		QualityMetrics:   &ScoreQualityMetrics{AnswerCorrectness: 0.9, Groundedness: 0.88, CitationPrecision: 0.87, CitationRecall: 0.86, ToolPrecision: 0.8, ToolRecall: 0.79, ToolArgAccuracy: 0.81, SynthesisInputCoverage: 0.85},
		SafetyMetrics:    &ScoreSafetyMetrics{PromptInjectionBlocked: true, UnauthorizedToolBlocked: true, SecretLeakBlocked: true},
		Failures:         []ScoreFailure{{Code: "low-tool-recall", Metric: "toolRecall", Layer: "tool", Severity: "medium"}},
		Recommendations:  []ScoreRecommendation{{Priority: "P2", Action: "tighten tool selection and argument validation", Rationale: "toolRecall 0.79 fell below 0.75"}},
	})
	if report == nil {
		t.Fatal("expected capability report")
	}
	if report.ReleaseDecision != ReleaseDecisionGo {
		t.Fatalf("expected Go release decision, got %#v", report)
	}
	if len(report.Radar) != 17 {
		t.Fatalf("expected radar metrics for all dimensions, got %#v", report.Radar)
	}
}

func TestReleaseClassification(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		score *ScorePayload
		want  ReleaseDecision
	}{
		{name: "go", score: &ScorePayload{Level: ScoreLevelA4, ReplayConsistent: true}, want: ReleaseDecisionGo},
		{name: "conditional", score: &ScorePayload{Level: ScoreLevelA2, ReplayConsistent: false}, want: ReleaseDecisionConditional},
		{name: "no-go", score: &ScorePayload{Level: ScoreLevelA1, ReplayConsistent: true, SafetyMetrics: &ScoreSafetyMetrics{CriticalFailure: true}}, want: ReleaseDecisionNoGo},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := classifyReleaseDecision(tc.score); got != tc.want {
				t.Fatalf("classifyReleaseDecision() = %s, want %s", got, tc.want)
			}
		})
	}
}

func scorePipelineEvents(replayConsistent bool, completedPayload map[string]any) []Event {
	startedAt := time.Date(2026, time.April, 19, 12, 0, 0, 0, time.UTC)
	completed := map[string]any{
		"usage":            map[string]any{"totalTokens": 80},
		"attempt":          1,
		"replayConsistent": replayConsistent,
	}
	for key, value := range completedPayload {
		completed[key] = value
	}
	return []Event{
		{RunID: "run", Sequence: 1, RecordedAt: startedAt, Type: storage.EventTeamStarted},
		{RunID: "run", Sequence: 2, RecordedAt: startedAt.Add(time.Millisecond), Type: storage.EventTaskScheduled, TaskID: "task-1", Payload: map[string]any{"budget": map[string]any{"tokens": 100}}},
		{RunID: "run", Sequence: 3, RecordedAt: startedAt.Add(2 * time.Millisecond), Type: storage.EventTaskStarted, TaskID: "task-1"},
		{RunID: "run", Sequence: 4, RecordedAt: startedAt.Add(3 * time.Millisecond), Type: storage.EventToolCalled, TaskID: "task-1"},
		{RunID: "run", Sequence: 5, RecordedAt: startedAt.Add(4 * time.Millisecond), Type: storage.EventTaskCompleted, TaskID: "task-1", Payload: completed},
		{RunID: "run", Sequence: 6, RecordedAt: startedAt.Add(5 * time.Millisecond), Type: storage.EventTaskOutputsPublished, TaskID: "task-1"},
		{RunID: "run", Sequence: 7, RecordedAt: startedAt.Add(6 * time.Millisecond), Type: storage.EventTeamCompleted},
	}
}
