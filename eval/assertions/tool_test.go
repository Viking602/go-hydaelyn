package assertions_test

import (
	"context"
	"testing"

	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/eval"
	"github.com/Viking602/go-hydaelyn/eval/assertions"
	"github.com/Viking602/go-hydaelyn/eval/matcher"
	"github.com/Viking602/go-hydaelyn/provider"
)

func textScript(text string) []provider.Event {
	return []provider.Event{
		{Kind: provider.EventTextDelta, Text: text},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	}
}

// writeToolCall records a tool-sourced blackboard item so the tool assertions
// observe a tool invocation through the public Runner facade.
func writeToolCall(t *testing.T, h eval.Harness, runID, tool, arg string) {
	t.Helper()
	if err := h.Runner().WriteItem(context.Background(), api.BlackboardItem{
		RunID:      runID,
		Type:       api.BlackboardItemEvidence,
		Source:     api.SourceIdentity{Type: api.SourceTool, ID: tool},
		Visibility: api.BlackboardVisibilityAgentVisible,
		Payload:    arg,
	}); err != nil {
		t.Fatalf("WriteItem(%s) error = %v", tool, err)
	}
}

func TestAssertion_ToolCalled(t *testing.T) {
	const runID = "run-toolcalled"
	c := eval.EvalCase{
		Name: "tool-called",
		Setup: func() eval.Harness {
			h := eval.NewHarness(eval.WithScript(textScript("ok")))
			writeToolCall(t, h, runID, "search", `{"q":"paris"}`)
			return h
		},
		Input: api.StartRunCommand{RunID: runID, RootTaskID: "root", Request: "find"},
		Assertions: []eval.Assertion{
			assertions.ToolCalled{Tool: "search"},
			assertions.ToolNotCalled{Tool: "delete"},
			assertions.ToolCalledNTimes{Tool: "search", Times: 1},
		},
	}
	if res := eval.Run(t, c); !res.Passed {
		t.Fatalf("expected pass, got %+v", res.Failures)
	}
}

func TestAssertion_ToolNotCalled_DetectsViolation(t *testing.T) {
	run, h := runWithToolCall(t, "run-toolnot", "delete", "{}")
	if err := (assertions.ToolNotCalled{Tool: "delete"}).Check(context.Background(), run, h); err == nil {
		t.Fatalf("expected ToolNotCalled to fail when the tool was called")
	}
	if err := (assertions.ToolCalled{Tool: "delete"}).Check(context.Background(), run, h); err != nil {
		t.Fatalf("expected ToolCalled to pass, got %v", err)
	}
}

func TestAssertion_ToolCalledNTimes(t *testing.T) {
	const runID = "run-tooln"
	c := eval.EvalCase{
		Name: "tool-n-times",
		Setup: func() eval.Harness {
			h := eval.NewHarness(eval.WithScript(textScript("ok")))
			writeToolCall(t, h, runID, "fetch", "a")
			writeToolCall(t, h, runID, "fetch", "b")
			return h
		},
		Input: api.StartRunCommand{RunID: runID, RootTaskID: "root", Request: "fetch twice"},
		Assertions: []eval.Assertion{
			assertions.ToolCalledNTimes{Tool: "fetch", Times: 2},
		},
	}
	if res := eval.Run(t, c); !res.Passed {
		t.Fatalf("expected pass, got %+v", res.Failures)
	}
}

func TestAssertion_ToolCalledNTimes_DedupesEventAndBlackboard(t *testing.T) {
	const runID = "run-tool-dedup"
	run, h := runToTerminal(t, runID, "drive")
	// One logical "deploy" call surfaces on BOTH public signals: an
	// ActionAttemptStarted event (the canonical invocation) and a tool-sourced
	// blackboard item (its output contribution). It must count once, not twice.
	appendEvent(t, h, runID, api.EventActionAttemptStarted, map[string]any{"toolName": "deploy"})
	writeToolCall(t, h, runID, "deploy", `{"target":"prod"}`)

	if err := (assertions.ToolCalledNTimes{Tool: "deploy", Times: 1}).Check(context.Background(), run, h); err != nil {
		t.Fatalf("a call seen on both signals should count once, got %v", err)
	}
	// The blackboard arg observation stays available for ToolCalledWithArg.
	withArg := assertions.ToolCalledWithArg{Tool: "deploy", Matcher: matcher.JSONContains(map[string]any{"target": "prod"})}
	if err := withArg.Check(context.Background(), run, h); err != nil {
		t.Fatalf("blackboard arg should remain matchable after dedup, got %v", err)
	}
}

func TestAssertion_ToolCalledWithArg_JSONMatcher(t *testing.T) {
	run, h := runWithToolCall(t, "run-toolarg", "geocode", `{"city":"paris","country":"fr"}`)
	a := assertions.ToolCalledWithArg{
		Tool:    "geocode",
		Matcher: matcher.JSONContains(map[string]any{"city": "paris"}),
	}
	if err := a.Check(context.Background(), run, h); err != nil {
		t.Fatalf("expected JSON-matcher arg assertion to pass, got %v", err)
	}
}

func TestAssertion_ToolCalledWithArg_NoMatchFails(t *testing.T) {
	run, h := runWithToolCall(t, "run-toolarg-miss", "geocode", `{"city":"berlin"}`)
	a := assertions.ToolCalledWithArg{
		Tool:    "geocode",
		Matcher: matcher.JSONContains(map[string]any{"city": "paris"}),
	}
	if err := a.Check(context.Background(), run, h); err == nil {
		t.Fatalf("expected arg mismatch to fail")
	}
}

func TestAssertion_ToolCalledWithArg_NotCalledFails(t *testing.T) {
	run, h := runWithToolCall(t, "run-toolarg-absent", "other", "{}")
	a := assertions.ToolCalledWithArg{
		Tool:    "geocode",
		Matcher: matcher.JSONContains(map[string]any{"city": "paris"}),
	}
	if err := a.Check(context.Background(), run, h); err == nil {
		t.Fatalf("expected absent tool to fail")
	}
}
