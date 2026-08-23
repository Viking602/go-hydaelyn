package execution

import "github.com/Viking602/venat/api"

type (
	AcquireTaskExecutionCommand     = api.AcquireTaskExecutionCommand
	HeartbeatTaskExecutionCommand   = api.HeartbeatTaskExecutionCommand
	ReleaseTaskExecutionCommand     = api.ReleaseTaskExecutionCommand
	AppendTaskExecutionEventCommand = api.AppendTaskExecutionEventCommand
)
