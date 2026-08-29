package agent

// FailureKind classifies a factual Engine outcome. Applications decide retry,
// escalation, approval, and routing from Kind, Reason, and the error chain.
type FailureKind string

const (
	FailureKindBudgetExhausted    FailureKind = "budget_exhausted"
	FailureKindToolUnavailable    FailureKind = "tool_unavailable"
	FailureKindSchemaInvalid      FailureKind = "schema_invalid"
	FailureKindRepairFailed       FailureKind = "repair_failed"
	FailureKindOutputBlocked      FailureKind = "output_blocked"
	FailureKindContextBuildFailed FailureKind = "context_build_failed"
	FailureKindEngineError        FailureKind = "engine_error"
	FailureKindStepAborted        FailureKind = "step_aborted"
)

// AgentFailure is the typed failure carried by Result. The unexported cause is
// intentionally omitted from JSON while remaining available to errors.Is and
// errors.As.
type AgentFailure struct {
	Kind   FailureKind `json:"kind"`
	Reason string      `json:"reason,omitempty"`

	cause error
}

// Error returns Reason or the failure kind.
func (f *AgentFailure) Error() string {
	if f == nil {
		return "<nil agent failure>"
	}
	if f.Reason != "" {
		return f.Reason
	}
	return string(f.Kind)
}

// Unwrap exposes the attached cause.
func (f *AgentFailure) Unwrap() error {
	if f == nil {
		return nil
	}
	return f.cause
}

// WithCause attaches an error without changing the serializable failure.
func (f *AgentFailure) WithCause(err error) *AgentFailure {
	if f != nil {
		f.cause = err
	}
	return f
}
