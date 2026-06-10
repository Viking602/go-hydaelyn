package hook

import (
	"context"
	"errors"
	"fmt"

	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/tool"
)

// ErrHandlerPanic wraps a panic recovered from a caller-supplied Handler.
// Handlers are extension points the engine invokes mid-loop; a handler that
// panics must not unwind the engine's stack and crash the worker process.
// The Chain recovers the panic and returns it as an error so the agent loop
// surfaces a typed failure instead. errors.Is(err, ErrHandlerPanic) reports
// whether a Chain error originated from a panicking handler.
var ErrHandlerPanic = errors.New("hook handler panicked")

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

func (c Chain) Prepend(handler Handler) Chain {
	if handler == nil {
		return c
	}
	next := make([]Handler, 0, len(c.handlers)+1)
	next = append(next, handler)
	next = append(next, c.handlers...)
	return Chain{handlers: next}
}

// guard runs one handler invocation, converting a panic raised by the
// caller-supplied handler into an ErrHandlerPanic-wrapped error tagged with
// the hook stage and the handler's dynamic type, so a chain with many
// handlers identifies which one panicked. A handler is untrusted code from
// the loop's perspective, so a panic in one must degrade to a typed failure
// rather than crash the engine.
func guard(stage string, handler Handler, fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%w: %s: %T: %v", ErrHandlerPanic, stage, handler, r)
		}
	}()
	return fn()
}

func (c Chain) TransformContext(ctx context.Context, messages []message.Message) ([]message.Message, error) {
	current := append([]message.Message{}, messages...)
	for _, handler := range c.handlers {
		if handler == nil {
			continue
		}
		var next []message.Message
		if err := guard("TransformContext", handler, func() error {
			var e error
			next, e = handler.TransformContext(ctx, current)
			return e
		}); err != nil {
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
		if err := guard("BeforeModelCall", handler, func() error {
			return handler.BeforeModelCall(ctx, request)
		}); err != nil {
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
		if err := guard("BeforeToolCall", handler, func() error {
			return handler.BeforeToolCall(ctx, call)
		}); err != nil {
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
		if err := guard("AfterToolCall", handler, func() error {
			return handler.AfterToolCall(ctx, result)
		}); err != nil {
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
		if err := guard("OnEvent", handler, func() error {
			return handler.OnEvent(ctx, event)
		}); err != nil {
			return err
		}
	}
	return nil
}
