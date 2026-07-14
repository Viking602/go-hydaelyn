package execution

import (
	"time"

	"github.com/Viking602/go-hydaelyn/internal/core/model"
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

func (AcquireTaskExecutionCommand) CommandName() string   { return "task_execution.acquire" }
func (HeartbeatTaskExecutionCommand) CommandName() string { return "task_execution.heartbeat" }
func (ReleaseTaskExecutionCommand) CommandName() string   { return "task_execution.release" }
