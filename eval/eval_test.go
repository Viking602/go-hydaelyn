package eval_test

import (
	"encoding/json"
	"testing"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/eval"
	"github.com/Viking602/venat/eval/assertions"
	"github.com/Viking602/venat/provider"
)

func textScript(text string) []provider.Event {
	return []provider.Event{
		{Kind: provider.EventTextDelta, Text: text},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	}
}

func TestEvalRun_PassingCaseReturnsPassed(t *testing.T) {
	c := eval.EvalCase{
		Name:        "passing",
		Description: "summarize the input",
		Setup: func() eval.Harness {
			return eval.NewHarness(eval.WithScript(textScript("hello world summary")))
		},
		Input: api.StartRunCommand{RunID: "run-pass", RootTaskID: "root", Request: "summarize"},
		Assertions: []eval.Assertion{
			assertions.RunTerminatedWithStatus{Status: api.RunStatusCompleted},
			assertions.OutputContains{Substring: "summary"},
			assertions.EventEmitted{Type: api.EventTaskCompleted},
		},
	}
	res := eval.Run(t, c)
	if !res.Passed {
		t.Fatalf("expected pass, got failures %+v", res.Failures)
	}
	if res.Run.Status != api.RunStatusCompleted {
		t.Fatalf("run status = %q, want completed", res.Run.Status)
	}
	if res.Duration <= 0 {
		t.Fatalf("expected positive duration, got %s", res.Duration)
	}
}

func TestEvalRun_OutputMatchesSchema(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","required":["status"]}`)
	c := eval.EvalCase{
		Name: "schema",
		Setup: func() eval.Harness {
			return eval.NewHarness(eval.WithScript(textScript(`{"status":"done","detail":"ok"}`)))
		},
		Input: api.StartRunCommand{RunID: "run-schema", RootTaskID: "root", Request: "emit json"},
		Assertions: []eval.Assertion{
			assertions.OutputMatchesSchema{Schema: schema},
		},
	}
	res := eval.Run(t, c)
	if !res.Passed {
		t.Fatalf("expected schema match, got failures %+v", res.Failures)
	}
}

func TestEvalRunSuite_RunsEveryCase(t *testing.T) {
	cases := []eval.EvalCase{
		{
			Name:  "one",
			Setup: func() eval.Harness { return eval.NewHarness(eval.WithScript(textScript("alpha"))) },
			Input: api.StartRunCommand{RunID: "run-one", RootTaskID: "root", Request: "a"},
			Assertions: []eval.Assertion{
				assertions.OutputContains{Substring: "alpha"},
			},
		},
		{
			Name:  "two",
			Setup: func() eval.Harness { return eval.NewHarness(eval.WithScript(textScript("beta"))) },
			Input: api.StartRunCommand{RunID: "run-two", RootTaskID: "root", Request: "b"},
			Assertions: []eval.Assertion{
				assertions.OutputContains{Substring: "beta"},
			},
		},
	}
	results := eval.RunSuite(t, cases)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for _, r := range results {
		if !r.Passed {
			t.Fatalf("case %q failed: %+v", r.Case, r.Failures)
		}
	}
}

func TestEvalRunMatrix_AllParamCombinationsExecuted(t *testing.T) {
	cases := []eval.EvalCase{
		{
			Name:  "alpha",
			Setup: func() eval.Harness { return eval.NewHarness(eval.WithScript(textScript("fast output"))) },
			Input: api.StartRunCommand{RunID: "run-alpha", RootTaskID: "root", Request: "a"},
			Assertions: []eval.Assertion{
				assertions.RunTerminatedWithStatus{Status: api.RunStatusCompleted},
			},
		},
		{
			Name:  "beta",
			Setup: func() eval.Harness { return eval.NewHarness(eval.WithScript(textScript("fast output"))) },
			Input: api.StartRunCommand{RunID: "run-beta", RootTaskID: "root", Request: "b"},
			Assertions: []eval.Assertion{
				assertions.RunTerminatedWithStatus{Status: api.RunStatusCompleted},
			},
		},
	}

	// Two param sets that swap the scripted model: each combination produces a
	// distinct output the case then asserts on, proving the param was applied.
	params := eval.MatrixParams{Params: []eval.MatrixParam{
		{
			Name: "fast",
			Apply: func(base eval.EvalCase) eval.EvalCase {
				v := base
				v.Setup = func() eval.Harness {
					return eval.NewHarness(eval.WithModel("fast"), eval.WithScript(textScript("fast output")))
				}
				v.Assertions = []eval.Assertion{assertions.OutputContains{Substring: "fast"}}
				return v
			},
		},
		{
			Name: "smart",
			Apply: func(base eval.EvalCase) eval.EvalCase {
				v := base
				v.Setup = func() eval.Harness {
					return eval.NewHarness(eval.WithModel("smart"), eval.WithScript(textScript("smart output")))
				}
				v.Assertions = []eval.Assertion{assertions.OutputContains{Substring: "smart"}}
				return v
			},
		},
	}}

	results := eval.RunMatrix(t, cases, params)
	if got, want := len(results), len(params.Params)*len(cases); got != want {
		t.Fatalf("RunMatrix produced %d results, want %d (params x cases)", got, want)
	}

	wantNames := map[string]bool{
		"fast/alpha":  false,
		"fast/beta":   false,
		"smart/alpha": false,
		"smart/beta":  false,
	}
	for _, r := range results {
		if !r.Passed {
			t.Fatalf("combination %q failed: %+v", r.Case, r.Failures)
		}
		if _, ok := wantNames[r.Case]; !ok {
			t.Fatalf("unexpected combination name %q", r.Case)
		}
		wantNames[r.Case] = true
	}
	for name, seen := range wantNames {
		if !seen {
			t.Fatalf("combination %q was not executed", name)
		}
	}
}

func TestEvalRunMatrix_EmptyParamsRunsEachCaseOnce(t *testing.T) {
	cases := []eval.EvalCase{
		{
			Name:  "solo",
			Setup: func() eval.Harness { return eval.NewHarness(eval.WithScript(textScript("ok"))) },
			Input: api.StartRunCommand{RunID: "run-solo", RootTaskID: "root", Request: "x"},
			Assertions: []eval.Assertion{
				assertions.RunTerminatedWithStatus{Status: api.RunStatusCompleted},
			},
		},
	}
	results := eval.RunMatrix(t, cases, eval.MatrixParams{})
	if len(results) != 1 {
		t.Fatalf("empty params should run each case once, got %d results", len(results))
	}
	if results[0].Case != "default/solo" {
		t.Fatalf("default param should namespace the case, got %q", results[0].Case)
	}
	if !results[0].Passed {
		t.Fatalf("solo case failed: %+v", results[0].Failures)
	}
}

func TestSummarizeUsage_FoldsRecords(t *testing.T) {
	records := []api.UsageRecord{
		{InputTokens: 10, OutputTokens: 5, ToolCalls: 1, Credits: 2, DurationMS: 100},
		{InputTokens: 3, OutputTokens: 7, ToolCalls: 2, Credits: 4, DurationMS: 50},
	}
	got := eval.SummarizeUsage(records)
	want := eval.UsageSummary{Records: 2, InputTokens: 13, OutputTokens: 12, ToolCalls: 3, Credits: 6, DurationMS: 150}
	if got != want {
		t.Fatalf("SummarizeUsage = %+v, want %+v", got, want)
	}
}
