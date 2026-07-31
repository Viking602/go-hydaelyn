package eval

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/provider"
)

// substringAssertion is a minimal in-package assertion used to exercise the
// failure verdict without importing eval/assertions (which imports eval and
// would create an import cycle in this white-box test).
type substringAssertion struct {
	want string
}

func (a substringAssertion) Name() string { return "substring" }

func (a substringAssertion) Check(ctx context.Context, run api.Run, h Harness) error {
	items, err := h.Runner().SelectItems(ctx, run.ID, api.BlackboardSelector{RunID: run.ID})
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.Payload == a.want || item.Content == a.want {
			return nil
		}
	}
	return errors.New("substring not found")
}

func script(text string) []provider.Event {
	return []provider.Event{
		{Kind: provider.EventTextDelta, Text: text},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	}
}

func TestEvalRun_FailingAssertionFailsCase(t *testing.T) {
	c := EvalCase{
		Name:  "failing",
		Input: api.StartRunCommand{RunID: "run-fail", RootTaskID: "root", Request: "do work"},
		Assertions: []Assertion{
			substringAssertion{want: "not-present"},
		},
	}
	h := NewHarness(WithScript(script("actual output")))
	defer h.Cleanup()
	res := runCase(context.Background(), c, h)
	if res.Passed {
		t.Fatal("expected case to fail")
	}
	if len(res.Failures) != 1 || res.Failures[0].Assertion != "substring" {
		t.Fatalf("failures = %+v", res.Failures)
	}
}

func TestEvalRun_TimeoutMarksFailed(t *testing.T) {
	c := EvalCase{
		Name:    "timeout",
		Timeout: time.Nanosecond,
		Input:   api.StartRunCommand{RunID: "run-timeout", RootTaskID: "root", Request: "slow"},
		Assertions: []Assertion{
			substringAssertion{want: "ok"},
		},
	}
	h := NewHarness(WithScript(script("ok")))
	defer h.Cleanup()
	res := runCase(context.Background(), c, h)
	if res.Passed {
		t.Fatal("expected timeout to fail the case")
	}
	if len(res.Failures) == 0 || res.Failures[0].Assertion != runFailure {
		t.Fatalf("expected %q failure, got %+v", runFailure, res.Failures)
	}
}

func TestRunCase_NilHarnessFails(t *testing.T) {
	c := EvalCase{Name: "nil"}
	res := runCase(context.Background(), c, nil)
	if res.Passed {
		t.Fatal("expected nil harness to fail")
	}
}

func TestSummarizeRunUsage_RollsUpLedger(t *testing.T) {
	const runID = "run-usage"
	ctx := context.Background()
	h := NewHarness()
	defer h.Cleanup()

	uow, err := h.Runner().Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	for _, rec := range []api.UsageRecord{
		{RunID: runID, InputTokens: 10, OutputTokens: 5, ToolCalls: 1, Credits: 2, DurationMS: 100},
		{RunID: runID, InputTokens: 3, OutputTokens: 7, ToolCalls: 2, Credits: 4, DurationMS: 50},
	} {
		if err := uow.UsageRecords().AppendUsage(ctx, rec); err != nil {
			t.Fatalf("AppendUsage: %v", err)
		}
	}
	if err := uow.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	got := summarizeRunUsage(ctx, h, runID)
	want := UsageSummary{Records: 2, InputTokens: 13, OutputTokens: 12, ToolCalls: 3, Credits: 6, DurationMS: 150}
	if got != want {
		t.Fatalf("summarizeRunUsage = %+v, want %+v", got, want)
	}

	// A run with no ledger records folds to the zero summary, never an error.
	if empty := summarizeRunUsage(ctx, h, "run-without-usage"); empty != (UsageSummary{}) {
		t.Fatalf("expected zero UsageSummary for an empty ledger, got %+v", empty)
	}
}
