package core

import (
	"context"

	reportsvc "github.com/Viking602/venat/internal/report"
)

func (r *Runtime) SubmitTypedReport(ctx context.Context, cmd SubmitTypedReportCommand) error {
	_, err := r.ExecuteCommand(ctx, cmd)
	return err
}

func registerReportUoWCommandHandlers(runtime *Runtime) {
	reportsvc.RegisterHandlers(runtime.commandBus, reportsvc.HandlerOptions{
		NewID:           runtime.newID,
		Authorize:       runtime.authorizeUoW,
		NewApproval:     runtime.newApprovalForTask,
		RecordTrace:     runtime.recordEndedTraceUoW,
		MaxHandoffDepth: maxHandoffDepth,
	})
}
