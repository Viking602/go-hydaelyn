package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/tool"
	"github.com/Viking602/go-hydaelyn/tool/kit"
)

// requireNoStepTimestamps asserts that the loop left every Step's wall-clock
// fields zero. A replayed loop must reproduce Steps byte-for-byte (ADR-007),
// so the loop is forbidden from stamping time.Now() onto them.
func requireNoStepTimestamps(t *testing.T, steps []Step) {
	t.Helper()
	for _, step := range steps {
		if !step.StartedAt.IsZero() || !step.EndedAt.IsZero() {
			t.Fatalf("step %d carries wall-clock timestamps (StartedAt=%v EndedAt=%v); Steps must stay replay-deterministic",
				step.Index, step.StartedAt, step.EndedAt)
		}
	}
}

func TestRunMessagesEmitsStepPerIterationToolThenFinish(t *testing.T) {
	driver, err := kit.Tool("lookup", func(_ context.Context, input struct {
		Query string `json:"query"`
	}) (string, error) {
		return "result:" + input.Query, nil
	})
	if err != nil {
		t.Fatalf("tool setup: %v", err)
	}
	engine := Engine{Provider: fakeProvider{}, Tools: tool.NewBus(driver)}

	output, err := engine.RunMessages(context.Background(), LoopInput{
		Model:         "test-model",
		Messages:      []message.Message{message.NewText(message.RoleUser, "find hydaelyn")},
		MaxIterations: 3,
		ToolMode:      tool.ModeSequential,
	})
	if err != nil {
		t.Fatalf("RunMessages() error = %v", err)
	}

	// fakeProvider emits one tool call (turn 1) then prose (turn 2): two
	// iterations, so two steps.
	if output.Iterations != 2 {
		t.Fatalf("Iterations = %d, want 2", output.Iterations)
	}
	if len(output.Steps) != output.Iterations {
		t.Fatalf("len(Steps) = %d, want one per iteration (%d)", len(output.Steps), output.Iterations)
	}
	if output.ToolCallsUsed != 1 {
		t.Fatalf("ToolCallsUsed = %d, want 1", output.ToolCallsUsed)
	}

	tool0 := output.Steps[0]
	if tool0.Index != 0 || tool0.Decision != StepDecisionContinue {
		t.Fatalf("step 0 = {Index:%d Decision:%s}, want {0 continue}", tool0.Index, tool0.Decision)
	}
	if tool0.ModelCall == nil || tool0.ModelCall.Model != "test-model" {
		t.Fatalf("step 0 ModelCall = %#v, want model test-model", tool0.ModelCall)
	}
	if len(tool0.ToolCalls) != 1 || tool0.ToolCalls[0].Name != "lookup" {
		t.Fatalf("step 0 ToolCalls = %#v, want one lookup trace", tool0.ToolCalls)
	}
	if len(tool0.ToolCalls[0].Output) == 0 {
		t.Fatalf("step 0 tool trace output is empty, want the tool result")
	}
	if tool0.BudgetUsed.ToolCalls != 1 {
		t.Fatalf("step 0 BudgetUsed.ToolCalls = %d, want 1", tool0.BudgetUsed.ToolCalls)
	}

	finish := output.Steps[1]
	if finish.Index != 1 || finish.Decision != StepDecisionFinish {
		t.Fatalf("step 1 = {Index:%d Decision:%s}, want {1 finish}", finish.Index, finish.Decision)
	}
	if len(finish.ToolCalls) != 0 {
		t.Fatalf("step 1 ToolCalls = %#v, want none on the finishing turn", finish.ToolCalls)
	}

	requireNoStepTimestamps(t, output.Steps)
}

func TestRunMessagesEmitsStepPerIterationAtMaxTurns(t *testing.T) {
	driver, err := kit.Tool("lookup", func(_ context.Context, _ struct {
		Query string `json:"query"`
	}) (string, error) {
		return "result", nil
	})
	if err != nil {
		t.Fatalf("tool setup: %v", err)
	}
	engine := Engine{Provider: &alwaysToolProvider{}, Tools: tool.NewBus(driver)}

	output, err := engine.RunMessages(context.Background(), LoopInput{
		Model:    "test-model",
		Messages: []message.Message{message.NewText(message.RoleUser, "loop forever")},
		// MaxIterations unset -> default ceiling of 12.
	})
	if err != nil {
		t.Fatalf("RunMessages() error = %v", err)
	}

	if len(output.Steps) != output.Iterations || output.Iterations != 12 {
		t.Fatalf("len(Steps)=%d Iterations=%d, want both 12", len(output.Steps), output.Iterations)
	}
	if output.ToolCallsUsed != 12 {
		t.Fatalf("ToolCallsUsed = %d, want 12", output.ToolCallsUsed)
	}
	for i, step := range output.Steps {
		if step.Index != i {
			t.Fatalf("step %d has non-contiguous Index %d", i, step.Index)
		}
		if step.Decision != StepDecisionContinue {
			t.Fatalf("step %d Decision = %s, want continue (never reached a terminal turn)", i, step.Decision)
		}
		if step.BudgetUsed.ToolCalls != i+1 {
			t.Fatalf("step %d BudgetUsed.ToolCalls = %d, want cumulative %d", i, step.BudgetUsed.ToolCalls, i+1)
		}
	}
	requireNoStepTimestamps(t, output.Steps)
}

func TestRunMessagesStepDecisionFinishOnTerminalTool(t *testing.T) {
	driver := &scriptedProvider{
		turns: [][]provider.Event{{
			{
				Kind: provider.EventToolCall,
				ToolCall: &message.ToolCall{
					ID:        "call-1",
					Name:      "submit_report",
					Arguments: json.RawMessage(`{"answer":"done"}`),
				},
			},
			{Kind: provider.EventDone, StopReason: provider.StopReasonToolUse},
		}},
	}
	engine := Engine{Provider: driver, Tools: tool.NewBus(terminalTool{})}

	output, err := engine.RunMessages(context.Background(), LoopInput{
		Model:         "test-model",
		Messages:      []message.Message{message.NewText(message.RoleUser, "finish")},
		MaxIterations: 3,
	})
	if err != nil {
		t.Fatalf("RunMessages() error = %v", err)
	}

	if len(output.Steps) != 1 {
		t.Fatalf("len(Steps) = %d, want 1 (terminal tool stops after first turn)", len(output.Steps))
	}
	step := output.Steps[0]
	if step.Decision != StepDecisionFinish {
		t.Fatalf("terminal-tool step Decision = %s, want finish", step.Decision)
	}
	if len(step.ToolCalls) != 1 || step.ToolCalls[0].Name != "submit_report" {
		t.Fatalf("terminal-tool step ToolCalls = %#v, want one submit_report trace", step.ToolCalls)
	}
	if string(step.ToolCalls[0].Output) != `{"answer":"done"}` {
		t.Fatalf("terminal-tool trace Output = %s, want the submitted arguments", step.ToolCalls[0].Output)
	}
	if output.ToolCallsUsed != 1 {
		t.Fatalf("ToolCallsUsed = %d, want 1", output.ToolCallsUsed)
	}
	requireNoStepTimestamps(t, output.Steps)
}

func TestEngineRepairConcatenatesStepsWithGlobalReindex(t *testing.T) {
	// Initial run returns enum-invalid output; the single repair run fixes it.
	// Each run is one finishing turn, so the merged trace must be two steps
	// with globally continuous indices 0 and 1 — not two index-0 collisions.
	_, engine := newOutputPolicyEngine(
		`{"status":"blocked","score":0.75,"count":2,"tags":["risk"],"accepted":true}`,
		`{"status":"ok","score":0.75,"count":2,"tags":["risk"],"accepted":true}`,
	)
	policy := outputPolicyReport()
	policy.Repair = true
	policy.MaxRepairAttempts = 1

	result := engine.Run(context.Background(), api.Task{Goal: "classify risk"}, policy)

	if !result.Valid {
		t.Fatalf("result.Valid = false, failure = %#v", result.Failure)
	}
	if result.RepairCount != 1 {
		t.Fatalf("RepairCount = %d, want 1", result.RepairCount)
	}
	if len(result.Steps) != 2 {
		t.Fatalf("len(Steps) = %d, want 2 (initial + repair turn)", len(result.Steps))
	}
	for i, step := range result.Steps {
		if step.Index != i {
			t.Fatalf("step %d has Index %d; repair steps must be globally reindexed", i, step.Index)
		}
		if step.Decision != StepDecisionFinish {
			t.Fatalf("step %d Decision = %s, want finish", i, step.Decision)
		}
	}
	requireNoStepTimestamps(t, result.Steps)
}
