package execution

import (
	"time"

	"github.com/Viking602/venat/internal/core/model"
)

type AcquireTaskExecutionCommand struct {
	RunID      string
	TaskID     string
	EnvelopeID string
	HolderType model.HolderType
	HolderID   string
	TTL        time.Duration
}

type HeartbeatTaskExecutionCommand struct {
	LeaseID  string
	HolderID string
	TTL      time.Duration
}

type ReleaseTaskExecutionCommand struct {
	LeaseID  string
	HolderID string
}

type AppendTaskExecutionEventCommand struct {
	RunID       string
	TaskID      string
	LeaseID     string
	HolderType  model.HolderType
	HolderID    string
	TaskVersion int
	Event       model.Event
}

func (AcquireTaskExecutionCommand) CommandName() string   { return "task_execution.acquire" }
func (HeartbeatTaskExecutionCommand) CommandName() string { return "task_execution.heartbeat" }
func (ReleaseTaskExecutionCommand) CommandName() string   { return "task_execution.release" }
func (AppendTaskExecutionEventCommand) CommandName() string {
	return "task_execution.event.append"
}
