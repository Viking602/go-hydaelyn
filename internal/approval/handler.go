package approval

import (
	"context"
	"maps"
	"slices"
	"time"

	commandbus "github.com/Viking602/venat/internal/command"
	"github.com/Viking602/venat/internal/core/model"
	"github.com/Viking602/venat/internal/core/ports"
	corestate "github.com/Viking602/venat/internal/core/state"
	"github.com/Viking602/venat/internal/eventpayload"
	"github.com/Viking602/venat/internal/lifecycle"
)

type Factory func(model.Task, string, string) (model.ApprovalRequest, model.ResumeToken)

type IDGenerator func(string) string

type HandlerOptions struct {
	NewApproval Factory
	NewID       IDGenerator
}

// RequestApprovalResult is the typed result returned by RequestApprovalCommand.
// Replacing the previous []any tuple keeps multi-value returns type-safe
// across the command-bus boundary.
type RequestApprovalResult struct {
	Approval model.ApprovalRequest
	Token    model.ResumeToken
}

func RegisterHandlers(bus *commandbus.Bus, options HandlerOptions) {
	commandbus.Register[RequestApprovalCommand](bus, requestApprovalHandler{options: options})
	commandbus.Register[DecideApprovalCommand](bus, decideApprovalHandler{options: options})
	commandbus.Register[RecoverResumeTokenCommand](bus, recoverResumeTokenHandler{})
}

type decideApprovalResult struct {
	Approval      model.ApprovalRequest
	Task          model.Task
	Run           model.Run
	Envelope      model.TaskEnvelope
	TaskResumed   bool
	RunTransition bool
}

type requestApprovalHandler struct{ options HandlerOptions }

func (requestApprovalHandler) Name() string { return RequestApprovalCommand{}.CommandName() }

func (h requestApprovalHandler) Handle(ctx context.Context, uow ports.UnitOfWork, cmd RequestApprovalCommand) (any, error) {
	task, err := uow.Tasks().LoadTask(ctx, cmd.RunID, cmd.TaskID)
	if err != nil {
		return nil, err
	}
	approvalFactory := h.options.NewApproval
	if approvalFactory == nil {
		approvalFactory = func(task model.Task, reason, requester string) (model.ApprovalRequest, model.ResumeToken) {
			return lifecycle.NewApprovalPair(func(prefix string) string { return prefix }, task, reason, requester)
		}
	}
	approval, token := approvalFactory(task, cmd.Reason, cmd.RequesterAgentID)
	approval.ActionID = cmd.ActionID
	approval.RiskSummary = cmd.RiskSummary
	approval.RequestedAction = cmd.RequestedAction
	approval.Metadata = maps.Clone(cmd.Metadata)
	if err := uow.Approvals().SaveApproval(ctx, approval); err != nil {
		return nil, err
	}
	if err := uow.ResumeTokens().SaveResumeToken(ctx, token); err != nil {
		return nil, err
	}
	if err := appendResumeTokenCreatedEvent(ctx, uow, token); err != nil {
		return nil, err
	}
	if err := uow.Events().AppendEvent(ctx, model.Event{RunID: cmd.RunID, TaskID: cmd.TaskID, Type: model.EventApprovalRequested, Payload: map[string]any{"approvalId": approval.ApprovalID, "resumeToken": token.TokenID, "reason": approval.Reason}, RecordedAt: time.Now().UTC()}); err != nil {
		return nil, err
	}
	return RequestApprovalResult{Approval: approval, Token: token}, nil
}

type decideApprovalHandler struct{ options HandlerOptions }

func (decideApprovalHandler) Name() string { return DecideApprovalCommand{}.CommandName() }

func (h decideApprovalHandler) Handle(ctx context.Context, uow ports.UnitOfWork, cmd DecideApprovalCommand) (any, error) {
	approval, err := uow.Approvals().LoadApproval(ctx, cmd.ApprovalID)
	if err != nil {
		return nil, err
	}
	if approval.RunID != cmd.RunID {
		return nil, model.ErrNotFound
	}
	approval.Status = cmd.Decision
	if err := uow.Approvals().SaveApproval(ctx, approval); err != nil {
		return nil, err
	}
	if err := uow.Events().AppendEvent(ctx, model.Event{RunID: approval.RunID, TaskID: approval.TaskID, Type: model.EventApprovalDecided, Payload: map[string]any{"approvalId": approval.ApprovalID, "decidedBy": cmd.DecidedBy, "decision": cmd.Decision, "reason": cmd.Reason}, RecordedAt: time.Now().UTC()}); err != nil {
		return nil, err
	}
	result := decideApprovalResult{Approval: approval}
	if cmd.Decision != "approved" {
		return result, nil
	}
	if task, err := uow.Tasks().LoadTask(ctx, approval.RunID, approval.TaskID); err == nil && task.Status == model.TaskStatusPaused {
		nextTask, err := corestate.TransitionTask(task, model.TaskStatusDispatched, true)
		if err != nil {
			return nil, err
		}
		if err := uow.Tasks().SaveTask(ctx, nextTask); err != nil {
			return nil, err
		}
		envelopeID := "env-" + approval.ApprovalID
		if h.options.NewID != nil {
			envelopeID = h.options.NewID("env")
		}
		envelope := model.TaskEnvelope{
			ID:              envelopeID,
			RunID:           nextTask.RunID,
			TaskID:          nextTask.ID,
			TargetAgentID:   nextTask.OwnerAgentID,
			TargetComponent: nextTask.OwnerComponent,
			Type:            "TaskEnvelope",
			Status:          "pending",
			TaskVersion:     nextTask.Version,
			ReadSelectors:   slices.Clone(nextTask.ReadSelectors),
			WriteTargets:    slices.Clone(nextTask.WriteTargets),
			RetryPolicy:     nextTask.RetryPolicy,
			CreatedAt:       time.Now().UTC(),
		}
		if err := uow.MailboxOutbox().QueueEnvelope(ctx, envelope); err != nil {
			return nil, err
		}
		if err := uow.Events().AppendEvent(ctx, model.Event{
			RunID: envelope.RunID, TaskID: envelope.TaskID, Type: model.EventTaskDispatched,
			Payload:    map[string]any{"reason": "approval_resolved", "task": eventpayload.Task(nextTask), "envelope": eventpayload.Envelope(envelope)},
			RecordedAt: time.Now().UTC(),
		}); err != nil {
			return nil, err
		}
		result.Envelope = envelope
		result.Task = nextTask
		result.TaskResumed = true
	} else if err != nil {
		return nil, err
	}
	if run, err := uow.Runs().LoadRun(ctx, approval.RunID); err == nil && run.Status == model.RunStatusWaitingApproval {
		nextRun, err := corestate.TransitionRun(run, model.RunStatusRunning)
		if err != nil {
			return nil, err
		}
		if err := uow.Runs().SaveRun(ctx, nextRun); err != nil {
			return nil, err
		}
		if err := uow.Events().AppendEvent(ctx, model.Event{RunID: nextRun.ID, TaskID: nextRun.RootTaskID, Type: model.EventRunStatusChanged, Payload: map[string]any{"from": string(run.Status), "to": string(nextRun.Status), "run": runPayload(nextRun)}, RecordedAt: time.Now().UTC()}); err != nil {
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

func (recoverResumeTokenHandler) Handle(ctx context.Context, uow ports.UnitOfWork, cmd RecoverResumeTokenCommand) (any, error) {
	token, err := uow.ResumeTokens().LoadResumeToken(ctx, cmd.TokenID)
	if err != nil {
		return nil, err
	}
	if !token.ExpiresAt.IsZero() && token.ExpiresAt.Before(time.Now().UTC()) {
		return nil, model.ErrInvalidCommand
	}
	return token, nil
}

func appendResumeTokenCreatedEvent(ctx context.Context, uow ports.UnitOfWork, token model.ResumeToken) error {
	return uow.Events().AppendEvent(ctx, model.Event{RunID: token.RunID, TaskID: token.TaskID, Type: model.EventResumeTokenCreated, Payload: map[string]any{"tokenId": token.TokenID, "approvalId": token.ApprovalID, "expiresAt": token.ExpiresAt}, RecordedAt: time.Now().UTC()})
}

func runPayload(run model.Run) map[string]any {
	return map[string]any{"runId": run.ID, "rootTaskId": run.RootTaskID, "status": string(run.Status), "request": run.Request, "createdAt": run.CreatedAt, "updatedAt": run.UpdatedAt}
}
