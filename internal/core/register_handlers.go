package core

import runsvc "github.com/Viking602/venat/internal/run"

func (r *Runtime) registerUoWCommandHandlers() {
	runsvc.RegisterHandlers(r.commandBus, r.newID)
	registerStateUoWCommandHandlers(r)
	registerAdvanceRunUoWCommandHandlers(r)
	registerBlackboardUoWCommandHandlers(r)
	registerMailboxUoWCommandHandlers(r)
	registerMailboxDispatchUoWCommandHandlers(r)
	registerDeadLetterUoWCommandHandlers(r)
	registerExecutionUoWCommandHandlers(r)
	registerResponseUoWCommandHandlers(r)
	registerReportUoWCommandHandlers(r)
	registerUserInputUoWCommandHandlers(r)
	registerActionUoWCommandHandlers(r)
	registerApprovalUoWCommandHandlers(r)
	registerHandoffUoWCommandHandlers(r)
	registerToolUoWCommandHandlers(r)
	registerTraceUoWCommandHandlers(r)
}
