package agent

import (
	"time"

	"github.com/Viking602/venat/api"
)

// LoopPolicy bounds one Engine.Run invocation. Distinct from
// api.TaskBudget (which travels with the api.Task itself): LoopPolicy
// is the loop-side default the Engine carries; the per-Task Budget
// overrides it when present.
type LoopPolicy struct {
	MaxIterations int `json:"maxIterations,omitempty"`
	// UnlimitedIterations disables the model-turn ceiling. It is intended for
	// interactive agents that are instead bounded by cancellation, context
	// management, and optional wall-clock/task budgets.
	UnlimitedIterations bool            `json:"unlimitedIterations,omitempty"`
	MaxWallClock        time.Duration   `json:"maxWallClock,omitempty"`
	Budget              *api.TaskBudget `json:"budget,omitempty"`
	ContextTokenTarget  int             `json:"contextTokenTarget,omitempty"`
}
