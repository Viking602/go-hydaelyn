package handoff

type HandoffCommand struct {
	RunID          string
	TaskID         string
	FromAgentID    string
	ToAgentID      string
	TaskVersion    int
	HandoffContext string
}

func (HandoffCommand) CommandName() string { return "handoff.request" }
