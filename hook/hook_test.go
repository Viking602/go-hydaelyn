package hook

import (
	"context"
	"errors"
	"testing"

	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/tool"
)

type mockHandler struct {
	transformCalled    bool
	beforeModelCalled  bool
	beforeToolCalled   bool
	afterToolCalled    bool
	onEventCalled      bool
	returnError        bool
}

func (m *mockHandler) TransformContext(ctx context.Context, messages []message.Message) ([]message.Message, error) {
	m.transformCalled = true
	if m.returnError {
		return nil, errors.New("transform error")
	}
	return append(messages, message.Message{Role: message.RoleSystem, Text: "transformed"}), nil
}

func (m *mockHandler) BeforeModelCall(ctx context.Context, request *provider.Request) error {
	m.beforeModelCalled = true
	if m.returnError {
		return errors.New("before model error")
	}
	return nil
}

func (m *mockHandler) BeforeToolCall(ctx context.Context, call *tool.Call) error {
	m.beforeToolCalled = true
	if m.returnError {
		return errors.New("before tool error")
	}
	return nil
}

func (m *mockHandler) AfterToolCall(ctx context.Context, result *tool.Result) error {
	m.afterToolCalled = true
	if m.returnError {
		return errors.New("after tool error")
	}
	return nil
}

func (m *mockHandler) OnEvent(ctx context.Context, event provider.Event) error {
	m.onEventCalled = true
	if m.returnError {
		return errors.New("on event error")
	}
	return nil
}

func TestNewChain(t *testing.T) {
	handler1 := &mockHandler{}
	handler2 := &mockHandler{}

	chain := NewChain(handler1, handler2)

	if len(chain.handlers) != 2 {
		t.Errorf("NewChain() handlers count = %v, want 2", len(chain.handlers))
	}
}

func TestChain_Append(t *testing.T) {
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

func TestChain_TransformContext(t *testing.T) {
	tests := []struct {
		name         string
		handlers     []Handler
		messages     []message.Message
		wantErr      bool
		wantLen      int
		wantCalled   []bool
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

func TestChain_BeforeModelCall(t *testing.T) {
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

func TestChain_BeforeToolCall(t *testing.T) {
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

func TestChain_AfterToolCall(t *testing.T) {
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

func TestChain_OnEvent(t *testing.T) {
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