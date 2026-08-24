package agent

import (
	"context"

	"github.com/Viking602/venat/message"
	"github.com/Viking602/venat/tool"
)

// ContextTransition applies a host-owned deterministic history transition after
// one tool batch has been appended. Checkpoint/rewind hosts use it to replace
// exploratory history with a concise report without teaching the core runtime
// product-specific tool names. Implementations must return complete history.
type ContextTransition interface {
	Apply(context.Context, []message.Message, []tool.Result) ([]message.Message, error)
}
