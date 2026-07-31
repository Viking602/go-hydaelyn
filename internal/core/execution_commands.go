package core

import executionsvc "github.com/Viking602/venat/internal/execution"

type (
	AcquireTaskExecutionCommand   = executionsvc.AcquireTaskExecutionCommand
	HeartbeatTaskExecutionCommand = executionsvc.HeartbeatTaskExecutionCommand
	ReleaseTaskExecutionCommand   = executionsvc.ReleaseTaskExecutionCommand
	AcquireTaskExecutionResult    = executionsvc.AcquireResult
)
