package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Viking602/venat/message"
	"github.com/Viking602/venat/provider"
	"github.com/Viking602/venat/tool"
)

type stepDeciderFunc func(LoopSnapshot) (StepDecision, error)

func (f stepDeciderFunc) Decide(snapshot LoopSnapshot) (StepDecision, error) {
	return f(snapshot)
}

func TestRunMessagesStepDeciderFinishStopsEarly(t *testing.T) {
	prov := &alwaysToolProvider{}
	engine := newLoopToolEngine(t, prov)
	output, err := engine.RunMessages(context.Background(), LoopInput{
		Model:    "test-model",
		Messages: []message.Message{message.NewText(message.RoleUser, "loop")},
		StepDecider: stepDeciderFunc(func(snapshot LoopSnapshot) (StepDecision, error) {
			if len(snapshot.Steps) >= 2 {
				return StepDecisionFinish, nil
			}
			return StepDecisionContinue, nil
		}),
	})
	if err != nil {
		t.Fatalf("RunMessages() error = %v", err)
	}
	if output.Iterations != 2 || output.StopReason != provider.StopReasonComplete {
		t.Fatalf("Iterations=%d StopReason=%q, want 2 / complete", output.Iterations, output.StopReason)
	}
	if prov.calls != 2 {
		t.Fatalf("provider calls = %d, want 2", prov.calls)
	}
	if got := output.Steps[len(output.Steps)-1].Decision; got != StepDecisionFinish {
		t.Fatalf("final decision = %q, want finish", got)
	}
}

func TestRunMessagesStepDeciderContinueIsTransparent(t *testing.T) {
	for _, test := range []struct {
		name     string
		decision StepDecision
	}{
		{name: "explicit continue", decision: StepDecisionContinue},
		{name: "empty decision", decision: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			prov := &alwaysToolProvider{}
			engine := newLoopToolEngine(t, prov)
			output, err := engine.RunMessages(context.Background(), LoopInput{
				Model:    "test-model",
				Messages: []message.Message{message.NewText(message.RoleUser, "loop")},
				StepDecider: stepDeciderFunc(func(LoopSnapshot) (StepDecision, error) {
					return test.decision, nil
				}),
			})
			if err != nil {
				t.Fatalf("RunMessages() error = %v", err)
			}
			if output.Iterations != 12 || output.StopReason != provider.StopReasonMaxTurns {
				t.Fatalf("Iterations=%d StopReason=%q, want 12 / max_turns", output.Iterations, output.StopReason)
			}
		})
	}
}

func TestRunMessagesStepDeciderFailAborts(t *testing.T) {
	engine := newLoopToolEngine(t, &alwaysToolProvider{})
	output, err := engine.RunMessages(context.Background(), LoopInput{
		Model:    "test-model",
		Messages: []message.Message{message.NewText(message.RoleUser, "loop")},
		StepDecider: stepDeciderFunc(func(LoopSnapshot) (StepDecision, error) {
			return StepDecisionFail, nil
		}),
	})
	if !errors.Is(err, ErrStepAborted) {
		t.Fatalf("error = %v, want ErrStepAborted", err)
	}
	if output.StopReason != provider.StopReasonError || output.Iterations != 1 {
		t.Fatalf("StopReason=%q Iterations=%d, want error / 1", output.StopReason, output.Iterations)
	}
	if got := output.Steps[len(output.Steps)-1].Decision; got != StepDecisionFail {
		t.Fatalf("final decision = %q, want fail", got)
	}
}

func TestRunMessagesStepDeciderErrorAborts(t *testing.T) {
	boom := errors.New("decider boom")
	engine := newLoopToolEngine(t, &alwaysToolProvider{})
	_, err := engine.RunMessages(context.Background(), LoopInput{
		Model:    "test-model",
		Messages: []message.Message{message.NewText(message.RoleUser, "loop")},
		StepDecider: stepDeciderFunc(func(LoopSnapshot) (StepDecision, error) {
			return "", boom
		}),
	})
	if !errors.Is(err, ErrStepAborted) || !errors.Is(err, boom) {
		t.Fatalf("error = %v, want ErrStepAborted and decider cause", err)
	}
}

func TestRunMessagesStepDeciderNotConsultedOnTerminalTool(t *testing.T) {
	driver := &scriptedProvider{turns: [][]provider.Event{{
		{Kind: provider.EventToolCall, ToolCall: &message.ToolCall{ID: "c1", Name: "submit_report", Arguments: json.RawMessage(`{"answer":"done"}`)}},
		{Kind: provider.EventDone, StopReason: provider.StopReasonToolUse},
	}}}
	engine := Engine{Provider: driver, Tools: tool.NewBus(terminalTool{})}
	consulted := false
	output, err := engine.RunMessages(context.Background(), LoopInput{
		Model:         "test-model",
		Messages:      []message.Message{message.NewText(message.RoleUser, "finish")},
		MaxIterations: 3,
		StepDecider: stepDeciderFunc(func(LoopSnapshot) (StepDecision, error) {
			consulted = true
			return StepDecisionFail, nil
		}),
	})
	if err != nil {
		t.Fatalf("RunMessages() error = %v", err)
	}
	if consulted {
		t.Fatal("StepDecider ran on terminal-tool completion")
	}
	if got := output.Steps[len(output.Steps)-1].Decision; got != StepDecisionFinish {
		t.Fatalf("final decision = %q, want finish", got)
	}
}

func TestRunMessagesStepDeciderNotConsultedOnNaturalFinish(t *testing.T) {
	driver := &scriptedProvider{turns: [][]provider.Event{{
		{Kind: provider.EventTextDelta, Text: "done"},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	}}}
	consulted := false
	_, err := (Engine{Provider: driver}).RunMessages(context.Background(), LoopInput{
		Model:         "test-model",
		Messages:      []message.Message{message.NewText(message.RoleUser, "hi")},
		MaxIterations: 3,
		StepDecider: stepDeciderFunc(func(LoopSnapshot) (StepDecision, error) {
			consulted = true
			return StepDecisionFail, nil
		}),
	})
	if err != nil {
		t.Fatalf("RunMessages() error = %v", err)
	}
	if consulted {
		t.Fatal("StepDecider ran on natural completion")
	}
}

func TestEngineRunStepDeciderFailSurfacesStepAbortedFailure(t *testing.T) {
	engine := newLoopToolEngine(t, &alwaysToolProvider{})
	engine.StepDecider = stepDeciderFunc(func(LoopSnapshot) (StepDecision, error) {
		return StepDecisionFail, nil
	})
	result := engine.Run(context.Background(), Request{Prompt: "loop"}, OutputPolicy{})
	if result.Failure == nil || result.Failure.Kind != FailureKindStepAborted {
		t.Fatalf("Failure = %#v, want FailureKindStepAborted", result.Failure)
	}
	if !errors.Is(result.Failure, ErrStepAborted) {
		t.Fatalf("Failure does not unwrap to ErrStepAborted: %#v", result.Failure)
	}
}
