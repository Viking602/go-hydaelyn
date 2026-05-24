package agent

import "time"

// ToolSafety classifies a tool's side-effect profile. ToolPolicy uses
// this to decide whether the agent loop may auto-retry the tool on
// transient failures.
//
// Non-idempotent side-effect tools MUST NOT be auto-retried by the
// loop — they MUST be routed through the runner's ActionAttempt
// protocol (api.ActionAttempt, idempotency-key-keyed). The loop calling
// such a tool without an ActionAttempt is a contract violation enforced
// by the GovernedToolBus in worker/.
type ToolSafety int

const (
	ToolReadOnly ToolSafety = iota
	ToolIdempotentSideEffect
	ToolNonIdempotentSideEffect
)

// ToolPolicy is the per-tool execution policy the agent loop enforces.
// Engine reads it from the bound tool.Bus; tool-specific overrides
// layer on top of the engine-wide defaults.
type ToolPolicy struct {
	Timeout        time.Duration `json:"timeout,omitempty"`
	Safety         ToolSafety    `json:"safety,omitempty"`
	MaxConcurrency int           `json:"maxConcurrency,omitempty"`
}
