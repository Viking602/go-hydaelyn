package kit

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Viking602/go-hydaelyn/mailbox"
	"github.com/Viking602/go-hydaelyn/team"
	"github.com/Viking602/go-hydaelyn/tool"
)

// stubMailbox captures Send calls and returns a canned response.
type stubMailbox struct {
	received mailbox.SendInput
	ids      []string
	sendErr  error
	called   int
}

func (s *stubMailbox) Send(_ context.Context, in mailbox.SendInput) ([]string, error) {
	s.called++
	s.received = in
	if s.sendErr != nil {
		return nil, s.sendErr
	}
	return s.ids, nil
}
func (s *stubMailbox) Fetch(context.Context, string, string, int, time.Duration) ([]mailbox.Envelope, error) {
	return nil, nil
}
func (s *stubMailbox) Ack(context.Context, mailbox.Receipt) error  { return nil }
func (s *stubMailbox) Nack(context.Context, string, string) error { return nil }
func (s *stubMailbox) Peek(context.Context, string, string, int) ([]mailbox.Envelope, error) {
	return nil, nil
}
func (s *stubMailbox) RecoverExpiredLeases(context.Context, time.Time) error { return nil }
func (s *stubMailbox) Subscribe(context.Context, string, string) (<-chan mailbox.Envelope, func(), error) {
	return nil, nil, nil
}

type stubProvider struct{ mbox mailbox.Mailbox }

func (p stubProvider) Mailbox() mailbox.Mailbox { return p.mbox }

func newToolWithStub(sendErr error, ids []string) (*stubMailbox, tool.Driver) {
	sb := &stubMailbox{ids: ids, sendErr: sendErr}
	return sb, NewSendMessageTool(stubProvider{mbox: sb})
}

func callerCtx() context.Context {
	return tool.WithCaller(context.Background(), tool.CallerInfo{
		TeamRunID: "run-1",
		AgentID:   "agent-a",
		TaskID:    "task-1",
	})
}

func TestSendMessage_Agent(t *testing.T) {
	stub, driver := newToolWithStub(nil, []string{"env-1"})
	args, _ := json.Marshal(map[string]any{
		"agentId": "agent-b",
		"subject": "hello",
		"body":    "hi there",
		"intent":  "ask",
	})
	res, err := driver.Execute(callerCtx(), tool.Call{ID: "c1", Name: "send_message", Arguments: args}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success, got error: %s", res.Content)
	}
	if stub.called != 1 {
		t.Fatalf("expected 1 Send call, got %d", stub.called)
	}
	if stub.received.To.Kind != mailbox.AddressKindAgent || stub.received.To.AgentID != "agent-b" {
		t.Fatalf("wrong recipient: %+v", stub.received.To)
	}
	if stub.received.From.AgentID != "agent-a" || stub.received.From.TeamRunID != "run-1" {
		t.Fatalf("wrong sender: %+v", stub.received.From)
	}
	if stub.received.Letter.Intent != mailbox.IntentAsk {
		t.Fatalf("intent not normalized: %q", stub.received.Letter.Intent)
	}
}

func TestSendMessage_Role(t *testing.T) {
	stub, driver := newToolWithStub(nil, []string{"env-1", "env-2"})
	args, _ := json.Marshal(map[string]any{
		"role":     string(team.RoleResearcher),
		"body":     "please research",
		"priority": "high",
	})
	res, err := driver.Execute(callerCtx(), tool.Call{ID: "c1", Name: "send_message", Arguments: args}, nil)
	if err != nil || res.IsError {
		t.Fatalf("unexpected: err=%v res=%+v", err, res)
	}
	if stub.received.To.Kind != mailbox.AddressKindRole || stub.received.To.Role != team.RoleResearcher {
		t.Fatalf("wrong role: %+v", stub.received.To)
	}
	if stub.received.Letter.Priority != mailbox.PriorityHigh {
		t.Fatalf("priority not normalized: %q", stub.received.Letter.Priority)
	}
}

func TestSendMessage_MissingCaller(t *testing.T) {
	_, driver := newToolWithStub(nil, []string{"env-1"})
	args, _ := json.Marshal(map[string]any{"agentId": "agent-b", "body": "hi"})
	res, err := driver.Execute(context.Background(), tool.Call{ID: "c1", Name: "send_message", Arguments: args}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected IsError, got: %+v", res)
	}
}

func TestSendMessage_EmptyBody(t *testing.T) {
	_, driver := newToolWithStub(nil, []string{})
	args, _ := json.Marshal(map[string]any{"agentId": "agent-b"})
	res, err := driver.Execute(callerCtx(), tool.Call{ID: "c1", Name: "send_message", Arguments: args}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected IsError for empty body, got: %+v", res)
	}
}

func TestSendMessage_NoRecipientsErrorSurfacedAsError(t *testing.T) {
	_, driver := newToolWithStub(mailbox.ErrNoRecipients, nil)
	args, _ := json.Marshal(map[string]any{"role": "nobody", "body": "hi"})
	res, err := driver.Execute(callerCtx(), tool.Call{ID: "c1", Name: "send_message", Arguments: args}, nil)
	if err != nil {
		t.Fatalf("unexpected error bubble: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected IsError for no recipients, got: %+v", res)
	}
}

func TestSendMessage_UnexpectedErrorBubbles(t *testing.T) {
	_, driver := newToolWithStub(errors.New("boom"), nil)
	args, _ := json.Marshal(map[string]any{"agentId": "agent-b", "body": "hi"})
	_, err := driver.Execute(callerCtx(), tool.Call{ID: "c1", Name: "send_message", Arguments: args}, nil)
	if err == nil {
		t.Fatalf("expected error to bubble")
	}
}

func TestSendMessage_NilMailbox(t *testing.T) {
	driver := NewSendMessageTool(stubProvider{mbox: nil})
	args, _ := json.Marshal(map[string]any{"agentId": "agent-b", "body": "hi"})
	res, err := driver.Execute(callerCtx(), tool.Call{ID: "c1", Name: "send_message", Arguments: args}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected IsError when mailbox is nil, got: %+v", res)
	}
}
