package core

import (
	"context"
	"time"

	commandbus "github.com/Viking602/go-hydaelyn/internal/core/command"
	"github.com/Viking602/go-hydaelyn/internal/core/ports"
)

func registerApprovalUoWCommandHandlers(runtime *Runtime) {
	commandbus.Register[RequestApprovalCommand](runtime.commandBus, requestApprovalHandler{runtime: runtime})
	commandbus.Register[DecideApprovalCommand](runtime.commandBus, decideApprovalHandler{})
	commandbus.Register[RecoverResumeTokenCommand](runtime.commandBus, recoverResumeTokenHandler{})
}

type decideApprovalResult struct {
	Approval      ApprovalRequest
	Task          Task
	Run           Run
	TaskResumed   bool
	RunTransition bool
}

type requestApprovalHandler struct{ runtime *Runtime }

func (requestApprovalHandler) Name() string { return RequestApprovalCommand{}.CommandName() }

func (h requestApprovalHandler) Handle(ctx context.Context, uow ports.FullUnitOfWork, cmd RequestApprovalCommand) (any, error) {
	task, err := uow.Tasks().LoadTask(ctx, cmd.RunID, cmd.TaskID)
	if err != nil {
		return nil, err
	}
	approval, token := h.runtime.newApprovalForTask(task, cmd.Reason, cmd.RequesterAgentID)
	approval.ActionID = cmd.ActionID
	approval.RiskSummary = cmd.RiskSummary
	approval.RequestedAction = cmd.RequestedAction
	if err := uow.Approvals().SaveApproval(ctx, approval); err != nil {
		return nil, err
	}
	if err := uow.ResumeTokens().SaveResumeToken(ctx, token); err != nil {
		return nil, err
	}
	if err := appendResumeTokenCreatedEventUoW(ctx, uow, token); err != nil {
		return nil, err
	}
	if err := uow.Events().AppendEvent(ctx, Event{RunID: cmd.RunID, TaskID: cmd.TaskID, Type: EventApprovalRequested, Payload: map[string]any{"approvalId": approval.ApprovalID, "resumeToken": token.TokenID, "reason": approval.Reason}, RecordedAt: time.Now().UTC()}); err != nil {
		return nil, err
	}
	return []any{approval, token}, nil
}

type decideApprovalHandler struct{}

func (decideApprovalHandler) Name() string { return DecideApprovalCommand{}.CommandName() }

func (decideApprovalHandler) Handle(ctx context.Context, uow ports.FullUnitOfWork, cmd DecideApprovalCommand) (any, error) {
	approval, err := uow.Approvals().LoadApproval(ctx, cmd.ApprovalID)
	if err != nil {
		return nil, err
	}
	if approval.RunID != cmd.RunID {
		return nil, ErrNotFound
	}
	approval.Status = cmd.Decision
	if err := uow.Approvals().SaveApproval(ctx, approval); err != nil {
		return nil, err
	}
	if err := uow.Events().AppendEvent(ctx, Event{RunID: approval.RunID, TaskID: approval.TaskID, Type: EventApprovalDecided, Payload: map[string]any{"approvalId": approval.ApprovalID, "decidedBy": cmd.DecidedBy, "decision": cmd.Decision, "reason": cmd.Reason}, RecordedAt: time.Now().UTC()}); err != nil {
		return nil, err
	}
	result := decideApprovalResult{Approval: approval}
	if cmd.Decision != "approved" {
		return result, nil
	}
	if task, err := uow.Tasks().LoadTask(ctx, approval.RunID, approval.TaskID); err == nil && task.Status == TaskStatusPaused {
		nextTask, err := transitionTaskPure(task, TaskStatusDispatched, true)
		if err != nil {
			return nil, err
		}
		if err := uow.Tasks().SaveTask(ctx, nextTask); err != nil {
			return nil, err
		}
		result.Task = nextTask
		result.TaskResumed = true
	} else if err != nil {
		return nil, err
	}
	if run, err := uow.Runs().LoadRun(ctx, approval.RunID); err == nil && run.Status == RunStatusWaitingApproval {
		nextRun, err := transitionRunPure(run, RunStatusRunning)
		if err != nil {
			return nil, err
		}
		if err := uow.Runs().SaveRun(ctx, nextRun); err != nil {
			return nil, err
		}
		if err := uow.Events().AppendEvent(ctx, Event{RunID: nextRun.ID, TaskID: nextRun.RootTaskID, Type: EventRunStatusChanged, Payload: map[string]any{"from": string(run.Status), "to": string(nextRun.Status), "run": runPayload(nextRun)}, RecordedAt: time.Now().UTC()}); err != nil {
			return nil, err
		}
		result.Run = nextRun
		result.RunTransition = true
	} else if err != nil {
		return nil, err
	}
	return result, nil
}

type recoverResumeTokenHandler struct{}

func (recoverResumeTokenHandler) Name() string { return RecoverResumeTokenCommand{}.CommandName() }

func (recoverResumeTokenHandler) Handle(ctx context.Context, uow ports.FullUnitOfWork, cmd RecoverResumeTokenCommand) (any, error) {
	token, err := uow.ResumeTokens().LoadResumeToken(ctx, cmd.TokenID)
	if err != nil {
		return nil, err
	}
	if !token.ExpiresAt.IsZero() && token.ExpiresAt.Before(time.Now().UTC()) {
		return nil, ErrInvalidCommand
	}
	return token, nil
}

func appendResumeTokenCreatedEventUoW(ctx context.Context, uow ports.FullUnitOfWork, token ResumeToken) error {
	return uow.Events().AppendEvent(ctx, Event{RunID: token.RunID, TaskID: token.TaskID, Type: EventResumeTokenCreated, Payload: map[string]any{"tokenId": token.TokenID, "approvalId": token.ApprovalID, "expiresAt": token.ExpiresAt}, RecordedAt: time.Now().UTC()})
}
