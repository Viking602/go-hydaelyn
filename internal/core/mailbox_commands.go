package core

import mailboxsvc "github.com/Viking602/go-hydaelyn/internal/mailbox"

type (
	DispatchTaskCommand       = mailboxsvc.DispatchTaskCommand
	FanOutDispatchTaskCommand = mailboxsvc.FanOutDispatchTaskCommand
	AckEnvelopeCommand        = mailboxsvc.AckEnvelopeCommand
	DeadLetterCommand         = mailboxsvc.DeadLetterCommand
)
