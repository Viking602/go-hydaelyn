package core

type TransitionRunCommand struct {
	RunID string
	To    RunStatus
}

type TransitionTaskCommand struct {
	RunID  string
	TaskID string
	To     TaskStatus
}

type AdvanceRunCommand struct {
	RunID string
}
