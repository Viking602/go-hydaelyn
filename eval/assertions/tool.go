package assertions

import (
	"context"
	"fmt"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/eval"
	"github.com/Viking602/venat/eval/matcher"
)

// Compile-time guarantees that every M2 assertion satisfies eval.Assertion.
var (
	_ eval.Assertion = ToolCalled{}
	_ eval.Assertion = ToolNotCalled{}
	_ eval.Assertion = ToolCalledNTimes{}
	_ eval.Assertion = ToolCalledWithArg{}
	_ eval.Assertion = PolicyDecisionAllowedBy{}
	_ eval.Assertion = PolicyDecisionDeniedBy{}
	_ eval.Assertion = WithinBudget{}
	_ eval.Assertion = WithinDuration{}
	_ eval.Assertion = BlackboardHasItem{}
	_ eval.Assertion = ApprovalRequested{}
)

// ToolCalled asserts that a tool with a given name was invoked at least once
// during the run. A tool invocation is observed two ways through the public
// surface: an ActionAttemptStarted event whose payload carries the tool name
// (the durable action-attempt protocol), or a blackboard item written by the
// tool itself (Source.Type == tool). Either counts.
type ToolCalled struct {
	// Tool is the name of the tool that must have been called.
	Tool string
}

// Name returns the assertion's stable identifier.
func (a ToolCalled) Name() string { return "ToolCalled" }

// Check reports whether the named tool was invoked at least once.
func (a ToolCalled) Check(ctx context.Context, run api.Run, harness eval.Harness) error {
	calls, err := toolCalls(ctx, run, harness)
	if err != nil {
		return err
	}
	if countToolCalls(calls, a.Tool) > 0 {
		return nil
	}
	return fmt.Errorf("tool %q was not called", a.Tool)
}

// ToolNotCalled asserts that a tool with a given name was never invoked during
// the run.
type ToolNotCalled struct {
	// Tool is the name of the tool that must not have been called.
	Tool string
}

// Name returns the assertion's stable identifier.
func (a ToolNotCalled) Name() string { return "ToolNotCalled" }

// Check reports whether the named tool was never invoked.
func (a ToolNotCalled) Check(ctx context.Context, run api.Run, harness eval.Harness) error {
	calls, err := toolCalls(ctx, run, harness)
	if err != nil {
		return err
	}
	if n := countToolCalls(calls, a.Tool); n > 0 {
		return fmt.Errorf("tool %q was called %d time(s), want 0", a.Tool, n)
	}
	return nil
}

// ToolCalledNTimes asserts that a tool with a given name was invoked exactly N
// times during the run.
type ToolCalledNTimes struct {
	// Tool is the name of the tool whose invocations are counted.
	Tool string
	// Times is the exact number of invocations expected.
	Times int
}

// Name returns the assertion's stable identifier.
func (a ToolCalledNTimes) Name() string { return "ToolCalledNTimes" }

// Check reports whether the named tool was invoked exactly Times.
func (a ToolCalledNTimes) Check(ctx context.Context, run api.Run, harness eval.Harness) error {
	calls, err := toolCalls(ctx, run, harness)
	if err != nil {
		return err
	}
	if n := countToolCalls(calls, a.Tool); n != a.Times {
		return fmt.Errorf("tool %q was called %d time(s), want %d", a.Tool, n, a.Times)
	}
	return nil
}

// ToolCalledWithArg asserts that a tool with a given name was invoked at least
// once with an argument satisfying a matcher. The argument is read from the
// tool's blackboard contribution (its Payload, falling back to Content), so the
// tool must surface its input/output on the blackboard for this assertion to
// observe it. The matcher folds that argument into a pass/fail verdict —
// matcher.JSONContains and matcher.JSONMatchSchema are the typical choices.
type ToolCalledWithArg struct {
	// Tool is the name of the tool whose argument is matched.
	Tool string
	// Matcher grades the observed tool argument.
	Matcher matcher.Matcher
}

// Name returns the assertion's stable identifier.
func (a ToolCalledWithArg) Name() string { return "ToolCalledWithArg" }

// Check reports whether the named tool was invoked with a matching argument.
func (a ToolCalledWithArg) Check(ctx context.Context, run api.Run, harness eval.Harness) error {
	if a.Matcher == nil {
		return fmt.Errorf("ToolCalledWithArg requires a matcher")
	}
	calls, err := toolCalls(ctx, run, harness)
	if err != nil {
		return err
	}
	var seen bool
	var lastDetail string
	for _, call := range calls {
		if call.tool != a.Tool {
			continue
		}
		seen = true
		if !call.hasArg {
			continue
		}
		ok, detail := a.Matcher.Match(call.arg)
		if ok {
			return nil
		}
		lastDetail = detail
	}
	if !seen {
		return fmt.Errorf("tool %q was not called", a.Tool)
	}
	if lastDetail == "" {
		lastDetail = "no observed call carried an argument on the blackboard"
	}
	return fmt.Errorf("tool %q was called but no argument matched: %s", a.Tool, lastDetail)
}

// callSource records which public-surface signal an observed tool invocation
// came from, so counting can avoid double-counting a single call that surfaces
// on both signals.
type callSource int

const (
	// sourceEvent is an ActionAttemptStarted event (the durable action-attempt
	// protocol): the canonical "a tool was invoked" signal, carrying no argument.
	sourceEvent callSource = iota
	// sourceBlackboard is a tool-sourced blackboard item: the tool's output
	// contribution, carrying an argument but not guaranteed one-per-invocation.
	sourceBlackboard
)

// toolCall is one observed tool invocation during a run. arg is set only when
// the observation carries an argument (blackboard-sourced observations do).
type toolCall struct {
	tool   string
	arg    any
	hasArg bool
	source callSource
}

// toolCalls gathers every observed tool invocation for the run from the public
// surface: ActionAttemptStarted events (name only) and tool-sourced blackboard
// items (name plus argument). The two signals are tagged with their callSource
// so countToolCalls can dedupe a call that surfaces on both.
func toolCalls(ctx context.Context, run api.Run, harness eval.Harness) ([]toolCall, error) {
	runner := harness.Runner()
	if runner == nil {
		return nil, fmt.Errorf("harness returned a nil runner")
	}
	var calls []toolCall

	events, err := runner.RunEvents(ctx, run.ID)
	if err != nil {
		return nil, fmt.Errorf("read run events: %w", err)
	}
	for _, ev := range events {
		if ev.Type != api.EventActionAttemptStarted {
			continue
		}
		name, _ := ev.Payload["toolName"].(string)
		if name == "" {
			continue
		}
		calls = append(calls, toolCall{tool: name, source: sourceEvent})
	}

	items, err := runner.SelectItems(ctx, run.ID, api.BlackboardSelector{RunID: run.ID, SourceTypes: []api.SourceType{api.SourceTool}})
	if err != nil {
		return nil, fmt.Errorf("select blackboard items: %w", err)
	}
	for _, item := range items {
		name := item.Source.ID
		if name == "" {
			continue
		}
		arg := item.Payload
		if arg == "" {
			arg = item.Content
		}
		calls = append(calls, toolCall{tool: name, arg: arg, hasArg: true, source: sourceBlackboard})
	}
	return calls, nil
}

// countToolCalls counts invocations of tool without double-counting a call that
// surfaced on both signals. ActionAttemptStarted events are the canonical
// invocation count, so when the tool has any event observations only those are
// counted; a tool observed solely on the blackboard (e.g. a scripted run that
// emits no action-attempt events) falls back to its blackboard-item count.
func countToolCalls(calls []toolCall, tool string) int {
	events, items := 0, 0
	for _, call := range calls {
		if call.tool != tool {
			continue
		}
		switch call.source {
		case sourceEvent:
			events++
		default:
			items++
		}
	}
	if events > 0 {
		return events
	}
	return items
}
