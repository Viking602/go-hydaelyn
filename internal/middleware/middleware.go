package middleware

import (
	"context"

	"github.com/Viking602/go-hydaelyn/hook"
	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/tool"
)

type Stage string

const (
	StageTeam       Stage = "team"
	StagePlanner    Stage = "planner"
	StageTask       Stage = "task"
	StageAgent      Stage = "agent"
	StageLLM        Stage = "llm"
	StageTool       Stage = "tool"
	StageMemory     Stage = "memory"
	StageVerify     Stage = "verify"
	StageSynthesize Stage = "synthesize"
)

type Envelope struct {
	Stage     Stage             `json:"stage"`
	Operation string            `json:"operation,omitempty"`
	TeamID    string            `json:"teamId,omitempty"`
	TaskID    string            `json:"taskId,omitempty"`
	AgentID   string            `json:"agentId,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	Request   any               `json:"request,omitempty"`
	Response  any               `json:"response,omitempty"`
}

type Next func(ctx context.Context, envelope *Envelope) error

type Handler interface {
	Handle(ctx context.Context, envelope *Envelope, next Next) error
}

type Func func(ctx context.Context, envelope *Envelope, next Next) error

func (f Func) Handle(ctx context.Context, envelope *Envelope, next Next) error {
	return f(ctx, envelope, next)
}

type Chain struct {
	handlers []Handler
}

func NewChain(handlers ...Handler) Chain {
	next := make([]Handler, 0, len(handlers))
	for _, handler := range handlers {
		if handler != nil {
			next = append(next, handler)
		}
	}
	return Chain{handlers: next}
}

func (c Chain) Append(handler Handler) Chain {
	if handler == nil {
		return c
	}
	next := append([]Handler{}, c.handlers...)
	next = append(next, handler)
	return Chain{handlers: next}
}

func (c Chain) Handle(ctx context.Context, envelope *Envelope, final Next) error {
	if final == nil {
		final = func(context.Context, *Envelope) error { return nil }
	}
	return c.Wrap(final)(ctx, envelope)
}

func (c Chain) Wrap(final Next) Next {
	next := final
	for idx := len(c.handlers) - 1; idx >= 0; idx-- {
		handler := c.handlers[idx]
		downstream := next
		next = func(ctx context.Context, envelope *Envelope) error {
			return handler.Handle(ctx, envelope, downstream)
		}
	}
	return next
}

func (c Chain) HookAdapter() hook.Handler {
	return hookAdapter{chain: c}
}

func (c Chain) Len() int {
	return len(c.handlers)
}

type hookAdapter struct {
	chain Chain
}

func (h hookAdapter) TransformContext(ctx context.Context, messages []message.Message) ([]message.Message, error) {
	current := append([]message.Message{}, messages...)
	envelope := &Envelope{
		Stage:     StageLLM,
		Operation: "transform_context",
		Request:   messages,
		Response:  &current,
	}
	if err := h.chain.Handle(ctx, envelope, func(_ context.Context, _ *Envelope) error { return nil }); err != nil {
		return nil, err
	}
	switch response := envelope.Response.(type) {
	case []message.Message:
		return append([]message.Message{}, response...), nil
	case *[]message.Message:
		if response == nil {
			return nil, nil
		}
		return append([]message.Message{}, (*response)...), nil
	}
	return current, nil
}

func (h hookAdapter) BeforeModelCall(ctx context.Context, request *provider.Request) error {
	return h.chain.Handle(ctx, &Envelope{
		Stage:     StageLLM,
		Operation: "before",
		Request:   request,
	}, func(_ context.Context, _ *Envelope) error { return nil })
}

func (h hookAdapter) BeforeToolCall(ctx context.Context, call *tool.Call) error {
	return h.chain.Handle(ctx, &Envelope{
		Stage:     StageTool,
		Operation: "before",
		Request:   call,
	}, func(_ context.Context, _ *Envelope) error { return nil })
}

func (h hookAdapter) AfterToolCall(ctx context.Context, result *tool.Result) error {
	return h.chain.Handle(ctx, &Envelope{
		Stage:     StageTool,
		Operation: "after",
		Response:  result,
	}, func(_ context.Context, _ *Envelope) error { return nil })
}

func (h hookAdapter) OnEvent(ctx context.Context, event provider.Event) error {
	return h.chain.Handle(ctx, &Envelope{
		Stage:     StageLLM,
		Operation: "event",
		Response:  event,
	}, func(_ context.Context, _ *Envelope) error { return nil })
}
