package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/Viking602/venat/message"
	"github.com/Viking602/venat/provider"
	"github.com/Viking602/venat/tool"
	"github.com/Viking602/venat/tool/kit"
)

func TestEngineOutputGuardrailCanReplaceFinalOutput(t *testing.T) {
	driver := &scriptedProvider{
		turns: [][]provider.Event{{
			{Kind: provider.EventTextDelta, Text: "unsafe draft"},
			{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
		}},
	}
	engine := Engine{Provider: driver}
	result, err := engine.RunMessages(context.Background(), LoopInput{
		Model: "test-model",
		Messages: []message.Message{
			message.NewText(message.RoleUser, "hi"),
		},
		MaxIterations: 1,
		OutputGuardrails: []OutputGuardrail{
			NewOutputGuardrail("replace", func(_ context.Context, input OutputGuardrailInput) (OutputGuardrailResult, error) {
				if input.Output.Text == "unsafe draft" {
					return ReplaceOutput(message.NewText(message.RoleAssistant, "safe answer")), nil
				}
				return AllowOutput(), nil
			}),
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	last := result.Messages[len(result.Messages)-1]
	if last.Text != "safe answer" {
		t.Fatalf("expected replaced final output, got %#v", last)
	}
	if len(result.Messages) != 2 {
		t.Fatalf("expected replaced output to avoid keeping rejected final answer, got %#v", result.Messages)
	}
}

func TestEngineOutputGuardrailCanRetryFinalOutput(t *testing.T) {
	driver := &scriptedProvider{
		turns: [][]provider.Event{
			{
				{Kind: provider.EventTextDelta, Text: "draft answer"},
				{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
			},
			{
				{Kind: provider.EventTextDelta, Text: "final answer"},
				{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
			},
		},
	}
	engine := Engine{Provider: driver}
	result, err := engine.RunMessages(context.Background(), LoopInput{
		Model: "test-model",
		Messages: []message.Message{
			message.NewText(message.RoleUser, "hi"),
		},
		MaxIterations: 3,
		OutputGuardrails: []OutputGuardrail{
			NewOutputGuardrail("retry", func(_ context.Context, input OutputGuardrailInput) (OutputGuardrailResult, error) {
				if input.Output.Text == "draft answer" {
					return RetryOutput(message.NewText(message.RoleUser, "please revise")), nil
				}
				return AllowOutput(), nil
			}),
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Iterations != 2 {
		t.Fatalf("expected second turn after guardrail retry, got %d iterations", result.Iterations)
	}
	last := result.Messages[len(result.Messages)-1]
	if last.Text != "final answer" {
		t.Fatalf("expected retried final output, got %#v", last)
	}
	if len(driver.requests) != 2 {
		t.Fatalf("expected retry to trigger second provider call, got %d", len(driver.requests))
	}
	second := driver.requests[1].Messages
	if second[len(second)-1].Role != message.RoleUser || second[len(second)-1].Text != "please revise" {
		t.Fatalf("expected retry feedback appended to second request, got %#v", second)
	}
}

func TestUnlimitedIterationsAlsoAppliesToOutputGuardrailRetries(t *testing.T) {
	turns := make([][]provider.Event, 14)
	for iteration := range turns {
		turns[iteration] = []provider.Event{
			{Kind: provider.EventTextDelta, Text: "draft"},
			{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
		}
	}
	driver := &scriptedProvider{turns: turns}
	engine := Engine{Provider: driver}
	result, err := engine.RunMessages(context.Background(), LoopInput{
		Model:               "test-model",
		Messages:            []message.Message{message.NewText(message.RoleUser, "keep revising")},
		MaxIterations:       2,
		UnlimitedIterations: true,
		OutputGuardrails: []OutputGuardrail{
			NewOutputGuardrail("retry", func(_ context.Context, input OutputGuardrailInput) (OutputGuardrailResult, error) {
				if !input.UnlimitedIterations {
					t.Fatal("guardrail did not receive unlimited iteration policy")
				}
				if input.Iteration < 13 {
					return RetryOutput(message.NewText(message.RoleUser, "revise again")), nil
				}
				return AllowOutput(), nil
			}),
		},
	})
	if err != nil {
		t.Fatalf("RunMessages() error = %v", err)
	}
	if result.StopReason != provider.StopReasonComplete || driver.callIndex != 13 {
		t.Fatalf("result stop=%q provider calls=%d, want completion beyond the default 12-turn ceiling", result.StopReason, driver.callIndex)
	}
}

func TestEngineOutputGuardrailRetryDoesNotIncludeRejectedOutputByDefault(t *testing.T) {
	driver := &scriptedProvider{
		turns: [][]provider.Event{
			{
				{Kind: provider.EventTextDelta, Text: "draft answer"},
				{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
			},
			{
				{Kind: provider.EventTextDelta, Text: "safe answer"},
				{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
			},
		},
	}
	engine := Engine{Provider: driver}
	_, err := engine.RunMessages(context.Background(), LoopInput{
		Model:         "test-model",
		Messages:      []message.Message{message.NewText(message.RoleUser, "hi")},
		MaxIterations: 3,
		OutputGuardrails: []OutputGuardrail{
			NewOutputGuardrail("retry", func(_ context.Context, input OutputGuardrailInput) (OutputGuardrailResult, error) {
				if input.Output.Text == "draft answer" {
					return RetryOutput(message.NewText(message.RoleUser, "please revise safely")), nil
				}
				return AllowOutput(), nil
			}),
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	second := driver.requests[1].Messages
	if len(second) != 2 {
		t.Fatalf("expected original user message plus retry feedback only, got %#v", second)
	}
	for _, msg := range second {
		if msg.Role == message.RoleAssistant && msg.Text == "draft answer" {
			t.Fatalf("expected rejected assistant output to stay out of retry context, got %#v", second)
		}
	}
}

func TestEngineOutputGuardrailRetryCanIncludeRejectedOutputWithPolicy(t *testing.T) {
	driver := &scriptedProvider{
		turns: [][]provider.Event{
			{
				{Kind: provider.EventTextDelta, Text: "draft answer"},
				{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
			},
			{
				{Kind: provider.EventTextDelta, Text: "safe answer"},
				{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
			},
		},
	}
	engine := Engine{Provider: driver}
	_, err := engine.RunMessages(context.Background(), LoopInput{
		Model:         "test-model",
		Messages:      []message.Message{message.NewText(message.RoleUser, "hi")},
		MaxIterations: 3,
		OutputGuardrails: []OutputGuardrail{
			NewOutputGuardrail("retry", func(_ context.Context, input OutputGuardrailInput) (OutputGuardrailResult, error) {
				if input.Output.Text == "draft answer" {
					return RetryOutputWithPolicy(RetryPolicy{IncludeRejectedOutput: true}, message.NewText(message.RoleUser, "please revise safely")), nil
				}
				return AllowOutput(), nil
			}),
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	second := driver.requests[1].Messages
	if len(second) != 3 {
		t.Fatalf("expected original user message, rejected output, and feedback, got %#v", second)
	}
	if second[1].Role != message.RoleAssistant || second[1].Text != "draft answer" {
		t.Fatalf("expected rejected assistant output to be included when policy allows it, got %#v", second)
	}
}

func TestEngineOutputGuardrailCanBlockFinalOutput(t *testing.T) {
	driver := &scriptedProvider{
		turns: [][]provider.Event{{
			{Kind: provider.EventTextDelta, Text: "unsafe answer"},
			{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
		}},
	}
	engine := Engine{Provider: driver}
	_, err := engine.RunMessages(context.Background(), LoopInput{
		Model: "test-model",
		Messages: []message.Message{
			message.NewText(message.RoleUser, "hi"),
		},
		MaxIterations: 1,
		OutputGuardrails: []OutputGuardrail{
			NewOutputGuardrail("block", func(_ context.Context, input OutputGuardrailInput) (OutputGuardrailResult, error) {
				if input.Output.Text == "unsafe answer" {
					return BlockOutput("unsafe output"), nil
				}
				return AllowOutput(), nil
			}),
		},
	})
	var tripwire *OutputGuardrailTripwireTriggeredError
	if !errors.As(err, &tripwire) {
		t.Fatalf("expected OutputGuardrailTripwireTriggeredError, got %v", err)
	}
	if tripwire.Guardrail != "block" {
		t.Fatalf("expected guardrail name in error, got %#v", tripwire)
	}
}

func TestEngineOutputGuardrailRetryExhaustionReturnsError(t *testing.T) {
	driver := &scriptedProvider{
		turns: [][]provider.Event{{
			{Kind: provider.EventTextDelta, Text: "draft answer"},
			{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
		}},
	}
	engine := Engine{Provider: driver}
	_, err := engine.RunMessages(context.Background(), LoopInput{
		Model: "test-model",
		Messages: []message.Message{
			message.NewText(message.RoleUser, "hi"),
		},
		MaxIterations: 1,
		OutputGuardrails: []OutputGuardrail{
			NewOutputGuardrail("retry", func(_ context.Context, input OutputGuardrailInput) (OutputGuardrailResult, error) {
				return RetryOutput(message.NewText(message.RoleUser, "please revise")), nil
			}),
		},
	})
	var retryErr *OutputGuardrailRetryLimitExceededError
	if !errors.As(err, &retryErr) {
		t.Fatalf("expected OutputGuardrailRetryLimitExceededError, got %v", err)
	}
}

func TestEngineOutputGuardrailsOnlyRunOnTerminalOutput(t *testing.T) {
	driver := &scriptedProvider{
		turns: [][]provider.Event{
			{
				{
					Kind: provider.EventToolCall,
					ToolCall: &message.ToolCall{
						ID:        "call-1",
						Name:      "lookup",
						Arguments: []byte(`{"query":"venat"}`),
					},
				},
				{Kind: provider.EventDone, StopReason: provider.StopReasonToolUse},
			},
			{
				{Kind: provider.EventTextDelta, Text: "done"},
				{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
			},
		},
	}
	toolDriver, err := kit.Tool("lookup", func(_ context.Context, input struct {
		Query string `json:"query"`
	},
	) (string, error) {
		return "result:" + input.Query, nil
	})
	if err != nil {
		t.Fatalf("tool setup: %v", err)
	}
	calls := 0
	engine := Engine{
		Provider: driver,
		Tools:    tool.NewBus(toolDriver),
	}
	result, err := engine.RunMessages(context.Background(), LoopInput{
		Model:         "test-model",
		Messages:      []message.Message{message.NewText(message.RoleUser, "find venat")},
		MaxIterations: 3,
		ToolMode:      tool.ModeSequential,
		OutputGuardrails: []OutputGuardrail{
			NewOutputGuardrail("count", func(_ context.Context, input OutputGuardrailInput) (OutputGuardrailResult, error) {
				calls++
				if input.Output.Text != "done" {
					t.Fatalf("guardrail should run only for terminal assistant output, got %#v", input.Output)
				}
				return AllowOutput(), nil
			}),
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected guardrail to run once on terminal output, got %d", calls)
	}
	if result.Messages[len(result.Messages)-1].Text != "done" {
		t.Fatalf("expected final assistant output, got %#v", result.Messages[len(result.Messages)-1])
	}
}

func TestEngineOutputGuardrailReplacementIsSeenByNextGuardrail(t *testing.T) {
	driver := &scriptedProvider{
		turns: [][]provider.Event{{
			{Kind: provider.EventTextDelta, Text: "unsafe draft"},
			{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
		}},
	}
	var secondSaw string
	engine := Engine{Provider: driver}
	result, err := engine.RunMessages(context.Background(), LoopInput{
		Model: "test-model",
		Messages: []message.Message{
			message.NewText(message.RoleUser, "hi"),
		},
		MaxIterations: 1,
		OutputGuardrails: []OutputGuardrail{
			NewOutputGuardrail("replacer", func(_ context.Context, input OutputGuardrailInput) (OutputGuardrailResult, error) {
				return ReplaceOutput(message.NewText(message.RoleAssistant, "safe answer")), nil
			}),
			NewOutputGuardrail("observer", func(_ context.Context, input OutputGuardrailInput) (OutputGuardrailResult, error) {
				secondSaw = input.Output.Text
				return AllowOutput(), nil
			}),
		},
	})
	if err != nil {
		t.Fatalf("RunMessages() error = %v", err)
	}
	if secondSaw != "safe answer" {
		t.Fatalf("second guardrail saw %q, want the replaced output", secondSaw)
	}
	last := result.Messages[len(result.Messages)-1]
	if last.Text != "safe answer" {
		t.Fatalf("final output = %#v, want the replaced output", last)
	}
}
