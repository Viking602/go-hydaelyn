package agent

import (
	"context"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/message"
)

// ContextManager builds the initial message slice for one api.Task and
// compacts historical message lists. Engine.Run reads ContextManager.Build to
// seed the loop and wires Compact into it. See LoopInput.Compact for the legacy
// cumulative-budget trigger and TargetContextManager for context-aware
// preparation before each model request.
//
// Compact receives only the history, so it cannot guarantee a token target. It
// must be deterministic and idempotent because the legacy cumulative-budget
// path may call it on every remaining turn, while the context-target fallback
// may call it before every request including the first. Replay must reproduce
// the same compactions (ADR-007).
type ContextManager interface {
	Build(ctx context.Context, task api.Task) ([]message.Message, error)
	Compact(ctx context.Context, history []message.Message) ([]message.Message, error)
}

// TargetContextManager optionally extends ContextManager with token-targeted
// context preparation. Engine.Run invokes CompactTo before every model request,
// including the first, when LoopPolicy.ContextTokenTarget is positive. The
// implementation owns model-appropriate token estimation and should cheaply
// return history unchanged when it already fits. targetTokens is the caller's
// usable history allowance after reserving space for output, tools, schemas,
// reasoning, and provider framing; it is not the model's raw context window.
//
// CompactTo must be deterministic and idempotent for replay, preserve complete
// tool turns, and retain framework-owned skill context messages. The engine
// validates both invariants before issuing the request.
type TargetContextManager interface {
	ContextManager
	CompactTo(ctx context.Context, history []message.Message, targetTokens int) ([]message.Message, error)
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
