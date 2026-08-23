package mailbox

import "github.com/Viking602/venat/api"

type (
	DispatchTaskCommand       = api.DispatchTaskCommand
	FanOutDispatchTaskCommand = api.FanOutDispatchTaskCommand
	AckEnvelopeCommand        = api.AckEnvelopeCommand
	DeadLetterCommand         = api.DeadLetterCommand
)
