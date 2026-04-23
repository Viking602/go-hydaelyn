package host

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/Viking602/go-hydaelyn/agent"
	"github.com/Viking602/go-hydaelyn/internal/session"
	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/storage"
	"github.com/Viking602/go-hydaelyn/tool/kit"
)

type fakeProvider struct{}

func (fakeProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "fake"}
}

func (fakeProvider) Stream(_ context.Context, request provider.Request) (provider.Stream, error) {
	if len(request.Messages) > 0 && request.Messages[len(request.Messages)-1].Role == message.RoleTool {
		return provider.NewSliceStream([]provider.Event{
			{Kind: provider.EventTextDelta, Text: "complete"},
			{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
		}), nil
	}
	return provider.NewSliceStream([]provider.Event{
		{
			Kind: provider.EventToolCall,
			ToolCall: &message.ToolCall{
				ID:        "call-1",
				Name:      "answer",
				Arguments: json.RawMessage(`{"topic":"mcp"}`),
			},
		},
		{Kind: provider.EventDone, StopReason: provider.StopReasonToolUse},
	}), nil
}

func TestPrompt(t *testing.T) {
	runner := New(Config{})
	runner.RegisterProvider("fake", fakeProvider{})
	driver, err := kit.Tool("answer", func(_ context.Context, input struct {
		Topic string `json:"topic"`
	}) (string, error) {
		return "topic:" + input.Topic, nil
	})
	if err != nil {
		t.Fatalf("Tool() error = %v", err)
	}
	runner.RegisterTool(driver)
	sess, err := runner.CreateSession(context.Background(), session.CreateParams{Branch: "main"})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	response, err := runner.Prompt(context.Background(), PromptRequest{
		SessionID: sess.ID,
		Provider:  "fake",
		Model:     "test",
		Messages:  []message.Message{message.NewText(message.RoleUser, "go")},
	})
	if err != nil {
		t.Fatalf("Prompt() error = %v", err)
	}
	if len(response.Messages) != 3 {
		t.Fatalf("expected assistant/tool/assistant chain, got %d messages", len(response.Messages))
	}
}

type delayedPromptProvider struct {
	gate chan struct{}
}

func (p *delayedPromptProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "delayed"}
}

func (p *delayedPromptProvider) Stream(context.Context, provider.Request) (provider.Stream, error) {
	return &delayedPromptStream{gate: p.gate}, nil
}

type delayedPromptStream struct {
	gate  chan struct{}
	stage int
}

func (s *delayedPromptStream) Recv() (provider.Event, error) {
	switch s.stage {
	case 0:
		s.stage++
		return provider.Event{Kind: provider.EventTextDelta, Text: "partial"}, nil
	case 1:
		<-s.gate
		s.stage++
		return provider.Event{Kind: provider.EventDone, StopReason: provider.StopReasonComplete}, nil
	default:
		return provider.Event{}, io.EOF
	}
}

func (s *delayedPromptStream) Close() error { return nil }

func TestPromptStreamDeliversFinalDisplayAfterProviderCompletes(t *testing.T) {
	runner := New(Config{})
	providerDriver := &delayedPromptProvider{gate: make(chan struct{})}
	runner.RegisterProvider("delayed", providerDriver)
	sess, err := runner.CreateSession(context.Background(), session.CreateParams{Branch: "main"})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	events := make(chan DisplayEvent, 1)
	resultErr := make(chan error, 1)
	go func() {
		_, err := runner.PromptStream(context.Background(), PromptRequest{
			SessionID: sess.ID,
			Provider:  "delayed",
			Model:     "test",
			Messages:  []message.Message{message.NewText(message.RoleUser, "stream now")},
		}, func(event DisplayEvent) error {
			if event.Kind == DisplayEventKindFinal {
				select {
				case events <- event:
				default:
				}
			}
			return nil
		})
		resultErr <- err
	}()

	close(providerDriver.gate)
	select {
	case event := <-events:
		if event.Text != "partial" {
			t.Fatalf("expected canonical final display event, got %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("expected PromptStream to surface final display event after provider completion")
	}

	select {
	case err := <-resultErr:
		if err != nil {
			t.Fatalf("PromptStream() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("PromptStream did not finish after provider gate opened")
	}
}

type cancelAwarePromptProvider struct{}

func (cancelAwarePromptProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "cancel-aware"}
}

func (cancelAwarePromptProvider) Stream(ctx context.Context, _ provider.Request) (provider.Stream, error) {
	return &cancelAwarePromptStream{ctx: ctx}, nil
}

type cancelAwarePromptStream struct {
	ctx   context.Context
	stage int
}

func (s *cancelAwarePromptStream) Recv() (provider.Event, error) {
	switch s.stage {
	case 0:
		s.stage++
		return provider.Event{Kind: provider.EventTextDelta, Text: "partial"}, nil
	case 1:
		<-s.ctx.Done()
		s.stage++
		return provider.Event{}, s.ctx.Err()
	default:
		return provider.Event{}, io.EOF
	}
}

func (s *cancelAwarePromptStream) Close() error { return nil }

func TestPromptStreamCancellationInterruptsUpstreamProvider(t *testing.T) {
	runner := New(Config{})
	runner.RegisterProvider("cancel-aware", cancelAwarePromptProvider{})
	sess, err := runner.CreateSession(context.Background(), session.CreateParams{Branch: "main"})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	_, err = runner.PromptStream(ctx, PromptRequest{
		SessionID: sess.ID,
		Provider:  "cancel-aware",
		Model:     "test",
		Messages:  []message.Message{message.NewText(message.RoleUser, "stream now")},
	}, func(DisplayEvent) error {
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation to interrupt upstream provider, got %v", err)
	}
}

func TestPromptStreamReturnsFinalDisplayOnly(t *testing.T) {
	runner := New(Config{})
	runner.RegisterProvider("capture", &capturePromptProvider{
		turns: [][]provider.Event{{
			{Kind: provider.EventTextDelta, Text: "unsafe "},
			{Kind: provider.EventTextDelta, Text: "final"},
			{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
		}},
	})
	sess, err := runner.CreateSession(context.Background(), session.CreateParams{Branch: "main"})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	events := make([]DisplayEvent, 0, 2)
	response, err := runner.PromptStream(context.Background(), PromptRequest{
		SessionID: sess.ID,
		Provider:  "capture",
		Model:     "test",
		Messages:  []message.Message{message.NewText(message.RoleUser, "go")},
	}, func(event DisplayEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatalf("PromptStream() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected one final display event, got %#v", events)
	}
	if events[0].Kind != DisplayEventKindFinal || events[0].Text != "unsafe final" {
		t.Fatalf("unexpected display events %#v", events)
	}
	if response.UserFacingAnswer != "unsafe final" {
		t.Fatalf("expected user-facing answer, got %#v", response)
	}
}

type capturePromptProvider struct {
	requests []provider.Request
	turns    [][]provider.Event
}

func (p *capturePromptProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "capture"}
}

func (p *capturePromptProvider) Stream(_ context.Context, request provider.Request) (provider.Stream, error) {
	p.requests = append(p.requests, request)
	turn := len(p.requests) - 1
	if turn >= len(p.turns) {
		turn = len(p.turns) - 1
	}
	return provider.NewSliceStream(p.turns[turn]), nil
}

func TestPromptForwardsAgentOptionsAndRecordsOutputGuardrailPolicyOutcome(t *testing.T) {
	runner := New(Config{})
	providerDriver := &capturePromptProvider{
		turns: [][]provider.Event{{
			{Kind: provider.EventTextDelta, Text: "unsafe final"},
			{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
		}},
	}
	runner.RegisterProvider("capture", providerDriver)
	runner.RegisterOutputGuardrail("safe-final", agent.NewOutputGuardrail("safe-final", func(_ context.Context, input agent.OutputGuardrailInput) (agent.OutputGuardrailResult, error) {
		if input.Output.Text == "unsafe final" {
			result := agent.ReplaceOutput(message.NewText(message.RoleAssistant, "safe final"))
			result.Metadata = map[string]string{"decision_source": "test"}
			return result, nil
		}
		return agent.AllowOutput(), nil
	}))
	sess, err := runner.CreateSession(context.Background(), session.CreateParams{Branch: "main"})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	response, err := runner.Prompt(context.Background(), PromptRequest{
		SessionID: sess.ID,
		Provider:  "capture",
		Model:     "test",
		Messages:  []message.Message{message.NewText(message.RoleUser, "go")},
		Agent: AgentOptions{
			MaxIterations:        2,
			StopSequences:        []string{"STOP"},
			ThinkingBudget:       42,
			OutputGuardrailNames: []string{"safe-final"},
		},
	})
	if err != nil {
		t.Fatalf("Prompt() error = %v", err)
	}
	if len(providerDriver.requests) != 1 {
		t.Fatalf("expected one provider request, got %d", len(providerDriver.requests))
	}
	request := providerDriver.requests[0]
	if len(request.StopSequences) != 1 || request.StopSequences[0] != "STOP" {
		t.Fatalf("expected stop sequences to be forwarded, got %#v", request.StopSequences)
	}
	if request.ThinkingBudget != 42 {
		t.Fatalf("expected thinking budget to be forwarded, got %d", request.ThinkingBudget)
	}
	if got := response.Messages[len(response.Messages)-1].Text; got != "safe final" {
		t.Fatalf("expected guardrail replacement in prompt response, got %#v", response.Messages)
	}

	events, err := runner.storage.Events().List(context.Background(), response.Run.ID)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	var policyEvent *storage.Event
	for idx := range events {
		if events[idx].Type == storage.EventPolicyOutcome && events[idx].Payload["policy"] == "output_guardrail.safe-final" {
			policyEvent = &events[idx]
			break
		}
	}
	if policyEvent == nil {
		t.Fatalf("expected output guardrail policy outcome event, got %#v", events)
	}
	if got := policyEvent.Payload["outcome"]; got != "replaced" {
		t.Fatalf("expected replaced outcome, got %#v", policyEvent.Payload)
	}
}

func TestPromptUsesAgentMaxIterations(t *testing.T) {
	runner := New(Config{})
	providerDriver := &capturePromptProvider{
		turns: [][]provider.Event{
			{
				{Kind: provider.EventTextDelta, Text: "draft answer"},
				{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
			},
			{
				{Kind: provider.EventTextDelta, Text: "should not happen"},
				{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
			},
		},
	}
	runner.RegisterProvider("capture", providerDriver)
	runner.RegisterOutputGuardrail("retry", agent.NewOutputGuardrail("retry", func(_ context.Context, input agent.OutputGuardrailInput) (agent.OutputGuardrailResult, error) {
		if input.Output.Text == "draft answer" {
			return agent.RetryOutput(message.NewText(message.RoleUser, "revise")), nil
		}
		return agent.AllowOutput(), nil
	}))
	sess, err := runner.CreateSession(context.Background(), session.CreateParams{Branch: "main"})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	_, err = runner.Prompt(context.Background(), PromptRequest{
		SessionID: sess.ID,
		Provider:  "capture",
		Model:     "test",
		Messages:  []message.Message{message.NewText(message.RoleUser, "go")},
		Agent: AgentOptions{
			MaxIterations:        1,
			OutputGuardrailNames: []string{"retry"},
		},
	})
	var retryErr *agent.OutputGuardrailRetryLimitExceededError
	if !errors.As(err, &retryErr) {
		t.Fatalf("expected retry limit error from forwarded max iterations, got %v", err)
	}
}

func TestPromptAppliesDefaultAndInlineOutputGuardrails(t *testing.T) {
	runner := New(Config{})
	providerDriver := &capturePromptProvider{
		turns: [][]provider.Event{{
			{Kind: provider.EventTextDelta, Text: "unsafe final"},
			{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
		}},
	}
	runner.RegisterProvider("capture", providerDriver)
	runner.UseOutputGuardrail(agent.NewOutputGuardrail("default-safe", func(_ context.Context, input agent.OutputGuardrailInput) (agent.OutputGuardrailResult, error) {
		if input.Output.Text == "unsafe final" {
			return agent.ReplaceOutput(message.NewText(message.RoleAssistant, "default safe final")), nil
		}
		return agent.AllowOutput(), nil
	}))
	sess, err := runner.CreateSession(context.Background(), session.CreateParams{Branch: "main"})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	response, err := runner.Prompt(context.Background(), PromptRequest{
		SessionID: sess.ID,
		Provider:  "capture",
		Model:     "test",
		Messages:  []message.Message{message.NewText(message.RoleUser, "go")},
	})
	if err != nil {
		t.Fatalf("Prompt() error = %v", err)
	}
	if got := response.Messages[len(response.Messages)-1].Text; got != "default safe final" {
		t.Fatalf("expected default output guardrail replacement, got %#v", response.Messages)
	}
}

func TestPromptInlineOutputGuardrailOverridesCandidate(t *testing.T) {
	runner := New(Config{})
	providerDriver := &capturePromptProvider{
		turns: [][]provider.Event{{
			{Kind: provider.EventTextDelta, Text: "unsafe final"},
			{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
		}},
	}
	runner.RegisterProvider("capture", providerDriver)
	sess, err := runner.CreateSession(context.Background(), session.CreateParams{Branch: "main"})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	response, err := runner.Prompt(context.Background(), PromptRequest{
		SessionID: sess.ID,
		Provider:  "capture",
		Model:     "test",
		Messages:  []message.Message{message.NewText(message.RoleUser, "go")},
		Agent: AgentOptions{
			OutputGuardrails: []agent.OutputGuardrail{
				agent.NewOutputGuardrail("inline-safe", func(_ context.Context, input agent.OutputGuardrailInput) (agent.OutputGuardrailResult, error) {
					if input.Output.Text == "unsafe final" {
						return agent.ReplaceOutput(message.NewText(message.RoleAssistant, "inline safe final")), nil
					}
					return agent.AllowOutput(), nil
				}),
			},
		},
	})
	if err != nil {
		t.Fatalf("Prompt() error = %v", err)
	}
	if got := response.Messages[len(response.Messages)-1].Text; got != "inline safe final" {
		t.Fatalf("expected inline output guardrail replacement, got %#v", response.Messages)
	}
}
