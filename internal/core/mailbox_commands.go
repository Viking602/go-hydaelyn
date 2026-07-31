package core

import mailboxsvc "github.com/Viking602/venat/internal/mailbox"

type (
	DispatchTaskCommand       = mailboxsvc.DispatchTaskCommand
	FanOutDispatchTaskCommand = mailboxsvc.FanOutDispatchTaskCommand
	AckEnvelopeCommand        = mailboxsvc.AckEnvelopeCommand
	DeadLetterCommand         = mailboxsvc.DeadLetterCommand
)
