package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/Viking602/venat/message"
	"github.com/Viking602/venat/provider"
	"github.com/Viking602/venat/tool"
)

// ErrHookPanic reports a panic recovered from caller-supplied hook code.
var ErrHookPanic = errors.New("agent hook panicked")

// Hook can transform context and observe or rewrite one Engine effect path.
type Hook interface {
	TransformContext(context.Context, []message.Message) ([]message.Message, error)
	BeforeModelCall(context.Context, *provider.Request) error
	BeforeToolCall(context.Context, *tool.Call) error
	AfterToolCall(context.Context, *tool.Result) error
	OnEvent(context.Context, provider.Event) error
}

// HookChain invokes hooks in registration order. Its zero value is empty.
type HookChain struct {
	hooks []Hook
}

// NewHookChain constructs a HookChain and preserves registration order.
func NewHookChain(hooks ...Hook) HookChain {
	return HookChain{hooks: append([]Hook(nil), hooks...)}
}

// Len reports the number of registered hooks.
func (c HookChain) Len() int { return len(c.hooks) }

// Append returns a new chain with hook last.
func (c HookChain) Append(hook Hook) HookChain {
	next := append([]Hook(nil), c.hooks...)
	next = append(next, hook)
	return HookChain{hooks: next}
}

// Prepend returns a new chain with hook first. A nil hook is ignored.
func (c HookChain) Prepend(hook Hook) HookChain {
	if hook == nil {
		return c
	}
	next := make([]Hook, 0, len(c.hooks)+1)
	next = append(next, hook)
	next = append(next, c.hooks...)
	return HookChain{hooks: next}
}

func guardHook(stage string, hook Hook, call func() error) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("%w: %s: %T: %v", ErrHookPanic, stage, hook, recovered)
		}
	}()
	return call()
}

// TransformContext applies each non-nil hook to a cloned slice.
func (c HookChain) TransformContext(ctx context.Context, messages []message.Message) ([]message.Message, error) {
	current := message.CloneMessages(messages)
	for _, hook := range c.hooks {
		if hook == nil {
			continue
		}
		var next []message.Message
		if err := guardHook("TransformContext", hook, func() error {
			var err error
			next, err = hook.TransformContext(ctx, current)
			return err
		}); err != nil {
			return nil, err
		}
		if next != nil {
			current = next
		}
	}
	return current, nil
}

// BeforeModelCall invokes each hook in order.
func (c HookChain) BeforeModelCall(ctx context.Context, request *provider.Request) error {
	for _, hook := range c.hooks {
		if hook == nil {
			continue
		}
		if err := guardHook("BeforeModelCall", hook, func() error { return hook.BeforeModelCall(ctx, request) }); err != nil {
			return err
		}
	}
	return nil
}

// BeforeToolCall invokes each hook in order.
func (c HookChain) BeforeToolCall(ctx context.Context, call *tool.Call) error {
	for _, hook := range c.hooks {
		if hook == nil {
			continue
		}
		if err := guardHook("BeforeToolCall", hook, func() error { return hook.BeforeToolCall(ctx, call) }); err != nil {
			return err
		}
	}
	return nil
}

// AfterToolCall invokes each hook in order.
func (c HookChain) AfterToolCall(ctx context.Context, result *tool.Result) error {
	for _, hook := range c.hooks {
		if hook == nil {
			continue
		}
		if err := guardHook("AfterToolCall", hook, func() error { return hook.AfterToolCall(ctx, result) }); err != nil {
			return err
		}
	}
	return nil
}

// OnEvent invokes each hook in order.
func (c HookChain) OnEvent(ctx context.Context, event provider.Event) error {
	for _, hook := range c.hooks {
		if hook == nil {
			continue
		}
		if err := guardHook("OnEvent", hook, func() error { return hook.OnEvent(ctx, event) }); err != nil {
			return err
		}
	}
	return nil
}
