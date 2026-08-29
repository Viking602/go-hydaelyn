package agent

import (
	"context"

	"github.com/Viking602/venat/message"
)

// ContextManager builds the initial message slice for one Request and compacts
// historical message lists. Implementations must be deterministic and
// idempotent so resumed execution reproduces the same provider input.
type ContextManager interface {
	Build(ctx context.Context, request Request) ([]message.Message, error)
	Compact(ctx context.Context, history []message.Message) ([]message.Message, error)
}

// TargetContextManager optionally adds token-targeted context preparation.
// CompactTo must preserve complete tool turns and framework-owned skill
// context.
type TargetContextManager interface {
	ContextManager
	CompactTo(ctx context.Context, history []message.Message, targetTokens int) ([]message.Message, error)
}

// ContextBuilderFunc adapts a build function to ContextManager. Compact is a
// no-op pass-through.
type ContextBuilderFunc func(ctx context.Context, request Request) ([]message.Message, error)

// Build dispatches to f.
func (f ContextBuilderFunc) Build(ctx context.Context, request Request) ([]message.Message, error) {
	return f(ctx, request)
}

// Compact returns history unchanged.
func (ContextBuilderFunc) Compact(_ context.Context, history []message.Message) ([]message.Message, error) {
	return history, nil
}
