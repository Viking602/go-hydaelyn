package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/tool"
)

// stepPolicyFunc adapts a plain function to the StepPolicy interface for tests.
type stepPolicyFunc func(LoopSnapshot) (StepDecision, error)

func (f stepPolicyFunc) Next(s LoopSnapshot) (StepDecision, error) { return f(s) }

func TestRunMessagesStepPolicyFinishStopsEarly(t *testing.T) {
	// alwaysToolProvider never finishes on its own, so without a policy this run
	// would hit the 12-iteration ceiling. A StepPolicy that returns Finish once
	// two steps are recorded stops it at two — the "iterate until a predicate
	// over the step trace holds" pattern.
	prov := &alwaysToolProvider{}
	engine := newLoopToolEngine(t, prov)

	output, err := engine.RunMessages(context.Background(), LoopInput{
		Model:    "test-model",
		Messages: []message.Message{message.NewText(message.RoleUser, "loop")},
		StepPolicy: stepPolicyFunc(func(s LoopSnapshot) (StepDecision, error) {
			if len(s.Steps) >= 2 {
				return StepDecisionFinish, nil
			}
			return StepDecisionContinue, nil
		}),
	})
	if err != nil {
		t.Fatalf("RunMessages() error = %v", err)
	}
	if output.Iterations != 2 {
		t.Fatalf("Iterations = %d, want 2 (policy finishes after the second step)", output.Iterations)
	}
	if output.StopReason != provider.StopReasonComplete {
		t.Fatalf("StopReason = %q, want complete", output.StopReason)
	}
	if prov.calls != 2 {
		t.Fatalf("provider calls = %d, want 2 (loop stopped early)", prov.calls)
	}
	if got := output.Steps[len(output.Steps)-1].Decision; got != StepDecisionFinish {
		t.Fatalf("final step Decision = %q, want finish (override recorded on the trace)", got)
	}
	requireNoStepTimestamps(t, output.Steps)
}

func TestRunMessagesStepPolicyContinueIsTransparent(t *testing.T) {
	for _, tc := range []struct {
		name     string
		decision StepDecision
	}{
		{"explicit continue", StepDecisionContinue},
		{"empty decision", StepDecision("")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prov := &alwaysToolProvider{}
			engine := newLoopToolEngine(t, prov)
			output, err := engine.RunMessages(context.Background(), LoopInput{
				Model:    "test-model",
				Messages: []message.Message{message.NewText(message.RoleUser, "loop")},
				StepPolicy: stepPolicyFunc(func(LoopSnapshot) (StepDecision, error) {
					return tc.decision, nil
				}),
			})
			if err != nil {
				t.Fatalf("RunMessages() error = %v", err)
			}
			if output.Iterations != 12 || output.StopReason != provider.StopReasonMaxTurns {
				t.Fatalf("Iterations=%d StopReason=%q, want 12 / max_turns (a continue/empty decision is transparent)",
					output.Iterations, output.StopReason)
			}
		})
	}
}

func TestRunMessagesStepPolicyFailAborts(t *testing.T) {
	prov := &alwaysToolProvider{}
	engine := newLoopToolEngine(t, prov)
	output, err := engine.RunMessages(context.Background(), LoopInput{
		Model:    "test-model",
		Messages: []message.Message{message.NewText(message.RoleUser, "loop")},
		StepPolicy: stepPolicyFunc(func(LoopSnapshot) (StepDecision, error) {
			return StepDecisionFail, nil
		}),
	})
	if !errors.Is(err, ErrStepAborted) {
		t.Fatalf("error = %v, want ErrStepAborted", err)
	}
	if output.StopReason != provider.StopReasonError {
		t.Fatalf("StopReason = %q, want error", output.StopReason)
	}
	if output.Iterations != 1 {
		t.Fatalf("Iterations = %d, want 1 (failed at the first continue boundary)", output.Iterations)
	}
	if got := output.Steps[len(output.Steps)-1].Decision; got != StepDecisionFail {
		t.Fatalf("final step Decision = %q, want fail", got)
	}
}

func TestRunMessagesStepPolicyHandoffStopsClean(t *testing.T) {
	prov := &alwaysToolProvider{}
	engine := newLoopToolEngine(t, prov)
	output, err := engine.RunMessages(context.Background(), LoopInput{
		Model:    "test-model",
		Messages: []message.Message{message.NewText(message.RoleUser, "loop")},
		StepPolicy: stepPolicyFunc(func(LoopSnapshot) (StepDecision, error) {
			return StepDecisionHandoff, nil
		}),
	})
	if err != nil {
		t.Fatalf("RunMessages() error = %v (handoff is a clean stop)", err)
	}
	if output.StopReason != provider.StopReasonComplete {
		t.Fatalf("StopReason = %q, want complete", output.StopReason)
	}
	if got := output.Steps[len(output.Steps)-1].Decision; got != StepDecisionHandoff {
		t.Fatalf("final step Decision = %q, want handoff", got)
	}
}

func TestRunMessagesStepPolicyNextErrorAborts(t *testing.T) {
	boom := errors.New("policy boom")
	prov := &alwaysToolProvider{}
	engine := newLoopToolEngine(t, prov)
	_, err := engine.RunMessages(context.Background(), LoopInput{
		Model:    "test-model",
		Messages: []message.Message{message.NewText(message.RoleUser, "loop")},
		StepPolicy: stepPolicyFunc(func(LoopSnapshot) (StepDecision, error) {
			return "", boom
		}),
	})
	if !errors.Is(err, ErrStepAborted) {
		t.Fatalf("error = %v, want ErrStepAborted from a policy Next error", err)
	}
}

func TestRunMessagesStepPolicyNotConsultedOnTerminalTool(t *testing.T) {
	// The terminal tool finishes the loop on turn 1 before any continue boundary,
	// so a Fail policy must never fire — the run completes cleanly.
	driver := &scriptedProvider{
		turns: [][]provider.Event{{
			{Kind: provider.EventToolCall, ToolCall: &message.ToolCall{ID: "c1", Name: "submit_report", Arguments: json.RawMessage(`{"answer":"done"}`)}},
			{Kind: provider.EventDone, StopReason: provider.StopReasonToolUse},
		}},
	}
	engine := Engine{Provider: driver, Tools: tool.NewBus(terminalTool{})}
	consulted := false
	output, err := engine.RunMessages(context.Background(), LoopInput{
		Model:         "test-model",
		Messages:      []message.Message{message.NewText(message.RoleUser, "finish")},
		MaxIterations: 3,
		StepPolicy: stepPolicyFunc(func(LoopSnapshot) (StepDecision, error) {
			consulted = true
			return StepDecisionFail, nil
		}),
	})
	if err != nil {
		t.Fatalf("RunMessages() error = %v, want a clean terminal-tool finish", err)
	}
	if consulted {
		t.Fatal("StepPolicy fired on a terminal-tool finish; it must only run at continue boundaries")
	}
	if got := output.Steps[len(output.Steps)-1].Decision; got != StepDecisionFinish {
		t.Fatalf("final step Decision = %q, want finish", got)
	}
}

func TestRunMessagesStepPolicyNotConsultedOnNaturalFinish(t *testing.T) {
	// A no-tool-call turn is a natural finish, not a continue boundary, so the
	// policy must not fire there either.
	driver := &scriptedProvider{
		turns: [][]provider.Event{{
			{Kind: provider.EventTextDelta, Text: "done"},
			{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
		}},
	}
	engine := Engine{Provider: driver}
	consulted := false
	_, err := engine.RunMessages(context.Background(), LoopInput{
		Model:         "test-model",
		Messages:      []message.Message{message.NewText(message.RoleUser, "hi")},
		MaxIterations: 3,
		StepPolicy: stepPolicyFunc(func(LoopSnapshot) (StepDecision, error) {
			consulted = true
			return StepDecisionFail, nil
		}),
	})
	if err != nil {
		t.Fatalf("RunMessages() error = %v, want a clean no-tool finish", err)
	}
	if consulted {
		t.Fatal("StepPolicy fired on a no-tool-call finish; it must only run at continue boundaries")
	}
}

func TestEngineRunStepPolicyFailSurfacesStepAbortedFailure(t *testing.T) {
	engine := newLoopToolEngine(t, &alwaysToolProvider{})
	engine.StepPolicy = stepPolicyFunc(func(LoopSnapshot) (StepDecision, error) {
		return StepDecisionFail, nil
	})
	result := engine.Run(context.Background(), api.Task{Goal: "loop"}, OutputPolicy{})
	if result.Failure == nil || result.Failure.Kind != FailureKindStepAborted {
		t.Fatalf("Failure = %#v, want FailureKindStepAborted", result.Failure)
	}
	if !errors.Is(result.Failure, ErrStepAborted) {
		t.Fatalf("Failure does not unwrap to ErrStepAborted: %#v", result.Failure)
	}
}
