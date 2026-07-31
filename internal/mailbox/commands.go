package mailbox

import "github.com/Viking602/venat/internal/core/model"

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
	To      model.Address
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

func (DispatchTaskCommand) CommandName() string       { return "task.dispatch" }
func (FanOutDispatchTaskCommand) CommandName() string { return "task.dispatch_fanout" }
func (AckEnvelopeCommand) CommandName() string        { return "mailbox.ack" }
func (DeadLetterCommand) CommandName() string         { return "mailbox.dead_letter" }
