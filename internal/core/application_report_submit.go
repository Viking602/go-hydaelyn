package core

import "context"

func (r *Runtime) SubmitTypedReport(ctx context.Context, cmd SubmitTypedReportCommand) error {
	_, err := r.ExecuteCommand(ctx, cmd)
	return err
}
