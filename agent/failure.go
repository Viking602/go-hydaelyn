package agent

// FailureKind enumerates the typed failure modes Engine.Run may surface.
// Multi-agent schedulers may branch on FailureKind to decide retry / handoff /
// escalate / approval / terminate.
type FailureKind string

const (
	FailureKindBudgetExhausted    FailureKind = "budget_exhausted"
	FailureKindToolUnavailable    FailureKind = "tool_unavailable"
	FailureKindSchemaInvalid      FailureKind = "schema_invalid"
	FailureKindRepairFailed       FailureKind = "repair_failed"
	FailureKindUnsafeAction       FailureKind = "unsafe_action"
	FailureKindContextBuildFailed FailureKind = "context_build_failed"
	FailureKindEngineError        FailureKind = "engine_error"
	FailureKindStepAborted        FailureKind = "step_aborted"
)

// AgentFailure is the only failure shape that crosses the agent →
// multiagent boundary (boundaries doc Principle 6). Engine.Run must
// surface failures here rather than via a bare error return.
//
// AgentFailure itself satisfies the error interface so call sites that
// want to bubble it through err-typed plumbing can do so; the wrapped
// cause (if any) is exposed via Unwrap, allowing errors.Is / errors.As
// to walk the chain.
type AgentFailure struct {
	Kind        FailureKind `json:"kind"`
	Reason      string      `json:"reason,omitempty"`
	Retryable   bool        `json:"retryable,omitempty"`
	Escalatable bool        `json:"escalatable,omitempty"`

	// cause is the underlying error (if any). Intentionally unexported
	// so it does not serialize through JSON marshaling and so the only
	// way to populate it is the WithCause builder, which the agent
	// package owns. Crossing-boundary consumers use Unwrap.
	cause error
}

// Error returns Reason or, when Reason is empty, the FailureKind value.
func (f *AgentFailure) Error() string {
	if f == nil {
		return "<nil agent failure>"
	}
	if f.Reason != "" {
		return f.Reason
	}
	return string(f.Kind)
}

// Unwrap returns the cause attached via WithCause, enabling
// errors.Is / errors.As traversal across the agent → multiagent
// boundary.
func (f *AgentFailure) Unwrap() error {
	if f == nil {
		return nil
	}
	return f.cause
}

// WithCause attaches an underlying error for errors.Is / errors.As
// propagation. Returns the receiver to allow fluent chaining inside
// Engine.Run; JSON marshaling of AgentFailure does not include the
// cause (persistence relies on Kind + Reason).
func (f *AgentFailure) WithCause(err error) *AgentFailure {
	if f == nil {
		return nil
	}
	f.cause = err
	return f
}
