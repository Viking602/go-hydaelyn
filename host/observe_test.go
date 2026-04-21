package host

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Viking602/go-hydaelyn/capability"
	"github.com/Viking602/go-hydaelyn/internal/middleware"
	"github.com/Viking602/go-hydaelyn/internal/session"
	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/observe"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/storage"
	"github.com/Viking602/go-hydaelyn/team"
	"github.com/Viking602/go-hydaelyn/tool/kit"
)

type observeProvider struct{}

func (observeProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "observe-provider"}
}

func (observeProvider) Stream(_ context.Context, request provider.Request) (provider.Stream, error) {
	if len(request.Messages) > 0 && request.Messages[len(request.Messages)-1].Role == message.RoleTool {
		return provider.NewSliceStream([]provider.Event{
			{Kind: provider.EventTextDelta, Text: "done"},
			{Kind: provider.EventDone, StopReason: provider.StopReasonComplete, Usage: provider.Usage{InputTokens: 2, OutputTokens: 3, TotalTokens: 5}},
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
		{Kind: provider.EventDone, StopReason: provider.StopReasonToolUse, Usage: provider.Usage{InputTokens: 3, OutputTokens: 4, TotalTokens: 7}},
	}), nil
}

func TestObserverCapturesTeamTaskLLMToolSignals(t *testing.T) {
	observer := observe.NewMemoryObserver()
	runner := New(Config{})
	runner.UseObserver(observer)
	runner.RegisterProvider("observe-provider", observeProvider{})
	driver, err := kit.Tool("lookup", func(_ context.Context, input struct {
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
		Provider:  "observe-provider",
		Model:     "test",
		Messages:  []message.Message{message.NewText(message.RoleUser, "hello")},
	})
	if err != nil {
		t.Fatalf("Prompt() error = %v", err)
	}
	spans := observer.Spans()
	if len(spans) == 0 {
		t.Fatalf("expected spans, got %#v", spans)
	}
	counters := observer.Counters()
	if counters["llm.calls"] == 0 || counters["tool.calls"] == 0 {
		t.Fatalf("expected llm/tool counters, got %#v", counters)
	}
}

func TestObserverLogsCapabilityDenyWithTraceID(t *testing.T) {
	observer := observe.NewMemoryObserver()
	runner := New(Config{})
	runner.UseObserver(observer)
	runner.UseCapabilityMiddleware(capability.RequireApproval())
	runner.RegisterProvider("observe-provider", observeProvider{})
	driver, err := kit.Tool("lookup", func(_ context.Context, input struct {
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
		Provider:  "observe-provider",
		Model:     "test",
		Messages:  []message.Message{message.NewText(message.RoleUser, "hello")},
	})
	if err == nil {
		t.Fatalf("expected permission error")
	}
	var capErr *capability.Error
	if !errors.As(err, &capErr) {
		t.Fatalf("expected capability error, got %v", err)
	}
	logs := observer.Logs()
	if len(logs) == 0 {
		t.Fatalf("expected logs, got %#v", logs)
	}
	if logs[0].Attrs["trace_id"] == "" {
		t.Fatalf("expected trace_id in logs, got %#v", logs[0])
	}
}

func TestMultiAgentCollaboration_LogsConflictTraceContext(t *testing.T) {
	observer := observe.NewMemoryObserver()
	runner := New(Config{})
	runner.UseObserver(observer)
	state := team.RunState{
		ID:     "team-observe-collab",
		Status: team.StatusRunning,
		Tasks:  []team.Task{{ID: "verify-1", Kind: team.TaskKindVerify, Stage: team.TaskStageVerify, Status: team.TaskStatusCompleted, Result: &team.Result{Summary: "unsupported by verifier"}}, {ID: "task-2", Kind: team.TaskKindResearch, Stage: team.TaskStageImplement, Status: team.TaskStatusRunning}},
	}
	var rootTrace string
	err := runner.runStage(context.Background(), &middleware.Envelope{Stage: middleware.StageTeam, Operation: "observe_collaboration", TeamID: state.ID}, func(ctx context.Context, _ *middleware.Envelope) error {
		rootTrace = observe.TraceID(ctx)
		runner.recordStaleWriteRejectedEvent(ctx, state.ID, "verify-1", "worker-a", eventReasonStateVersionConflict)
		runner.recordVerifierDecisionEvent(ctx, state, state.Tasks[0])
		runner.recordTaskCancelledEvent(ctx, state, state.Tasks[1], eventReasonTeamAborted)
		return nil
	})
	if err != nil {
		t.Fatalf("runStage() error = %v", err)
	}
	if rootTrace == "" {
		t.Fatal("expected root trace id")
	}
	logs := observer.Logs()
	if len(logs) < 3 {
		t.Fatalf("expected collaboration logs, got %#v", logs)
	}
	seen := map[string]bool{}
	for _, log := range logs {
		switch log.Message {
		case string(storage.EventStaleWriteRejected), string(storage.EventVerifierBlocked), string(storage.EventTaskCancelled):
			if log.Attrs["trace_id"] != rootTrace {
				t.Fatalf("expected trace %q on %#v", rootTrace, log)
			}
			if log.Attrs["correlation_id"] == "" || log.Attrs["team_id"] == "" {
				t.Fatalf("expected correlation attrs on %#v", log)
			}
			seen[log.Message] = true
		}
	}
	for _, message := range []string{string(storage.EventStaleWriteRejected), string(storage.EventVerifierBlocked), string(storage.EventTaskCancelled)} {
		if !seen[message] {
			t.Fatalf("expected log for %s in %#v", message, logs)
		}
	}
	counters := observer.Counters()
	if counters["collaboration_stale_writes_rejected"] == 0 || counters["collaboration_verifier_blocked"] == 0 {
		t.Fatalf("expected collaboration counters, got %#v", counters)
	}
}
