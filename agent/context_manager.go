package agent

import (
	"context"

	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/message"
)

// ContextManager builds the initial message slice for one api.Task and
// compacts historical message lists when the loop's token budget is
// tight. Engine.Run reads ContextManager.Build to seed the loop and wires
// Compact into it: once the LoopPolicy.MaxTokens / TaskBudget boundary is
// approached, the loop calls Compact before every remaining turn, so an
// implementation must be idempotent on an already-compacted history. See
// LoopInput.Compact for the trigger and determinism contract.
type ContextManager interface {
	Build(ctx context.Context, task api.Task) ([]message.Message, error)
	Compact(ctx context.Context, history []message.Message) ([]message.Message, error)
}

// ContextBuilderFunc adapts a plain function to the ContextManager
// interface; the Compact half is a no-op pass-through.
type ContextBuilderFunc func(ctx context.Context, task api.Task) ([]message.Message, error)

// Build dispatches to the underlying function.
func (f ContextBuilderFunc) Build(ctx context.Context, task api.Task) ([]message.Message, error) {
	return f(ctx, task)
}

// Compact returns the history unchanged.
func (ContextBuilderFunc) Compact(_ context.Context, history []message.Message) ([]message.Message, error) {
	return history, nil
}
