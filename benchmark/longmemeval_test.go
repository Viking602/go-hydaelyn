package benchmark

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildLongMemEvalPromptSortsSessionsAndRemovesAnswerMarkers(t *testing.T) {
	t.Parallel()
	prompt := buildLongMemEvalPrompt(longMemEvalEntry{
		QuestionDate: "2024/04/10",
		Question:     "What did I ask you to remember?",
		HaystackDates: []string{
			"2024/04/09",
			"2024/04/01",
		},
		HaystackSessions: [][]longMemEvalTurn{
			{
				{Role: "user", Content: "later fact", HasAnswer: true},
			},
			{
				{Role: "user", Content: "earlier fact"},
			},
		},
	})
	firstIndex := strings.Index(prompt, "2024/04/01")
	secondIndex := strings.Index(prompt, "2024/04/09")
	if firstIndex == -1 || secondIndex == -1 || firstIndex >= secondIndex {
		t.Fatalf("expected sessions to be sorted by date in prompt: %s", prompt)
	}
	if strings.Contains(prompt, "has_answer") {
		t.Fatalf("prompt should not expose has_answer labels: %s", prompt)
	}
}

func TestCollectLongMemEvalResultsParsesOfficialEvalLog(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	referencePath := filepath.Join(dir, "reference.json")
	logPath := filepath.Join(dir, "hypotheses.jsonl.eval-results-gpt-4o")
	reference := `[
  {"question_id":"q1","question_type":"multi-session","question":"q?","answer":"a","question_date":"2024/01/01","haystack_dates":[],"haystack_sessions":[]},
  {"question_id":"q2_abs","question_type":"knowledge-update","question":"q?","answer":"a","question_date":"2024/01/01","haystack_dates":[],"haystack_sessions":[]}
]`
	logs := `{"question_id":"q1","autoeval_label":{"model":"gpt-4o-2024-08-06","label":true}}
{"question_id":"q2_abs","autoeval_label":{"model":"gpt-4o-2024-08-06","label":false}}
`
	if err := os.WriteFile(referencePath, []byte(reference), 0o644); err != nil {
		t.Fatalf("write reference: %v", err)
	}
	if err := os.WriteFile(logPath, []byte(logs), 0o644); err != nil {
		t.Fatalf("write logs: %v", err)
	}
	bundle, err := collectLongMemEvalResults(dir, "gpt-4o")
	if err != nil {
		t.Fatalf("collect results: %v", err)
	}
	if bundle.Scores["qaAccuracy"] != 0.5 {
		t.Fatalf("unexpected qaAccuracy: %#v", bundle.Scores)
	}
	if bundle.Scores["abstentionAccuracy"] != 0 {
		t.Fatalf("unexpected abstentionAccuracy: %#v", bundle.Scores)
	}
	if bundle.Scores["questionType.multi-session"] != 1 {
		t.Fatalf("unexpected per-type score: %#v", bundle.Scores)
	}
}
