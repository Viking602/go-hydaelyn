package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/tool"
)

func TestAsTool_DefinitionAdvertisesIdentityAndSchema(t *testing.T) {
	child := Engine{Provider: singleTurnProvider("ok"), Model: "child"}
	schema := tool.Schema{Type: "object"}
	driver := AsTool(child, SubagentDef{Name: "researcher", Description: "delegates research", InputSchema: schema})

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
	if def.EffectType != tool.EffectReadOnly {
		t.Fatalf("Definition().EffectType = %q, want read_only", def.EffectType)
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
