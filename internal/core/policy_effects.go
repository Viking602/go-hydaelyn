package core

import (
	"context"
	"errors"
	"maps"
	"time"

	"github.com/Viking602/venat/internal/core/model"
	"github.com/Viking602/venat/internal/core/ports"
)

func (r *Runtime) authorizeUoW(ctx context.Context, uow ports.UnitOfWork, request model.PolicyRequest) (model.PolicyDecision, error) {
	if request.RunID == "" {
		request.RunID = requestRunID(request)
	}
	if request.TaskID == "" {
		request.TaskID = requestTaskID(request)
	}
	decision, err := r.currentPolicyEngine().Authorize(ctx, request)
	if err != nil {
		return model.PolicyDecision{}, err
	}
	if decision.Effect == "" {
		decision.Effect = model.PolicyEffectAllow
	}
	if err := uow.Trace().SaveTraceSpan(ctx, model.TraceSpan{ID: r.newID("span"), RunID: request.RunID, TaskID: request.TaskID, Name: "policy.authorize." + string(request.Operation), Component: "policy", Status: model.TraceSpanEnded, StartedAt: time.Now().UTC(), EndedAt: time.Now().UTC()}); err != nil {
		return model.PolicyDecision{}, err
	}
	switch decision.Effect {
	case model.PolicyEffectDeny, model.PolicyEffectAbort:
		return decision, ErrPolicyDenied
	case model.PolicyEffectRequireApproval:
		if err := r.applyPolicyApprovalEffectUoW(ctx, uow, request, decision); err != nil {
			return decision, err
		}
		return decision, commitWithError(ErrPolicyDenied)
	case model.PolicyEffectPause:
		if err := r.applyPolicyPauseEffectUoW(ctx, uow, request, decision); err != nil {
			return decision, err
		}
		return decision, commitWithError(ErrPolicyDenied)
	default:
		return decision, nil
	}
}

func (r *Runtime) applyPolicyApprovalEffectUoW(ctx context.Context, uow ports.UnitOfWork, request model.PolicyRequest, decision model.PolicyDecision) error {
	task, ok, err := policyEffectTaskUoW(ctx, uow, request)
	if err != nil || !ok {
		return err
	}
	reason := firstNonEmpty(decision.Reason, "policy requires approval")
	approval, token := r.newApprovalForTask(task, reason, request.Actor.ID)
	approval.RiskSummary = reason
	approval.RequestedAction = string(request.Operation)
	approval.Metadata = maps.Clone(decision.Metadata)
	token.Metadata = maps.Clone(decision.Metadata)
	if err := uow.Approvals().SaveApproval(ctx, approval); err != nil {
		return err
	}
	if err := uow.ResumeTokens().SaveResumeToken(ctx, token); err != nil {
		return err
	}
	if err := appendResumeTokenCreatedEventUoW(ctx, uow, token); err != nil {
		return err
	}
	if err := pauseTaskForPolicyUoW(ctx, uow, task, reason); err != nil {
		return err
	}
	if err := transitionRunForPolicyUoW(ctx, uow, task.RunID, model.RunStatusWaitingApproval); err != nil {
		return err
	}
	return uow.Events().AppendEvent(ctx, model.Event{RunID: task.RunID, TaskID: task.ID, Type: model.EventApprovalRequested, Payload: map[string]any{"approvalId": approval.ApprovalID, "resumeToken": token.TokenID, "reason": reason, "decisionId": decision.DecisionID, "operation": string(request.Operation)}, RecordedAt: time.Now().UTC()})
}

func (r *Runtime) applyPolicyPauseEffectUoW(ctx context.Context, uow ports.UnitOfWork, request model.PolicyRequest, decision model.PolicyDecision) error {
	task, ok, err := policyEffectTaskUoW(ctx, uow, request)
	if err != nil || !ok {
		return err
	}
	reason := firstNonEmpty(decision.Reason, "policy paused operation")
	if err := pauseTaskForPolicyUoW(ctx, uow, task, reason); err != nil {
		return err
	}
	return transitionRunForPolicyUoW(ctx, uow, task.RunID, model.RunStatusBlocked)
}

func policyEffectTaskUoW(ctx context.Context, uow ports.UnitOfWork, request model.PolicyRequest) (model.Task, bool, error) {
	if request.RunID == "" || request.TaskID == "" {
		return model.Task{}, false, nil
	}
	task, err := uow.Tasks().LoadTask(ctx, request.RunID, request.TaskID)
	if errors.Is(err, ErrNotFound) {
		return model.Task{}, false, nil
	}
	if err != nil {
		return model.Task{}, false, err
	}
	return task, true, nil
}

func pauseTaskForPolicyUoW(ctx context.Context, uow ports.UnitOfWork, task model.Task, reason string) error {
	if isTerminalTask(task.Status) {
		return nil
	}
	paused, err := transitionTaskPure(task, model.TaskStatusPaused, true)
	if err != nil {
		if errors.Is(err, ErrInvalidTransition) || errors.Is(err, ErrTerminalState) {
			return nil
		}
		return err
	}
	if err := uow.Tasks().SaveTask(ctx, paused); err != nil {
		return err
	}
	if lease, ok, err := uow.Leases().ActiveLeaseForTask(ctx, paused.RunID, paused.ID); err != nil {
		return err
	} else if ok {
		lease.Status = model.LeaseStatusReleased
		if err := uow.Leases().SaveLease(ctx, lease); err != nil {
			return err
		}
	}
	return uow.Events().AppendEvent(ctx, model.Event{RunID: paused.RunID, TaskID: paused.ID, Type: model.EventTaskPaused, Payload: map[string]any{"reason": reason, "task": taskEventPayload(paused)}, RecordedAt: time.Now().UTC()})
}

func transitionRunForPolicyUoW(ctx context.Context, uow ports.UnitOfWork, runID string, status model.RunStatus) error {
	run, err := uow.Runs().LoadRun(ctx, runID)
	if errors.Is(err, ErrNotFound) || isTerminalRun(run.Status) {
		return nil
	}
	if err != nil {
		return err
	}
	next, err := transitionRunPure(run, status)
	if err != nil {
		if errors.Is(err, ErrInvalidTransition) || errors.Is(err, ErrTerminalState) {
			return nil
		}
		return err
	}
	if err := uow.Runs().SaveRun(ctx, next); err != nil {
		return err
	}
	return uow.Events().AppendEvent(ctx, model.Event{RunID: next.ID, TaskID: next.RootTaskID, Type: model.EventRunStatusChanged, Payload: map[string]any{"from": string(run.Status), "to": string(next.Status), "run": runPayload(next)}, RecordedAt: time.Now().UTC()})
}

func appendResumeTokenCreatedEventUoW(ctx context.Context, uow ports.UnitOfWork, token model.ResumeToken) error {
	return uow.Events().AppendEvent(ctx, model.Event{RunID: token.RunID, TaskID: token.TaskID, Type: model.EventResumeTokenCreated, Payload: map[string]any{"tokenId": token.TokenID, "approvalId": token.ApprovalID, "expiresAt": token.ExpiresAt}, RecordedAt: time.Now().UTC()})
}
