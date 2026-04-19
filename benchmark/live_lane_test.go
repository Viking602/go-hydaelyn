package benchmark

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Viking602/go-hydaelyn/evaluation"
	"github.com/Viking602/go-hydaelyn/provider"
)

type liveLaneTestDriver struct {
	events []provider.Event
	err    error
}

func (d liveLaneTestDriver) Metadata() provider.Metadata {
	return provider.Metadata{Name: "test-provider", Version: "test-v1", Models: []string{"test-model"}}
}

func (d liveLaneTestDriver) Stream(context.Context, provider.Request) (provider.Stream, error) {
	if d.err != nil {
		return nil, d.err
	}
	return provider.NewSliceStream(d.events), nil
}

func withLiveLaneProvider(t *testing.T, name string, factory func(LaneSpec) (provider.Driver, error)) {
	t.Helper()
	original, existed := liveLaneProviderFactories[name]
	liveLaneProviderFactories[name] = factory
	t.Cleanup(func() {
		if existed {
			liveLaneProviderFactories[name] = original
			return
		}
		delete(liveLaneProviderFactories, name)
	})
}

func writeLiveLaneCase(t *testing.T, dir string, body string) string {
	t.Helper()
	path := filepath.Join(dir, "case.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write case: %v", err)
	}
	return path
}

func TestLiveLaneContract(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	casePath := writeLiveLaneCase(t, workspace, `{
	  "schemaVersion": "1.0",
	  "id": "nightly-contract",
	  "suite": "live",
	  "pattern": "deepsearch",
	  "input": {"prompt": "Say alpha and beta"},
	  "expected": {"mustInclude": ["alpha", "beta"]}
	}`)
	withLiveLaneProvider(t, "test", func(LaneSpec) (provider.Driver, error) {
		return liveLaneTestDriver{events: []provider.Event{
			{Kind: provider.EventTextDelta, Text: "alpha beta", Usage: provider.Usage{InputTokens: 100, OutputTokens: 20, TotalTokens: 120}},
			{Kind: provider.EventDone, StopReason: provider.StopReasonComplete, Usage: provider.Usage{InputTokens: 100, OutputTokens: 20, TotalTokens: 120}},
		}}, nil
	})
	run, err := RunLiveLane(context.Background(), casePath, LaneSpec{ID: "nightly", Provider: "test", Model: "model-x", ProviderProvenance: "fixture-provider", ModelProvenance: "snapshot-a", PromptCostPer1KUSD: 0.01, CompletionCostPer1KUSD: 0.02})
	if err != nil {
		t.Fatalf("RunLiveLane error = %v", err)
	}
	if run.Mode != evaluation.EvalRunModeLive || run.Status != evaluation.EvalRunStatusCompleted {
		t.Fatalf("unexpected run: %#v", run)
	}
	if run.Provenance == nil || run.Provenance.Provider != "test-provider" || run.Provenance.Model != "model-x" {
		t.Fatalf("missing provenance: %#v", run.Provenance)
	}
	if run.Usage == nil || run.Usage.TotalTokens != 120 || run.Usage.LatencyMs < 0 {
		t.Fatalf("missing usage: %#v", run.Usage)
	}
	if run.ScoreRef == nil || run.TraceRefs == nil || run.TraceRefs.ModelEvents == nil || run.ArtifactRefs == nil || run.ArtifactRefs.Answer == nil {
		t.Fatalf("missing canonical refs: %#v", run)
	}
	scoreData, err := os.ReadFile(run.ScoreRef.Path)
	if err != nil {
		t.Fatalf("read score artifact: %v", err)
	}
	var score evaluation.ScorePayload
	if err := json.Unmarshal(scoreData, &score); err != nil {
		t.Fatalf("decode score artifact: %v", err)
	}
	if score.QualityMetrics == nil || score.QualityMetrics.AnswerCorrectness != 1 {
		t.Fatalf("unexpected score payload: %#v", score)
	}
	manifestPath := filepath.Join(filepath.Dir(run.ScoreRef.Path), "manifest.json")
	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatalf("missing manifest: %v", err)
	}
}

func TestLaneBudgetPolicy(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	casePath := writeLiveLaneCase(t, workspace, `{"schemaVersion":"1.0","id":"budget-case","suite":"live","pattern":"deepsearch","input":{"prompt":"trigger tool"}}`)
	withLiveLaneProvider(t, "testbudget", func(LaneSpec) (provider.Driver, error) {
		return liveLaneTestDriver{events: []provider.Event{
			{Kind: provider.EventToolCallDelta, ToolCallDelta: &provider.ToolCallDelta{ID: "tool-1", Name: "lookup"}, Usage: provider.Usage{InputTokens: 10, OutputTokens: 0, TotalTokens: 10}},
			{Kind: provider.EventTextDelta, Text: "partial", Usage: provider.Usage{InputTokens: 10, OutputTokens: 2, TotalTokens: 12}},
		}}, nil
	})
	run, err := RunLiveLane(context.Background(), casePath, LaneSpec{ID: "nightly", Provider: "testbudget", Model: "model-x", MaxToolCalls: 0, MaxTokens: 11})
	if err == nil {
		t.Fatal("expected budget error")
	}
	if run == nil || run.Status != evaluation.EvalRunStatusFailed {
		t.Fatalf("expected failed run, got %#v err=%v", run, err)
	}
	joined := marshalPolicyOutcomes(t, run.PolicyOutcomes)
	if !strings.Contains(joined, "live_lane.budget.tokens") && !strings.Contains(joined, "live_lane.budget.tool_calls") {
		t.Fatalf("expected budget policy outcome, got %s", joined)
	}
}

func TestLaneSecretsRedaction(t *testing.T) {
	workspace := t.TempDir()
	casePath := writeLiveLaneCase(t, workspace, `{"schemaVersion":"1.0","id":"secret-case","suite":"live","pattern":"deepsearch","input":{"prompt":"repeat secret"}}`)
	secret := "sk-test-secret-12345678"
	t.Setenv("LIVE_LANE_API_KEY", secret)
	withLiveLaneProvider(t, "testsecret", func(LaneSpec) (provider.Driver, error) {
		return liveLaneTestDriver{events: []provider.Event{
			{Kind: provider.EventTextDelta, Text: "echo " + secret, Usage: provider.Usage{InputTokens: 8, OutputTokens: 3, TotalTokens: 11}},
			{Kind: provider.EventDone, StopReason: provider.StopReasonComplete, Usage: provider.Usage{InputTokens: 8, OutputTokens: 3, TotalTokens: 11}},
		}}, nil
	})
	run, err := RunLiveLane(context.Background(), casePath, LaneSpec{ID: "nightly", Provider: "testsecret", Model: "model-x", APIKeyEnv: "LIVE_LANE_API_KEY", RequiredSecrets: []string{"LIVE_LANE_API_KEY"}})
	if err != nil {
		t.Fatalf("RunLiveLane error = %v", err)
	}
	for _, name := range []string{"answer.txt", "model_events.json", "policy_outcomes.json", "run.json"} {
		data, readErr := os.ReadFile(filepath.Join(filepath.Dir(run.ScoreRef.Path), name))
		if readErr != nil {
			t.Fatalf("read %s: %v", name, readErr)
		}
		if strings.Contains(string(data), secret) {
			t.Fatalf("secret leaked in %s: %s", name, string(data))
		}
		if (name == "answer.txt" || name == "model_events.json") && !strings.Contains(string(data), "[REDACTED]") {
			t.Fatalf("expected redaction marker in %s: %s", name, string(data))
		}
	}
	if _, err := RunLiveLane(context.Background(), casePath, LaneSpec{ID: "missing", Provider: "testsecret", Model: "model-x", RequiredSecrets: []string{"MISSING_SECRET"}}); err == nil || !strings.Contains(err.Error(), "MISSING_SECRET") {
		t.Fatalf("expected missing secret error, got %v", err)
	}
}

func TestLaneVarianceTolerance(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	casePath := writeLiveLaneCase(t, workspace, `{
	  "schemaVersion": "1.0",
	  "id": "variance-case",
	  "suite": "live",
	  "pattern": "deepsearch",
	  "input": {"prompt": "return alpha beta"},
	  "expected": {"mustInclude": ["alpha", "beta"]},
	  "thresholds": {"groundedness": 0.9},
	  "limits": {"maxLatencyMs": 1000}
	}`)
	responses := [][]provider.Event{
		{{Kind: provider.EventTextDelta, Text: "alpha beta", Usage: provider.Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15}}, {Kind: provider.EventDone, StopReason: provider.StopReasonComplete, Usage: provider.Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15}}},
		{{Kind: provider.EventTextDelta, Text: "alpha", Usage: provider.Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15}}, {Kind: provider.EventDone, StopReason: provider.StopReasonComplete, Usage: provider.Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15}}},
	}
	index := 0
	withLiveLaneProvider(t, "testvariance", func(LaneSpec) (provider.Driver, error) {
		if index >= len(responses) {
			return nil, errors.New("no more responses")
		}
		driver := liveLaneTestDriver{events: responses[index]}
		index++
		return driver, nil
	})
	first, err := RunLiveLane(context.Background(), casePath, LaneSpec{ID: "nightly", Provider: "testvariance", Model: "model-x", MetricTolerances: map[string]float64{"overallScore": 0.6, "groundedness": 0.6}, LatencyToleranceMs: 200})
	if err != nil {
		t.Fatalf("first live run error = %v", err)
	}
	second, err := RunLiveLane(context.Background(), casePath, LaneSpec{ID: "nightly", Provider: "testvariance", Model: "model-x", MetricTolerances: map[string]float64{"overallScore": 0.2, "groundedness": 0.2}, LatencyToleranceMs: 200})
	if err != nil {
		t.Fatalf("second live run error = %v", err)
	}
	if second.Variance == nil || second.Variance.ComparedRunID != first.ID {
		t.Fatalf("expected variance tracking against prior run: %#v", second.Variance)
	}
	if len(second.Variance.ExceededMetrics) == 0 {
		t.Fatalf("expected exceeded variance metrics: %#v", second.Variance)
	}
	joined := marshalPolicyOutcomes(t, second.PolicyOutcomes)
	if !strings.Contains(joined, "live_lane.variance") || !strings.Contains(joined, "live_lane.threshold.groundedness") {
		t.Fatalf("expected variance and threshold policy outcomes, got %s", joined)
	}
}

func marshalPolicyOutcomes(t *testing.T, items []evaluation.EvalRunPolicyOutcome) string {
	t.Helper()
	data, err := json.Marshal(items)
	if err != nil {
		t.Fatalf("marshal policy outcomes: %v", err)
	}
	return string(data)
}
