package core

import "context"

func (r *Runtime) executeUoWCommand(ctx context.Context, command RuntimeCommand) (result any, err error) {
	uow, err := r.beginWriteUoW(ctx)
	if err != nil {
		return nil, err
	}
	committed := false
	defer rollbackIfNotCommitted(ctx, uow, &committed, &err)
	result, err = r.commandBus.Execute(ctx, uow, command)
	if err != nil {
		if isCommitCommandError(err) {
			if commitErr := uow.Commit(ctx); commitErr != nil {
				return nil, commitErr
			}
			committed = true
			if r.storeProvider != nil {
				r.notifyUoWCommandSubscribers(command, result)
			}
		}
		return nil, err
	}
	if err := uow.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true
	if r.storeProvider != nil {
		r.notifyUoWCommandSubscribers(command, result)
	}
	return publicUoWCommandResult(command, result), nil
}

func publicUoWCommandResult(command RuntimeCommand, result any) any {
	switch command.(type) {
	case TransitionRunCommand, TransitionTaskCommand, HeartbeatTaskExecutionCommand, ReleaseTaskExecutionCommand,
		AppendTaskExecutionEventCommand, AckEnvelopeCommand, DeadLetterCommand, WriteBlackboardItemCommand,
		SubmitTypedReportCommand, SubmitUserInputCommand, HandoffCommand, SubmitResponseOutputCommand,
		PublishResponseCommand, DecideApprovalCommand, EndTraceSpanCommand:
		return nil
	case AdvanceRunCommand:
		if advanced, ok := result.(advanceRunResult); ok {
			return advanced.Run
		}
	case CompleteActionAttemptCommand:
		if completed, ok := result.(completeActionAttemptResult); ok {
			return completed.Attempt
		}
	case ResolveActionAttemptCommand:
		if resolved, ok := result.(resolveActionAttemptResult); ok {
			return resolved.Attempt
		}
	case ReconcileResponsePublicationCommand:
		if reconciled, ok := result.(reconcileResponsePublicationResult); ok {
			return reconciled.Message
		}
	}
	return result
}
