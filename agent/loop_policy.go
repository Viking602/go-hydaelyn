package agent

import (
	"time"

	"github.com/Viking602/go-hydaelyn/api"
)

// LoopPolicy bounds one Engine.Run invocation. Distinct from
// api.TaskBudget (which travels with the api.Task itself): LoopPolicy
// is the loop-side default the Engine carries; the per-Task Budget
// overrides it when present.
type LoopPolicy struct {
	MaxIterations      int             `json:"maxIterations,omitempty"`
	MaxWallClock       time.Duration   `json:"maxWallClock,omitempty"`
	Budget             *api.TaskBudget `json:"budget,omitempty"`
	ContextTokenTarget int             `json:"contextTokenTarget,omitempty"`
}
