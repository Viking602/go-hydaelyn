package core

import "time"

type AcquireTaskExecutionCommand struct {
	RunID      string
	TaskID     string
	EnvelopeID string
	HolderType HolderType
	HolderID   string
	TTL        time.Duration
}

type HeartbeatTaskExecutionCommand struct {
	LeaseID string
	TTL     time.Duration
}

type ReleaseTaskExecutionCommand struct {
	LeaseID  string
	HolderID string
}
