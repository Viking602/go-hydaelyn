package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/message"
	"github.com/Viking602/venat/provider"
	"github.com/Viking602/venat/tool"
)

func TestAsTool_DefinitionAdvertisesIdentityAndSchema(t *testing.T) {
	child := Engine{Provider: singleTurnProvider("ok"), Model: "child"}
	schema := tool.Schema{Type: "object", Properties: map[string]tool.Schema{"input": {Type: "string"}}}
	driver := AsTool(child, SubagentDef{Name: "researcher", Description: "delegates research", InputSchema: schema})
	schema.Properties["input"] = tool.Schema{Type: "integer"}

	def := driver.Definition()
	if def.Name != "researcher" {
		t.Fatalf("Definition().Name = %q, want researcher", def.Name)
	}
	if def.Description != "delegates research" {
		t.Fatalf("Definition().Description = %q", def.Description)
	}
	if def.InputSchema.Type != "object" {
		t.Fatalf("Definition().InputSchema.Type = %q, want object", def.InputSchema.Type)
	}
	if def.InputSchema.Properties["input"].Type != "string" {
		t.Fatalf("AsTool retained caller-owned schema: %#v", def.InputSchema)
	}
	def.InputSchema.Properties["input"] = tool.Schema{Type: "boolean"}
	if current := driver.Definition().InputSchema.Properties["input"].Type; current != "string" {
		t.Fatalf("Definition returned mutable schema ownership: %q", current)
	}
	// A tool-less, pure-reasoning child aggregates to read-only — the genuinely
	// safe case. Children that can call side-effecting tools are covered below.
	if def.EffectType != tool.EffectReadOnly {
		t.Fatalf("Definition().EffectType = %q, want read_only", def.EffectType)
	}
}

func TestAsToolRoutesThroughDurableSchedulerWithStableIdentity(t *testing.T) {
	cache := make(map[string]SubagentExecution)
	var requests []SubagentRequest
	childRuns := 0
	updates := 0
	scheduler := SubagentSchedulerFunc(func(
		ctx context.Context,
		request SubagentRequest,
		sink tool.UpdateSink,
	) (SubagentExecution, error) {
		requests = append(requests, request)
		if SubagentDepth(ctx) != request.Depth || request.Depth != 1 {
			t.Fatalf("scheduler depth context=%d request=%d", SubagentDepth(ctx), request.Depth)
		}
		if sink != nil {
			if err := sink(tool.Update{Kind: "scheduled"}); err != nil {
				return SubagentExecution{}, err
			}
		}
		if prior, ok := cache[request.ID]; ok {
			return prior, nil
		}
		childRuns++
		execution := SubagentExecution{
			Result:               Result{Text: "durable child answer", Usage: provider.Usage{OutputTokens: 7, TotalTokens: 7}},
			ParentUsageAccounted: true,
		}
		cache[request.ID] = execution
		return execution, nil
	})
	child := Engine{Provider: singleTurnProvider("must not run"), Model: "child", SubagentScheduler: scheduler}
	driver := AsTool(child, SubagentDef{Name: "researcher"})
	parent := tool.CallerInfo{SessionID: "session-1", TaskID: "task-1", AgentID: "main"}
	ctx := tool.WithCaller(context.Background(), parent)
	ctx = withParentUsageSink(ctx, func(usage, externallyAccounted provider.Usage) {
		if usage.TotalTokens != 7 || externallyAccounted.TotalTokens != 7 {
			t.Fatalf("reported child usage = %#v external=%#v", usage, externallyAccounted)
		}
	})
	call := tool.Call{ID: "call-1", OperationID: "turn-1:0", Name: "researcher", Arguments: json.RawMessage(`{"input":"inspect"}`)}
	sink := func(tool.Update) error {
		updates++
		return nil
	}
	first, err := driver.Execute(ctx, call, sink)
	if err != nil || first.IsError || first.Content != "durable child answer" {
		t.Fatalf("first scheduled result = %#v, %v", first, err)
	}
	second, err := driver.Execute(ctx, call, sink)
	if err != nil || second.IsError || second.Content != first.Content {
		t.Fatalf("replayed scheduled result = %#v, %v", second, err)
	}
	if len(requests) != 2 || requests[0].ID == "" || requests[0].ID != requests[1].ID {
		t.Fatalf("stable scheduler requests = %#v", requests)
	}
	if requests[0].Parent != parent || requests[0].Task.Goal != "inspect" || requests[0].Call.OperationID != "turn-1:0" {
		t.Fatalf("scheduler request lost parent/task/call identity: %#v", requests[0])
	}
	if childRuns != 1 || updates != 2 {
		t.Fatalf("durable child runs=%d updates=%d, want 1 and 2", childRuns, updates)
	}
	if same := ComputeSubagentID(parent, tool.Call{ID: "call-2", Name: "researcher"}); same == requests[0].ID {
		t.Fatal("different parent tool-call slots produced the same subagent ID")
	}
	if sameSlot := ComputeSubagentID(parent, tool.Call{
		ID: "provider-retry-id", OperationID: "turn-1:0", Name: "researcher",
	}); sameSlot != requests[0].ID {
		t.Fatalf("stable operation slot changed subagent id: %q != %q", sameSlot, requests[0].ID)
	}
}

func TestAsToolUnknownSchedulerFailureRequiresDurableReconciliation(t *testing.T) {
	child := Engine{
		SubagentScheduler: SubagentSchedulerFunc(func(context.Context, SubagentRequest, tool.UpdateSink) (SubagentExecution, error) {
			return SubagentExecution{}, errors.New("reply lost after child start")
		}),
	}
	result, err := AsTool(child, SubagentDef{Name: "researcher"}).Execute(
		tool.WithCaller(context.Background(), tool.CallerInfo{SessionID: "session"}),
		tool.Call{ID: "call", Name: "researcher", Arguments: json.RawMessage(`{}`)},
		nil,
	)
	if !errors.Is(err, ErrSubagentOutcomeUnknown) || result.Name != "" {
		t.Fatalf("unknown scheduler outcome result=%#v err=%v", result, err)
	}
}

func TestAsToolNotStartedSchedulerFailureIsRecoverableToolResult(t *testing.T) {
	child := Engine{
		SubagentScheduler: SubagentSchedulerFunc(func(context.Context, SubagentRequest, tool.UpdateSink) (SubagentExecution, error) {
			return SubagentExecution{}, fmt.Errorf("%w: admission unavailable", ErrSubagentNotStarted)
		}),
	}
	result, err := AsTool(child, SubagentDef{Name: "researcher"}).Execute(
		tool.WithCaller(context.Background(), tool.CallerInfo{SessionID: "session"}),
		tool.Call{ID: "call", Name: "researcher", Arguments: json.RawMessage(`{}`)},
		nil,
	)
	if err != nil || !result.IsError || !strings.Contains(result.Content, "admission unavailable") {
		t.Fatalf("not-started scheduler result=%#v err=%v", result, err)
	}
}

func TestAsToolDurableSchedulerRequiresParentNamespace(t *testing.T) {
	invoked := false
	child := Engine{
		SubagentScheduler: SubagentSchedulerFunc(func(context.Context, SubagentRequest, tool.UpdateSink) (SubagentExecution, error) {
			invoked = true
			return SubagentExecution{Result: Result{Text: "unexpected"}}, nil
		}),
	}
	result, err := AsTool(child, SubagentDef{Name: "researcher"}).Execute(
		context.Background(),
		tool.Call{ID: "call", Name: "researcher", Arguments: json.RawMessage(`{}`)},
		nil,
	)
	if err != nil || invoked || !result.IsError || !strings.Contains(result.Content, "durable parent identity is missing") {
		t.Fatalf("missing parent identity result=%#v invoked=%v err=%v", result, invoked, err)
	}
}

func TestAsTool_AggregatesChildWriteEffect(t *testing.T) {
	child := Engine{
		Provider: singleTurnProvider("ok"),
		Model:    "child",
		Tools: tool.NewBus(staticTool{def: tool.Definition{
			Name: "writer", InputSchema: tool.Schema{Type: "object"}, EffectType: tool.EffectWrite,
		}}),
	}
	def := AsTool(child, SubagentDef{Name: "sub"}).Definition()
	if def.EffectType != tool.EffectWrite {
		t.Fatalf("EffectType = %q, want write (a child that can write must not advertise read_only)", def.EffectType)
	}
}

func TestAsTool_AggregatesChildExternalEffect(t *testing.T) {
	child := Engine{
		Provider: singleTurnProvider("ok"),
		Model:    "child",
		Tools: tool.NewBus(staticTool{def: tool.Definition{
			Name: "caller", InputSchema: tool.Schema{Type: "object"}, EffectType: tool.EffectExternalSideEffect,
		}}),
	}
	def := AsTool(child, SubagentDef{Name: "sub"}).Definition()
	if def.EffectType != tool.EffectExternalSideEffect {
		t.Fatalf("EffectType = %q, want external_side_effect", def.EffectType)
	}
}

func TestAsTool_TakesMaxEffectAcrossChildTools(t *testing.T) {
	child := Engine{
		Provider: singleTurnProvider("ok"),
		Model:    "child",
		Tools: tool.NewBus(
			staticTool{def: tool.Definition{Name: "reader", InputSchema: tool.Schema{Type: "object"}, EffectType: tool.EffectReadOnly}},
			staticTool{def: tool.Definition{Name: "writer", InputSchema: tool.Schema{Type: "object"}, EffectType: tool.EffectWrite}},
		),
	}
	def := AsTool(child, SubagentDef{Name: "sub"}).Definition()
	if def.EffectType != tool.EffectWrite {
		t.Fatalf("EffectType = %q, want write (the max across a read-only and a write tool)", def.EffectType)
	}
}

// TestAsTool_AggregatesApprovalAndRisk pins the core Codex finding: an
// approval-gated child tool that declares no explicit effect must surface as an
// approval-required external side effect on the wrapper, mirroring how the
// worker derives a runner tool. Risk level and policy tags ride along.
func TestAsTool_AggregatesApprovalAndRisk(t *testing.T) {
	child := Engine{
		Provider: singleTurnProvider("ok"),
		Model:    "child",
		Tools: tool.NewBus(staticTool{def: tool.Definition{
			Name:             "danger",
			InputSchema:      tool.Schema{Type: "object"},
			RequiresApproval: true, // no explicit effect — must normalize to external
			RiskLevel:        "high",
			PolicyTags:       []string{"pii"},
		}}),
	}
	def := AsTool(child, SubagentDef{Name: "sub"}).Definition()
	if !def.RequiresApproval {
		t.Fatal("RequiresApproval = false, want true (a child approval tool must surface on the wrapper)")
	}
	if def.EffectType != tool.EffectExternalSideEffect {
		t.Fatalf("EffectType = %q, want external_side_effect (approval with no effect mirrors the worker)", def.EffectType)
	}
	if def.RiskLevel != "high" {
		t.Fatalf("RiskLevel = %q, want high", def.RiskLevel)
	}
	if !reflect.DeepEqual(def.PolicyTags, []string{"pii"}) {
		t.Fatalf("PolicyTags = %v, want [pii]", def.PolicyTags)
	}
}

// TestAsTool_AbsorbsSecuritySubfield pins that approval/risk declared on the
// nested Security struct (not the flat fields) is also aggregated.
func TestAsTool_AbsorbsSecuritySubfield(t *testing.T) {
	child := Engine{
		Provider: singleTurnProvider("ok"),
		Model:    "child",
		Tools: tool.NewBus(staticTool{def: tool.Definition{
			Name:        "danger",
			InputSchema: tool.Schema{Type: "object"},
			Security:    message.ToolSecurity{RequiresApproval: true, RiskLevel: "critical"},
		}}),
	}
	def := AsTool(child, SubagentDef{Name: "sub"}).Definition()
	if !def.RequiresApproval {
		t.Fatal("RequiresApproval = false, want true (Security.RequiresApproval must surface)")
	}
	if def.EffectType != tool.EffectExternalSideEffect {
		t.Fatalf("EffectType = %q, want external_side_effect", def.EffectType)
	}
	if def.RiskLevel != "critical" {
		t.Fatalf("RiskLevel = %q, want critical (from Security.RiskLevel)", def.RiskLevel)
	}
}

// TestAsTool_PolicyTagsAreDedupedAndSorted pins replay determinism: Bus
// iteration order is map-based, so aggregated tags must be deduped and sorted.
func TestAsTool_PolicyTagsAreDedupedAndSorted(t *testing.T) {
	child := Engine{
		Provider: singleTurnProvider("ok"),
		Model:    "child",
		Tools: tool.NewBus(
			staticTool{def: tool.Definition{Name: "a", InputSchema: tool.Schema{Type: "object"}, PolicyTags: []string{"net", "pii"}}},
			staticTool{def: tool.Definition{Name: "b", InputSchema: tool.Schema{Type: "object"}, PolicyTags: []string{"pii", "fs"}}},
		),
	}
	def := AsTool(child, SubagentDef{Name: "sub"}).Definition()
	want := []string{"fs", "net", "pii"}
	if !reflect.DeepEqual(def.PolicyTags, want) {
		t.Fatalf("PolicyTags = %v, want %v (deduped and sorted for replay determinism)", def.PolicyTags, want)
	}
}

// TestAsTool_EffectFloorRaisesToollessChild pins that SubagentDef.Effect raises
// the floor when a child's tools are not statically visible.
func TestAsTool_EffectFloorRaisesToollessChild(t *testing.T) {
	child := Engine{Provider: singleTurnProvider("ok"), Model: "child"} // no tools
	def := AsTool(child, SubagentDef{Name: "sub", Effect: tool.EffectWrite}).Definition()
	if def.EffectType != tool.EffectWrite {
		t.Fatalf("EffectType = %q, want write (the declared floor on a tool-less child)", def.EffectType)
	}
}

// TestAsTool_EffectFloorNeverLowersChildRisk pins that the floor can only raise
// risk: a lower declared floor never masks a more dangerous child tool.
func TestAsTool_EffectFloorNeverLowersChildRisk(t *testing.T) {
	child := Engine{
		Provider: singleTurnProvider("ok"),
		Model:    "child",
		Tools: tool.NewBus(staticTool{def: tool.Definition{
			Name: "caller", InputSchema: tool.Schema{Type: "object"}, EffectType: tool.EffectExternalSideEffect,
		}}),
	}
	def := AsTool(child, SubagentDef{Name: "sub", Effect: tool.EffectWrite}).Definition()
	if def.EffectType != tool.EffectExternalSideEffect {
		t.Fatalf("EffectType = %q, want external_side_effect (a lower floor must not mask a more dangerous child)", def.EffectType)
	}
}

func TestAsTool_FoldsChildUsageIntoParentLoop(t *testing.T) {
	child := Engine{
		Provider: &scriptedProvider{turns: [][]provider.Event{{
			{Kind: provider.EventTextDelta, Text: "child"},
			{Kind: provider.EventDone, StopReason: provider.StopReasonComplete, Usage: provider.Usage{InputTokens: 4, OutputTokens: 2, TotalTokens: 6}},
		}}},
		Model: "child",
	}
	parent := Engine{
		Provider: &scriptedProvider{turns: [][]provider.Event{
			{
				{Kind: provider.EventToolCall, ToolCall: &message.ToolCall{ID: "call-1", Name: "researcher", Arguments: json.RawMessage(`{"input":"q"}`)}},
				{Kind: provider.EventDone, StopReason: provider.StopReasonToolUse, Usage: provider.Usage{InputTokens: 1, OutputTokens: 1, TotalTokens: 2}},
			},
			{
				{Kind: provider.EventTextDelta, Text: "done"},
				{Kind: provider.EventDone, StopReason: provider.StopReasonComplete, Usage: provider.Usage{InputTokens: 1, OutputTokens: 1, TotalTokens: 2}},
			},
		}},
		Model: "parent",
		Tools: tool.NewBus(AsTool(child, SubagentDef{Name: "researcher"})),
	}
	output, err := parent.RunMessages(context.Background(), LoopInput{
		Model:     "parent",
		Messages:  []message.Message{message.NewText(message.RoleUser, "go")},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("parent RunMessages error = %v", err)
	}
	if output.Usage.TotalTokens < 10 {
		t.Fatalf("parent usage = %#v, want child tokens folded in", output.Usage)
	}
}

func TestAsToolParallelChildrenShareParentTokenReservation(t *testing.T) {
	scheduler := SubagentSchedulerFunc(func(
		_ context.Context,
		request SubagentRequest,
		_ tool.UpdateSink,
	) (SubagentExecution, error) {
		return SubagentExecution{Result: Result{
			Text:  "done",
			Usage: provider.Usage{InputTokens: int(request.Task.Budget.MaxTokens)},
		}}, nil
	})
	driver := AsTool(Engine{SubagentScheduler: scheduler}, SubagentDef{Name: "child"})
	ctx := tool.WithCaller(context.Background(), tool.CallerInfo{SessionID: "session"})
	ctx = withParentTokenBudget(ctx, 10)
	var group sync.WaitGroup
	errs := make(chan error, 2)
	for _, id := range []string{"one", "two"} {
		group.Add(1)
		go func(id string) {
			defer group.Done()
			_, err := driver.Execute(ctx, tool.Call{ID: id, Name: "child", Arguments: json.RawMessage(`{}`)}, nil)
			errs <- err
		}(id)
	}
	group.Wait()
	close(errs)
	successes, exhausted := 0, 0
	for err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrBudgetExhausted):
			exhausted++
		default:
			t.Fatalf("parallel child error = %v", err)
		}
	}
	if successes != 1 || exhausted != 1 {
		t.Fatalf("parallel child outcomes success=%d exhausted=%d", successes, exhausted)
	}
}

func TestAsTool_SuccessMapsTextAndCarriesIdentifiers(t *testing.T) {
	child := Engine{Provider: singleTurnProvider("child answer"), Model: "child"}
	driver := AsTool(child, SubagentDef{Name: "researcher", Description: "d"})

	call := tool.Call{ID: "call-1", Name: "researcher", Arguments: json.RawMessage(`{"input":"do work"}`)}
	result, err := driver.Execute(context.Background(), call, nil)
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v (a subagent must never hard-abort the parent)", err)
	}
	if result.IsError {
		t.Fatalf("Execute marked success as error: %+v", result)
	}
	if result.Content != "child answer" {
		t.Fatalf("Content = %q, want child answer", result.Content)
	}
	if result.ToolCallID != "call-1" || result.Name != "researcher" {
		t.Fatalf("result identifiers = (%q,%q), want (call-1,researcher)", result.ToolCallID, result.Name)
	}
}

// TestAsTool_TerminalToolResultSurfacesAsContent pins the fix for a child that
// submits its final answer through a terminal tool: the child completes with no
// trailing assistant text (result.Text == "") and, under the empty OutputPolicy
// the subagent runs it with, no structured output — so without the fallback the
// wrapper would return a blank, useless result. It must instead surface the
// terminal tool's content and structured payload.
func TestAsTool_TerminalToolResultSurfacesAsContent(t *testing.T) {
	finish := terminalAnswerTool{
		name:       "finish",
		content:    "the child's final answer",
		structured: json.RawMessage(`{"answer":42}`),
	}
	driver := &scriptedProvider{turns: [][]provider.Event{
		{
			{Kind: provider.EventToolCall, ToolCall: &message.ToolCall{ID: "f1", Name: "finish", Arguments: json.RawMessage(`{}`)}},
			{Kind: provider.EventDone, StopReason: provider.StopReasonToolUse},
		},
	}}
	child := Engine{Provider: driver, Tools: tool.NewBus(finish), Model: "child"}
	sub := AsTool(child, SubagentDef{Name: "sub"})

	call := tool.Call{ID: "c", Name: "sub", Arguments: json.RawMessage(`{"input":"go"}`)}
	result, err := sub.Execute(context.Background(), call, nil)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.IsError {
		t.Fatalf("terminal-tool child surfaced as error: %+v", result)
	}
	if result.Content != "the child's final answer" {
		t.Fatalf("Content = %q, want the terminal tool's content (not blank)", result.Content)
	}
	if string(result.Structured) != `{"answer":42}` {
		t.Fatalf("Structured = %s, want the terminal tool's payload", result.Structured)
	}
}

// TestAsTool_TerminalToolErrorStatusPropagates pins that a child which ends by
// calling a terminal tool that rejects its input (an IsError tool result) is
// surfaced as an error delegation, not a completed one: RunMessages still
// finishes through the terminal path with no Failure, so the wrapper must carry
// the terminal result's IsError status itself.
func TestAsTool_TerminalToolErrorStatusPropagates(t *testing.T) {
	reject := terminalAnswerTool{
		name:    "finish",
		content: "submission rejected: missing required field",
		isError: true,
	}
	driver := &scriptedProvider{turns: [][]provider.Event{
		{
			{Kind: provider.EventToolCall, ToolCall: &message.ToolCall{ID: "f1", Name: "finish", Arguments: json.RawMessage(`{}`)}},
			{Kind: provider.EventDone, StopReason: provider.StopReasonToolUse},
		},
	}}
	child := Engine{Provider: driver, Tools: tool.NewBus(reject), Model: "child"}
	sub := AsTool(child, SubagentDef{Name: "sub"})

	call := tool.Call{ID: "c", Name: "sub", Arguments: json.RawMessage(`{"input":"go"}`)}
	result, err := sub.Execute(context.Background(), call, nil)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("a failed terminal submission must surface as an error result, got success: %+v", result)
	}
	if result.Content != "submission rejected: missing required field" {
		t.Fatalf("Content = %q, want the terminal error content", result.Content)
	}
}

// TestAsTool_AssistantTextWinsOverTerminalFallback pins that the fallback only
// fires when there is no assistant text: a child that ends with assistant text
// surfaces that text, not a trailing tool result.
func TestAsTool_AssistantTextWinsOverTerminalFallback(t *testing.T) {
	child := Engine{
		Provider: singleTurnProvider("assistant final answer"),
		Model:    "child",
		Tools:    tool.NewBus(staticTool{def: tool.Definition{Name: "noop", InputSchema: tool.Schema{Type: "object"}}}),
	}
	sub := AsTool(child, SubagentDef{Name: "sub"})

	call := tool.Call{ID: "c", Name: "sub", Arguments: json.RawMessage(`{"input":"go"}`)}
	result, err := sub.Execute(context.Background(), call, nil)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.Content != "assistant final answer" {
		t.Fatalf("Content = %q, want the assistant text", result.Content)
	}
}

// TestAsTool_TruncatedRunDoesNotReportNonTerminalToolAsAnswer pins that a child
// which stops on its iteration budget (StopReasonMaxTurns) after only a
// non-terminal tool call — leaving no assistant text — is not reported as a
// completed delegation carrying that mid-run tool output. The non-terminal
// lookup result is not a final answer, and the truncation surfaces as an error.
func TestAsTool_TruncatedRunDoesNotReportNonTerminalToolAsAnswer(t *testing.T) {
	lookup := staticTool{def: tool.Definition{Name: "lookup", InputSchema: tool.Schema{Type: "object"}}}
	driver := &scriptedProvider{turns: [][]provider.Event{
		{
			{Kind: provider.EventToolCall, ToolCall: &message.ToolCall{ID: "l1", Name: "lookup", Arguments: json.RawMessage(`{}`)}},
			{Kind: provider.EventDone, StopReason: provider.StopReasonToolUse},
		},
	}}
	child := Engine{
		Provider:   driver,
		Tools:      tool.NewBus(lookup),
		Model:      "child",
		LoopPolicy: LoopPolicy{MaxIterations: 1}, // one turn: calls lookup, then runs out
	}
	sub := AsTool(child, SubagentDef{Name: "sub"})

	call := tool.Call{ID: "c", Name: "sub", Arguments: json.RawMessage(`{"input":"go"}`)}
	result, err := sub.Execute(context.Background(), call, nil)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.Content == "ok" {
		t.Fatal("subagent promoted the non-terminal lookup output to the final answer")
	}
	if !result.IsError {
		t.Fatalf("a truncated, non-converged child run must surface as an error result, got success: %+v", result)
	}
	if !strings.Contains(result.Content, "without a final answer") {
		t.Fatalf("Content = %q, want a truncation explanation", result.Content)
	}
}

func TestAsTool_GoalFromInputField(t *testing.T) {
	var captured string
	child := Engine{
		Provider:       singleTurnProvider("ok"),
		Model:          "child",
		ContextBuilder: goalRecorder{goal: &captured},
	}
	driver := AsTool(child, SubagentDef{Name: "sub"})

	call := tool.Call{ID: "c", Name: "sub", Arguments: json.RawMessage(`{"input":"summarize the doc"}`)}
	if _, err := driver.Execute(context.Background(), call, nil); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if captured != "summarize the doc" {
		t.Fatalf("child goal = %q, want the input field value", captured)
	}
}

func TestAsTool_GoalFromRawArgsWhenNoInputField(t *testing.T) {
	var captured string
	child := Engine{
		Provider:       singleTurnProvider("ok"),
		Model:          "child",
		ContextBuilder: goalRecorder{goal: &captured},
	}
	driver := AsTool(child, SubagentDef{Name: "sub"})

	raw := `{"topic":"weather","city":"NYC"}`
	call := tool.Call{ID: "c", Name: "sub", Arguments: json.RawMessage(raw)}
	if _, err := driver.Execute(context.Background(), call, nil); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if captured != raw {
		t.Fatalf("child goal = %q, want the raw arguments JSON", captured)
	}
}

func TestAsTool_DepthGuardRefusesBeyondMaxDepth(t *testing.T) {
	child := Engine{Provider: singleTurnProvider("ok"), Model: "child"}
	driver := AsTool(child, SubagentDef{Name: "sub", MaxDepth: 1})

	ctx := withSubagentDepth(context.Background(), 1)
	call := tool.Call{ID: "c", Name: "sub", Arguments: json.RawMessage(`{"input":"x"}`)}
	result, err := driver.Execute(ctx, call, nil)
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected an error result when max depth is reached")
	}
	if !strings.Contains(result.Content, "max nesting depth") {
		t.Fatalf("Content = %q, want a max-nesting-depth refusal", result.Content)
	}
}

func TestAsTool_RunsChildAtIncrementedDepth(t *testing.T) {
	probe := &depthProbe{}
	driver := &scriptedProvider{turns: [][]provider.Event{
		{
			{Kind: provider.EventToolCall, ToolCall: &message.ToolCall{ID: "p1", Name: "probe", Arguments: json.RawMessage(`{}`)}},
			{Kind: provider.EventDone, StopReason: provider.StopReasonToolUse},
		},
		{
			{Kind: provider.EventTextDelta, Text: "done"},
			{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
		},
	}}
	child := Engine{Provider: driver, Tools: tool.NewBus(probe), Model: "child"}
	sub := AsTool(child, SubagentDef{Name: "sub"})

	call := tool.Call{ID: "c", Name: "sub", Arguments: json.RawMessage(`{"input":"go"}`)}
	if _, err := sub.Execute(context.Background(), call, nil); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !probe.ran {
		t.Fatal("child never executed its tool")
	}
	if probe.observed != 1 {
		t.Fatalf("child ran at subagent depth %d, want 1 (parent depth 0 + 1)", probe.observed)
	}
}

func TestAsTool_InputSchemaRejectsInvalidArgs(t *testing.T) {
	child := Engine{Provider: singleTurnProvider("ok"), Model: "child"}
	schema := tool.Schema{
		Type:       "object",
		Properties: map[string]tool.Schema{"input": {Type: "string"}},
		Required:   []string{"input"},
	}
	driver := AsTool(child, SubagentDef{Name: "sub", InputSchema: schema})

	call := tool.Call{ID: "c", Name: "sub", Arguments: json.RawMessage(`{"wrong":"x"}`)}
	result, err := driver.Execute(context.Background(), call, nil)
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected an error result for schema-violating arguments")
	}
	if !strings.Contains(result.Content, "input rejected") {
		t.Fatalf("Content = %q, want an input-rejected message", result.Content)
	}
}

func TestAsTool_InputSchemaAcceptsValidArgs(t *testing.T) {
	child := Engine{Provider: singleTurnProvider("child answer"), Model: "child"}
	schema := tool.Schema{
		Type:       "object",
		Properties: map[string]tool.Schema{"input": {Type: "string"}},
		Required:   []string{"input"},
	}
	driver := AsTool(child, SubagentDef{Name: "sub", InputSchema: schema})

	call := tool.Call{ID: "c", Name: "sub", Arguments: json.RawMessage(`{"input":"hello"}`)}
	result, err := driver.Execute(context.Background(), call, nil)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.IsError {
		t.Fatalf("valid args were rejected: %+v", result)
	}
	if result.Content != "child answer" {
		t.Fatalf("Content = %q, want child answer", result.Content)
	}
}

func TestAsTool_EmptySchemaAcceptsAnyArgs(t *testing.T) {
	child := Engine{Provider: singleTurnProvider("child answer"), Model: "child"}
	driver := AsTool(child, SubagentDef{Name: "sub"}) // no InputSchema

	call := tool.Call{ID: "c", Name: "sub", Arguments: json.RawMessage(`{"anything":[1,2,3]}`)}
	result, err := driver.Execute(context.Background(), call, nil)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.IsError {
		t.Fatalf("empty schema rejected arbitrary args: %+v", result)
	}
}

func TestAsTool_ChildFailureBecomesErrorResultNotGoError(t *testing.T) {
	child := Engine{Provider: failingProvider{}, Model: "child"}
	driver := AsTool(child, SubagentDef{Name: "sub"})

	call := tool.Call{ID: "c", Name: "sub", Arguments: json.RawMessage(`{"input":"x"}`)}
	result, err := driver.Execute(context.Background(), call, nil)
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v (a child failure must surface as an error result, not a hard abort)", err)
	}
	if !result.IsError {
		t.Fatal("expected an error result when the child fails")
	}
	if result.Content == "" {
		t.Fatal("error result has no reason content")
	}
	// The typed classification is serialized so a parent can branch on the kind.
	if !strings.Contains(string(result.Structured), "kind") {
		t.Fatalf("Structured = %s, want a serialized AgentFailure with a kind", result.Structured)
	}
}

// goalRecorder is a ContextManager that captures the task goal it is asked to
// build a context for, so a test can assert how AsTool mapped tool arguments to
// the child task.
type goalRecorder struct {
	goal *string
}

func (g goalRecorder) Build(_ context.Context, task api.Task) ([]message.Message, error) {
	*g.goal = task.Goal
	return []message.Message{message.NewText(message.RoleUser, task.Goal)}, nil
}

func (goalRecorder) Compact(_ context.Context, history []message.Message) ([]message.Message, error) {
	return history, nil
}

// depthProbe is a tool that records the subagent nesting depth it observes on
// the context when the child engine invokes it.
type depthProbe struct {
	observed int
	ran      bool
}

func (d *depthProbe) Definition() tool.Definition {
	return tool.Definition{Name: "probe", InputSchema: tool.Schema{Type: "object"}}
}

func (d *depthProbe) Execute(ctx context.Context, call tool.Call, _ tool.UpdateSink) (tool.Result, error) {
	d.observed = subagentDepth(ctx)
	d.ran = true
	return tool.Result{ToolCallID: call.ID, Name: "probe", Content: "ok"}, nil
}

// failingProvider is a driver whose Stream always errors, so a child engine
// built on it produces a Result with a non-nil Failure.
type failingProvider struct{}

func (failingProvider) Metadata() provider.Metadata { return provider.Metadata{Name: "failing"} }

func (failingProvider) Stream(context.Context, provider.Request) (provider.Stream, error) {
	return nil, errors.New("provider unavailable")
}

// staticTool is a tool.Driver with a fixed Definition and a no-op Execute, used
// to give a child engine tools of a known governance shape so a test can assert
// how AsTool aggregates child risk onto the wrapper.
type staticTool struct {
	def tool.Definition
}

func (s staticTool) Definition() tool.Definition { return s.def }

func (s staticTool) Execute(_ context.Context, call tool.Call, _ tool.UpdateSink) (tool.Result, error) {
	return tool.Result{ToolCallID: call.ID, Name: s.def.Name, Content: "ok"}, nil
}

// terminalAnswerTool is a terminal tool.Driver whose Execute returns a fixed
// content and structured payload, simulating a child that submits its final
// answer through a terminal tool rather than as trailing assistant text. (The
// shared terminalTool in agent_test.go sets no Content, so it cannot pin the
// blank-Content regression this test guards.)
type terminalAnswerTool struct {
	name       string
	content    string
	structured json.RawMessage
	isError    bool
}

func (t terminalAnswerTool) Definition() tool.Definition {
	return tool.Definition{Name: t.name, InputSchema: tool.Schema{Type: "object"}, Terminal: true}
}

func (t terminalAnswerTool) Execute(_ context.Context, call tool.Call, _ tool.UpdateSink) (tool.Result, error) {
	return tool.Result{ToolCallID: call.ID, Name: t.name, Content: t.content, Structured: t.structured, IsError: t.isError}, nil
}
