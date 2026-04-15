package hook

import (
	"context"

	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/tool"
)

type Handler interface {
	TransformContext(ctx context.Context, messages []message.Message) ([]message.Message, error)
	BeforeModelCall(ctx context.Context, request *provider.Request) error
	BeforeToolCall(ctx context.Context, call *tool.Call) error
	AfterToolCall(ctx context.Context, result *tool.Result) error
	OnEvent(ctx context.Context, event provider.Event) error
}

type Chain struct {
	handlers []Handler
}

func NewChain(handlers ...Handler) Chain {
	return Chain{handlers: handlers}
}

func (c Chain) Append(handler Handler) Chain {
	next := append([]Handler{}, c.handlers...)
	next = append(next, handler)
	return Chain{handlers: next}
}

func (c Chain) TransformContext(ctx context.Context, messages []message.Message) ([]message.Message, error) {
	current := append([]message.Message{}, messages...)
	for _, handler := range c.handlers {
		if handler == nil {
			continue
		}
		next, err := handler.TransformContext(ctx, current)
		if err != nil {
			return nil, err
		}
		if next != nil {
			current = next
		}
	}
	return current, nil
}

func (c Chain) BeforeModelCall(ctx context.Context, request *provider.Request) error {
	for _, handler := range c.handlers {
		if handler == nil {
			continue
		}
		if err := handler.BeforeModelCall(ctx, request); err != nil {
			return err
		}
	}
	return nil
}

func (c Chain) BeforeToolCall(ctx context.Context, call *tool.Call) error {
	for _, handler := range c.handlers {
		if handler == nil {
			continue
		}
		if err := handler.BeforeToolCall(ctx, call); err != nil {
			return err
		}
	}
	return nil
}

func (c Chain) AfterToolCall(ctx context.Context, result *tool.Result) error {
	for _, handler := range c.handlers {
		if handler == nil {
			continue
		}
		if err := handler.AfterToolCall(ctx, result); err != nil {
			return err
		}
	}
	return nil
}

func (c Chain) OnEvent(ctx context.Context, event provider.Event) error {
	for _, handler := range c.handlers {
		if handler == nil {
			continue
		}
		if err := handler.OnEvent(ctx, event); err != nil {
			return err
		}
	}
	return nil
}
