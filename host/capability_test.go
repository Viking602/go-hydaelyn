package host

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Viking602/go-hydaelyn/capability"
	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/internal/session"
	"github.com/Viking602/go-hydaelyn/storage"
	"github.com/Viking602/go-hydaelyn/tool"
	"github.com/Viking602/go-hydaelyn/toolkit"
)

type capabilityProvider struct{}

func (capabilityProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "cap-provider"}
}

func (capabilityProvider) Stream(_ context.Context, request provider.Request) (provider.Stream, error) {
	if len(request.Messages) > 0 && request.Messages[len(request.Messages)-1].Role == message.RoleTool {
		return provider.NewSliceStream([]provider.Event{
			{Kind: provider.EventTextDelta, Text: "done"},
			{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
		}), nil
	}
	return provider.NewSliceStream([]provider.Event{
		{
			Kind: provider.EventToolCall,
			ToolCall: &message.ToolCall{
				ID:        "call-1",
				Name:      "lookup",
				Arguments: json.RawMessage(`{"query":"hydaelyn"}`),
			},
		},
		{Kind: provider.EventDone, StopReason: provider.StopReasonToolUse},
	}), nil
}

func TestCapabilityMiddlewareObservesLLMAndToolCalls(t *testing.T) {
	runner := New(Config{})
	trace := make([]string, 0, 4)
	runner.UseCapabilityMiddleware(capability.Func(func(ctx context.Context, call capability.Call, next capability.Next) (capability.Result, error) {
		trace = append(trace, string(call.Type)+":"+call.Name)
		return next(ctx, call)
	}))
	runner.RegisterProvider("cap-provider", capabilityProvider{})
	driver, err := toolkit.Tool("lookup", func(_ context.Context, input struct {
		Query string `json:"query"`
	}) (string, error) {
		return "result:" + input.Query, nil
	})
	if err != nil {
		t.Fatalf("Tool() error = %v", err)
	}
	runner.RegisterTool(driver)
	sess, err := runner.CreateSession(context.Background(), session.CreateParams{Branch: "main"})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	_, err = runner.Prompt(context.Background(), PromptRequest{
		SessionID: sess.ID,
		Provider:  "cap-provider",
		Model:     "test",
		Messages:  []message.Message{message.NewText(message.RoleUser, "go")},
	})
	if err != nil {
		t.Fatalf("Prompt() error = %v", err)
	}
	if len(trace) < 2 {
		t.Fatalf("expected capability middleware trace, got %#v", trace)
	}
	if trace[0] != "llm:cap-provider" {
		t.Fatalf("expected llm capability first, got %#v", trace)
	}
	foundTool := false
	for _, item := range trace {
		if item == "tool:lookup" {
			foundTool = true
		}
	}
	if !foundTool {
		t.Fatalf("expected tool capability call, got %#v", trace)
	}
}

func TestPolicyOutcomeEmission(t *testing.T) {
	t.Parallel()

	runner := New(Config{})
	runner.UseCapabilityMiddleware(capability.RequirePermissions())
	runner.RegisterCapability(capability.TypeTool, "dangerous", func(context.Context, capability.Call) (capability.Result, error) {
		return capability.Result{Output: "should-not-run"}, nil
	})

	_, err := runner.InvokeCapability(context.Background(), capability.Call{
		Type: capability.TypeTool,
		Name: "dangerous",
		Permissions: []capability.Permission{{
			Name:    "tool:dangerous",
			Granted: false,
		}},
		Metadata: map[string]string{
			"teamId": "team-policy-outcome",
			"taskId": "task-1",
		},
	})
	if err == nil {
		t.Fatal("expected permission denial")
	}
	var capErr *capability.Error
	if !errors.As(err, &capErr) || capErr.Kind != capability.ErrorKindPermission {
		t.Fatalf("expected permission error, got %v", err)
	}

	events, err := runner.storage.Events().List(context.Background(), "team-policy-outcome")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 policy outcome event, got %#v", events)
	}
	event := events[0]
	if event.Type != storage.EventPolicyOutcome {
		t.Fatalf("expected policy outcome event, got %#v", event)
	}
	if event.TeamID != "team-policy-outcome" || event.TaskID != "task-1" {
		t.Fatalf("expected event correlation, got %#v", event)
	}
	if got := event.Payload["policy"]; got != "capability.permission" {
		t.Fatalf("expected permission policy, got %#v", event.Payload)
	}
	if got := event.Payload["schemaVersion"]; got != "1.1" {
		t.Fatalf("expected v1.1 schema version, got %#v", event.Payload)
	}
	if got := event.Payload["layer"]; got != "capability" {
		t.Fatalf("expected capability layer, got %#v", event.Payload)
	}
	if got := event.Payload["stage"]; got != "tool" {
		t.Fatalf("expected tool stage, got %#v", event.Payload)
	}
	if got := event.Payload["operation"]; got != "invoke" {
		t.Fatalf("expected invoke operation, got %#v", event.Payload)
	}
	if got := event.Payload["action"]; got != "block" {
		t.Fatalf("expected block action, got %#v", event.Payload)
	}
	if got := event.Payload["outcome"]; got != "denied" {
		t.Fatalf("expected denied outcome, got %#v", event.Payload)
	}
	if got := event.Payload["severity"]; got != "error" {
		t.Fatalf("expected error severity, got %#v", event.Payload)
	}
	if blocked, _ := event.Payload["blocking"].(bool); !blocked {
		t.Fatalf("expected blocking policy outcome, got %#v", event.Payload)
	}
	if evidence, ok := event.Payload["evidence"].(map[string]any); !ok || evidence == nil {
		t.Fatalf("expected evidence payload, got %#v", event.Payload)
	}
}

func TestCapabilityToolDriverUsesTrustedSecurityContextPermissions(t *testing.T) {
	t.Parallel()

	runner := New(Config{})
	runner.UseCapabilityMiddleware(capability.RequirePermissions())
	driver, err := toolkit.Tool(
		"guarded-write",
		func(_ context.Context, _ struct{}) (string, error) { return "ok", nil },
		toolkit.RequiredPermissions("tool:guarded-write"),
	)
	if err != nil {
		t.Fatalf("Tool() error = %v", err)
	}
	runner.RegisterTool(driver)

	_, err = runner.tools.Execute(context.Background(), tool.Call{ID: "call-1", Name: "guarded-write", Arguments: []byte(`{}`)}, nil)
	if err == nil {
		t.Fatal("expected permission denial without trusted grant")
	}
	var capErr *capability.Error
	if !errors.As(err, &capErr) || capErr.Kind != capability.ErrorKindPermission {
		t.Fatalf("expected permission error, got %v", err)
	}

	ctx := capability.WithPermissionGrant(context.Background(), capability.PermissionGrant{Name: "tool:guarded-write", GrantedBy: "policy"})
	result, err := runner.tools.Execute(ctx, tool.Call{ID: "call-2", Name: "guarded-write", Arguments: []byte(`{}`)}, nil)
	if err != nil {
		t.Fatalf("expected trusted grant to pass, got %v", err)
	}
	if result.Name != "guarded-write" || result.Content != "ok" {
		t.Fatalf("unexpected tool result %#v", result)
	}
}

func TestCapabilityToolDriverFallsBackToMetadataPermissions(t *testing.T) {
	t.Parallel()

	runner := New(Config{})
	runner.UseCapabilityMiddleware(capability.RequirePermissions())
	driver, err := toolkit.Tool(
		"legacy-guarded-write",
		func(_ context.Context, _ struct{}) (string, error) { return "ok", nil },
		toolkit.Metadata(map[string]string{"permission": "tool:legacy-guarded-write"}),
	)
	if err != nil {
		t.Fatalf("Tool() error = %v", err)
	}
	runner.RegisterTool(driver)

	_, err = runner.tools.Execute(context.Background(), tool.Call{ID: "call-1", Name: "legacy-guarded-write", Arguments: []byte(`{}`)}, nil)
	if err == nil {
		t.Fatal("expected metadata permission fallback to deny without trusted grant")
	}
	ctx := capability.WithPermissionGrant(context.Background(), capability.PermissionGrant{Name: "tool:legacy-guarded-write", GrantedBy: "policy"})
	if _, err := runner.tools.Execute(ctx, tool.Call{ID: "call-2", Name: "legacy-guarded-write", Arguments: []byte(`{}`)}, nil); err != nil {
		t.Fatalf("expected metadata permission fallback to allow trusted grant, got %v", err)
	}
}
