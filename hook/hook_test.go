package hook

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/tool"
)

type mockHandler struct {
	transformCalled   bool
	beforeModelCalled bool
	beforeToolCalled  bool
	afterToolCalled   bool
	onEventCalled     bool
	returnError       bool
}

func (m *mockHandler) TransformContext(_ context.Context, messages []message.Message) ([]message.Message, error) {
	m.transformCalled = true
	if m.returnError {
		return nil, errors.New("transform error")
	}
	return append(messages, message.Message{Role: message.RoleSystem, Text: "transformed"}), nil
}

func (m *mockHandler) BeforeModelCall(_ context.Context, _ *provider.Request) error {
	m.beforeModelCalled = true
	if m.returnError {
		return errors.New("before model error")
	}
	return nil
}

func (m *mockHandler) BeforeToolCall(_ context.Context, _ *tool.Call) error {
	m.beforeToolCalled = true
	if m.returnError {
		return errors.New("before tool error")
	}
	return nil
}

func (m *mockHandler) AfterToolCall(_ context.Context, _ *tool.Result) error {
	m.afterToolCalled = true
	if m.returnError {
		return errors.New("after tool error")
	}
	return nil
}

func (m *mockHandler) OnEvent(_ context.Context, _ provider.Event) error {
	m.onEventCalled = true
	if m.returnError {
		return errors.New("on event error")
	}
	return nil
}

// panicHandler panics from exactly one hook stage, leaving the others as
// no-ops, so a test can drive each Chain method into a recovered panic.
type panicHandler struct{ stage string }

func (h panicHandler) TransformContext(_ context.Context, m []message.Message) ([]message.Message, error) {
	if h.stage == "TransformContext" {
		panic("transform boom")
	}
	return m, nil
}

func (h panicHandler) BeforeModelCall(_ context.Context, _ *provider.Request) error {
	if h.stage == "BeforeModelCall" {
		panic("before-model boom")
	}
	return nil
}

func (h panicHandler) BeforeToolCall(_ context.Context, _ *tool.Call) error {
	if h.stage == "BeforeToolCall" {
		panic("before-tool boom")
	}
	return nil
}

func (h panicHandler) AfterToolCall(_ context.Context, _ *tool.Result) error {
	if h.stage == "AfterToolCall" {
		panic("after-tool boom")
	}
	return nil
}

func (h panicHandler) OnEvent(_ context.Context, _ provider.Event) error {
	if h.stage == "OnEvent" {
		panic("on-event boom")
	}
	return nil
}

// TestChainConvertsHandlerPanicToError pins the Chain's containment contract:
// a caller-supplied handler that panics must not unwind the engine's stack.
// Every Chain method recovers the panic and returns an ErrHandlerPanic-wrapped
// error instead, so the agent loop can surface a typed failure. If any stage
// failed to recover, this test would crash the package binary rather than fail.
func TestChainConvertsHandlerPanicToError(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		stage string
		call  func(Chain) error
	}{
		{"TransformContext", func(c Chain) error { _, err := c.TransformContext(ctx, nil); return err }},
		{"BeforeModelCall", func(c Chain) error { return c.BeforeModelCall(ctx, &provider.Request{}) }},
		{"BeforeToolCall", func(c Chain) error { return c.BeforeToolCall(ctx, &tool.Call{}) }},
		{"AfterToolCall", func(c Chain) error { return c.AfterToolCall(ctx, &tool.Result{}) }},
		{"OnEvent", func(c Chain) error { return c.OnEvent(ctx, provider.Event{}) }},
	}
	for _, tc := range cases {
		t.Run(tc.stage, func(t *testing.T) {
			chain := NewChain(panicHandler{stage: tc.stage})
			err := tc.call(chain)
			if err == nil {
				t.Fatalf("%s: expected an error from a panicking handler, got nil", tc.stage)
			}
			if !errors.Is(err, ErrHandlerPanic) {
				t.Fatalf("%s: errors.Is(err, ErrHandlerPanic) = false, err = %v", tc.stage, err)
			}
			if !strings.Contains(err.Error(), "panicHandler") {
				t.Fatalf("%s: error %q does not identify the panicking handler", tc.stage, err)
			}
		})
	}
}

func TestNewChain(t *testing.T) {
	handler1 := &mockHandler{}
	handler2 := &mockHandler{}

	chain := NewChain(handler1, handler2)

	if len(chain.handlers) != 2 {
		t.Errorf("NewChain() handlers count = %v, want 2", len(chain.handlers))
	}
}

func TestChainAppend(t *testing.T) {
	handler1 := &mockHandler{}
	handler2 := &mockHandler{}
	handler3 := &mockHandler{}

	chain := NewChain(handler1)
	newChain := chain.Append(handler2)
	newChain2 := newChain.Append(handler3)

	// Original chain should not be modified
	if len(chain.handlers) != 1 {
		t.Errorf("Original chain handlers count = %v, want 1", len(chain.handlers))
	}

	// First appended chain should have 2
	if len(newChain.handlers) != 2 {
		t.Errorf("Appended chain handlers count = %v, want 2", len(newChain.handlers))
	}

	// Second appended chain should have 3
	if len(newChain2.handlers) != 3 {
		t.Errorf("Twice appended chain handlers count = %v, want 3", len(newChain2.handlers))
	}
}

func TestChainTransformContext(t *testing.T) {
	tests := []struct {
		name       string
		handlers   []Handler
		messages   []message.Message
		wantErr    bool
		wantLen    int
		wantCalled []bool
	}{
		{
			name:       "empty chain",
			handlers:   []Handler{},
			messages:   []message.Message{{Role: message.RoleUser, Text: "hello"}},
			wantErr:    false,
			wantLen:    1,
			wantCalled: []bool{},
		},
		{
			name:       "single handler",
			handlers:   []Handler{&mockHandler{}},
			messages:   []message.Message{{Role: message.RoleUser, Text: "hello"}},
			wantErr:    false,
			wantLen:    2,
			wantCalled: []bool{true},
		},
		{
			name:       "multiple handlers",
			handlers:   []Handler{&mockHandler{}, &mockHandler{}},
			messages:   []message.Message{{Role: message.RoleUser, Text: "hello"}},
			wantErr:    false,
			wantLen:    3,
			wantCalled: []bool{true, true},
		},
		{
			name:       "handler returns error",
			handlers:   []Handler{&mockHandler{returnError: true}},
			messages:   []message.Message{{Role: message.RoleUser, Text: "hello"}},
			wantErr:    true,
			wantLen:    0,
			wantCalled: []bool{true},
		},
		{
			name:       "nil handler skipped",
			handlers:   []Handler{nil, &mockHandler{}},
			messages:   []message.Message{{Role: message.RoleUser, Text: "hello"}},
			wantErr:    false,
			wantLen:    2,
			wantCalled: []bool{false, true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chain := NewChain(tt.handlers...)
			got, err := chain.TransformContext(context.Background(), tt.messages)

			if (err != nil) != tt.wantErr {
				t.Errorf("TransformContext() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && len(got) != tt.wantLen {
				t.Errorf("TransformContext() messages length = %v, want %v", len(got), tt.wantLen)
			}

			for i, h := range tt.handlers {
				if h != nil {
					mh := h.(*mockHandler)
					if mh.transformCalled != tt.wantCalled[i] {
						t.Errorf("Handler %d transformCalled = %v, want %v", i, mh.transformCalled, tt.wantCalled[i])
					}
				}
			}
		})
	}
}

func TestChainBeforeModelCall(t *testing.T) {
	tests := []struct {
		name     string
		handlers []Handler
		wantErr  bool
	}{
		{
			name:     "empty chain",
			handlers: []Handler{},
			wantErr:  false,
		},
		{
			name:     "handlers called",
			handlers: []Handler{&mockHandler{}, &mockHandler{}},
			wantErr:  false,
		},
		{
			name:     "handler error",
			handlers: []Handler{&mockHandler{returnError: true}},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chain := NewChain(tt.handlers...)
			req := &provider.Request{Model: "test"}
			err := chain.BeforeModelCall(context.Background(), req)

			if (err != nil) != tt.wantErr {
				t.Errorf("BeforeModelCall() error = %v, wantErr %v", err, tt.wantErr)
			}

			for i, h := range tt.handlers {
				if h != nil {
					mh := h.(*mockHandler)
					if !mh.beforeModelCalled && !tt.wantErr {
						t.Errorf("Handler %d beforeModelCalled = false, want true", i)
					}
				}
			}
		})
	}
}

func TestChainBeforeToolCall(t *testing.T) {
	tests := []struct {
		name     string
		handlers []Handler
		wantErr  bool
	}{
		{
			name:     "empty chain",
			handlers: []Handler{},
			wantErr:  false,
		},
		{
			name:     "handlers called",
			handlers: []Handler{&mockHandler{}, &mockHandler{}},
			wantErr:  false,
		},
		{
			name:     "handler error",
			handlers: []Handler{&mockHandler{returnError: true}},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chain := NewChain(tt.handlers...)
			call := &tool.Call{ID: "call-1", Name: "test-tool"}
			err := chain.BeforeToolCall(context.Background(), call)

			if (err != nil) != tt.wantErr {
				t.Errorf("BeforeToolCall() error = %v, wantErr %v", err, tt.wantErr)
			}

			for i, h := range tt.handlers {
				if h != nil {
					mh := h.(*mockHandler)
					if !mh.beforeToolCalled && !tt.wantErr {
						t.Errorf("Handler %d beforeToolCalled = false, want true", i)
					}
				}
			}
		})
	}
}

func TestChainAfterToolCall(t *testing.T) {
	tests := []struct {
		name     string
		handlers []Handler
		wantErr  bool
	}{
		{
			name:     "empty chain",
			handlers: []Handler{},
			wantErr:  false,
		},
		{
			name:     "handlers called",
			handlers: []Handler{&mockHandler{}, &mockHandler{}},
			wantErr:  false,
		},
		{
			name:     "handler error",
			handlers: []Handler{&mockHandler{returnError: true}},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chain := NewChain(tt.handlers...)
			result := &tool.Result{Content: "result"}
			err := chain.AfterToolCall(context.Background(), result)

			if (err != nil) != tt.wantErr {
				t.Errorf("AfterToolCall() error = %v, wantErr %v", err, tt.wantErr)
			}

			for i, h := range tt.handlers {
				if h != nil {
					mh := h.(*mockHandler)
					if !mh.afterToolCalled && !tt.wantErr {
						t.Errorf("Handler %d afterToolCalled = false, want true", i)
					}
				}
			}
		})
	}
}

func TestChainOnEvent(t *testing.T) {
	tests := []struct {
		name     string
		handlers []Handler
		wantErr  bool
	}{
		{
			name:     "empty chain",
			handlers: []Handler{},
			wantErr:  false,
		},
		{
			name:     "handlers called",
			handlers: []Handler{&mockHandler{}, &mockHandler{}},
			wantErr:  false,
		},
		{
			name:     "handler error",
			handlers: []Handler{&mockHandler{returnError: true}},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chain := NewChain(tt.handlers...)
			event := provider.Event{Kind: provider.EventTextDelta, Text: "test"}
			err := chain.OnEvent(context.Background(), event)

			if (err != nil) != tt.wantErr {
				t.Errorf("OnEvent() error = %v, wantErr %v", err, tt.wantErr)
			}

			for i, h := range tt.handlers {
				if h != nil {
					mh := h.(*mockHandler)
					if !mh.onEventCalled && !tt.wantErr {
						t.Errorf("Handler %d onEventCalled = false, want true", i)
					}
				}
			}
		})
	}
}
