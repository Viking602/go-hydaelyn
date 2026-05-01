package core

import "context"

type HandoffCommand struct {
	RunID          string
	TaskID         string
	FromAgentID    string
	ToAgentID      string
	TaskVersion    int
	HandoffContext string
}

func (r *Runtime) RequestHandoff(ctx context.Context, cmd HandoffCommand) error {
	_, err := r.ExecuteCommand(ctx, cmd)
	return err
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
