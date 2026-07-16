package agent

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/provider"
)

// recordingCompactor counts how many times the loop invokes it and, when
// replace is set, returns that slice as the compacted history. A non-nil fail is
// returned instead, to exercise the loop's compaction-error path.
type recordingCompactor struct {
	calls    int
	replace  []message.Message
	fail     error
	received []message.Message
}

func (c *recordingCompactor) compact(_ context.Context, history []message.Message) ([]message.Message, error) {
	c.calls++
	c.received = append(c.received[:0], history...)
	if c.fail != nil {
		return history, c.fail
	}
	if c.replace != nil {
		return append([]message.Message{}, c.replace...), nil
	}
	return history, nil
}

func usagePerTurn(total int) provider.Usage {
	// Split across input/output so the usage mirrors a real turn; TotalTokens is
	// what the budget and compaction trigger read.
	return provider.Usage{InputTokens: total - total/2, OutputTokens: total / 2, TotalTokens: total}
}

func TestRunMessagesCompactsWhenTokenBudgetApproached(t *testing.T) {
	// 10 tokens/turn against a 45-token budget (headroom band = 45/5 = 9): the
	// loop enters the band once 36+ tokens are spent and compacts before the next
	// turn.
	comp := &recordingCompactor{}
	engine := newLoopToolEngine(t, &usageToolProvider{perTurn: usagePerTurn(10)})
	output, _ := engine.RunMessages(context.Background(), LoopInput{
		Model:     "test-model",
		Messages:  []message.Message{message.NewText(message.RoleUser, "loop")},
		MaxTokens: 45,
		Compact:   comp.compact,
	})
	if comp.calls == 0 {
		t.Fatalf("Compact was never invoked; want at least one call once the token budget is approached (Iterations=%d)", output.Iterations)
	}
	// The first compaction lands only after the budget enters the headroom band,
	// never on the opening turns.
	if output.Iterations < 4 {
		t.Fatalf("loop ran only %d turns; compaction should trigger deep into the budget, not early", output.Iterations)
	}
}

func TestRunMessagesDoesNotCompactWithoutTokenBudget(t *testing.T) {
	comp := &recordingCompactor{}
	engine := newLoopToolEngine(t, &usageToolProvider{perTurn: usagePerTurn(10)})
	output, err := engine.RunMessages(context.Background(), LoopInput{
		Model:    "test-model",
		Messages: []message.Message{message.NewText(message.RoleUser, "loop")},
		// MaxTokens unset -> no token boundary to approach.
		Compact: comp.compact,
	})
	if err != nil {
		t.Fatalf("RunMessages() error = %v", err)
	}
	if comp.calls != 0 {
		t.Fatalf("Compact ran %d times with no token budget; it must never compact when MaxTokens is zero", comp.calls)
	}
	if output.Iterations != 12 {
		t.Fatalf("Iterations = %d, want the 12 ceiling", output.Iterations)
	}
}

func TestRunMessagesDoesNotCompactWhenBudgetRoomy(t *testing.T) {
	comp := &recordingCompactor{}
	engine := newLoopToolEngine(t, &usageToolProvider{perTurn: usagePerTurn(10)})
	_, err := engine.RunMessages(context.Background(), LoopInput{
		Model:     "test-model",
		Messages:  []message.Message{message.NewText(message.RoleUser, "loop")},
		MaxTokens: 1_000_000, // 120 tokens over 12 turns never approaches this
		Compact:   comp.compact,
	})
	if err != nil {
		t.Fatalf("RunMessages() error = %v", err)
	}
	if comp.calls != 0 {
		t.Fatalf("Compact ran %d times with a roomy budget; it must only fire near the ceiling", comp.calls)
	}
}

func TestRunMessagesAdoptsCompactedHistory(t *testing.T) {
	// When Compact fires it replaces the working history, so the marker it
	// returns must survive into the loop's output messages.
	marker := message.NewText(message.RoleSystem, "COMPACTED-MARKER")
	comp := &recordingCompactor{replace: []message.Message{marker, message.NewText(message.RoleUser, "continue")}}
	engine := newLoopToolEngine(t, &usageToolProvider{perTurn: usagePerTurn(10)})
	output, _ := engine.RunMessages(context.Background(), LoopInput{
		Model:     "test-model",
		Messages:  []message.Message{message.NewText(message.RoleUser, "loop")},
		MaxTokens: 45,
		Compact:   comp.compact,
	})
	if comp.calls == 0 {
		t.Fatal("Compact never fired; cannot assert the loop adopted the compacted history")
	}
	if len(output.Messages) == 0 || output.Messages[0].Text != "COMPACTED-MARKER" {
		t.Fatalf("loop did not adopt the compacted history; first message = %#v", output.Messages)
	}
}

func TestRunMessagesCompactionErrorAbortsLoop(t *testing.T) {
	boom := errors.New("compact boom")
	comp := &recordingCompactor{fail: boom}
	engine := newLoopToolEngine(t, &usageToolProvider{perTurn: usagePerTurn(10)})
	_, err := engine.RunMessages(context.Background(), LoopInput{
		Model:     "test-model",
		Messages:  []message.Message{message.NewText(message.RoleUser, "loop")},
		MaxTokens: 45,
		Compact:   comp.compact,
	})
	if !errors.Is(err, boom) {
		t.Fatalf("error = %v, want it to wrap the compaction error", err)
	}
}

func TestRunMessagesRejectsIncompleteCompactedToolTurn(t *testing.T) {
	comp := &recordingCompactor{replace: []message.Message{
		message.NewText(message.RoleSystem, "compacted"),
		{
			Role: message.RoleAssistant,
			ToolCalls: []message.ToolCall{{
				ID:   "missing-result",
				Name: "tool",
			}},
		},
	}}
	engine := newLoopToolEngine(t, &usageToolProvider{perTurn: usagePerTurn(10)})
	output, err := engine.RunMessages(context.Background(), LoopInput{
		Model:     "test-model",
		Messages:  []message.Message{message.NewText(message.RoleUser, "original")},
		MaxTokens: 45,
		Compact:   comp.compact,
	})
	if !errors.Is(err, message.ErrIncompleteToolTurn) {
		t.Fatalf("error = %v, want errors.Is(err, message.ErrIncompleteToolTurn)", err)
	}
	if comp.calls == 0 {
		t.Fatal("Compact was never invoked")
	}
	if !reflect.DeepEqual(output.Messages, comp.received) {
		t.Fatalf("partial output did not preserve the pre-compaction history:\noutput: %#v\noriginal: %#v", output.Messages, comp.received)
	}
}

// recordingContextManager is a ContextManager whose Compact records calls, used
// to prove Engine.Run wires ContextManager.Compact into the loop.
type recordingContextManager struct {
	compactCalls int
}

func (*recordingContextManager) Build(_ context.Context, task api.Task) ([]message.Message, error) {
	return []message.Message{
		message.NewText(message.RoleSystem, "You are a test agent."),
		message.NewText(message.RoleUser, task.Goal),
	}, nil
}

func (c *recordingContextManager) Compact(_ context.Context, history []message.Message) ([]message.Message, error) {
	c.compactCalls++
	return history, nil
}

func TestEngineRunWiresContextManagerCompact(t *testing.T) {
	cm := &recordingContextManager{}
	engine := newLoopToolEngine(t, &usageToolProvider{perTurn: usagePerTurn(10)})
	engine.ContextBuilder = cm
	engine.LoopPolicy.Budget = &api.TaskBudget{MaxTokens: 45}

	result := engine.Run(context.Background(), api.Task{Goal: "loop"}, OutputPolicy{})
	if cm.compactCalls == 0 {
		t.Fatalf("Engine.Run did not invoke ContextManager.Compact; failure=%#v", result.Failure)
	}
}
