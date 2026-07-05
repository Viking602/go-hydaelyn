package agent

import "time"

// ToolSafety classifies a tool's side-effect profile. In v0.8.0 it is a
// declared policy vocabulary; concrete runtime side-effect gating is enforced
// through tool.Definition metadata and the worker GovernedToolBus.
//
// Engine-level retry decisions based on ToolSafety are reserved for v0.9.0.
// Until that integration lands, non-idempotent side effects are guarded by
// RequiresActionTask / Runner ActionAttempt metadata rather than this enum.
type ToolSafety int

const (
	ToolReadOnly ToolSafety = iota
	ToolIdempotentSideEffect
	ToolNonIdempotentSideEffect
)

// ToolPolicy is the per-tool execution policy vocabulary reserved for
// Engine integration in v0.9.0.
type ToolPolicy struct {
	Timeout        time.Duration `json:"timeout,omitempty"`
	Safety         ToolSafety    `json:"safety,omitempty"`
	MaxConcurrency int           `json:"maxConcurrency,omitempty"`
}
