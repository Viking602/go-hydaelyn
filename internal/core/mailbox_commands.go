package core

type DispatchTaskCommand struct {
	RunID           string
	TaskID          string
	TargetAgentID   string
	TargetComponent string
	Payload         map[string]any
}

// FanOutDispatchTaskCommand dispatches one task to multiple recipients
// resolved from an Address. The framework writes one envelope per resolved
// agent; per-task ownership remains the developer's responsibility.
type FanOutDispatchTaskCommand struct {
	RunID   string
	TaskID  string
	To      Address
	Payload map[string]any
}

type AckEnvelopeCommand struct {
	EnvelopeID string
	HolderID   string
}

type DeadLetterCommand struct {
	EnvelopeID string
	Reason     string
}
