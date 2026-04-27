package runtime

import (
	"context"
	"errors"
	"maps"
)

type allowPolicyEngine struct{}

func (allowPolicyEngine) Authorize(context.Context, PolicyRequest) (PolicyDecision, error) {
	return PolicyDecision{Effect: PolicyEffectAllow}, nil
}

type messagePolicyAdapter struct {
	check MessagePolicyChecker
}

func (p messagePolicyAdapter) Authorize(_ context.Context, request PolicyRequest) (PolicyDecision, error) {
	if p.check == nil || request.Message == nil {
		return PolicyDecision{Effect: PolicyEffectAllow}, nil
	}
	decision := p.check(*request.Message)
	if decision.Effect == "" {
		decision.Effect = PolicyEffectAllow
	}
	return decision, nil
}

func (r *Runtime) authorizeLocked(ctx context.Context, request PolicyRequest) (PolicyDecision, error) {
	if request.RunID == "" {
		request.RunID = requestRunID(request)
	}
	if request.TaskID == "" {
		request.TaskID = requestTaskID(request)
	}
	engine := r.policy
	if engine == nil {
		engine = allowPolicyEngine{}
	}
	decision, err := engine.Authorize(ctx, request)
	if err != nil {
		return PolicyDecision{}, err
	}
	if decision.Effect == "" {
		decision.Effect = PolicyEffectAllow
	}
	r.recordTraceLocked(request.RunID, request.TaskID, "policy.authorize."+string(request.Operation), "policy")
	switch decision.Effect {
	case PolicyEffectDeny, PolicyEffectAbort:
		return decision, ErrPolicyDenied
	case PolicyEffectRequireApproval:
		if err := r.applyPolicyApprovalEffectLocked(request, decision); err != nil {
			return decision, err
		}
		return decision, ErrPolicyDenied
	case PolicyEffectPause:
		if err := r.applyPolicyPauseEffectLocked(request, decision); err != nil {
			return decision, err
		}
		return decision, ErrPolicyDenied
	}
	return decision, nil
}

func (r *Runtime) applyPolicyApprovalEffectLocked(request PolicyRequest, decision PolicyDecision) error {
	task, ok := r.policyEffectTaskLocked(request)
	if !ok {
		return nil
	}
	reason := firstNonEmpty(decision.Reason, "policy requires approval")
	approval, token := r.createApprovalLocked(task, reason, request.Actor.ID)
	approval.RiskSummary = reason
	approval.RequestedAction = string(request.Operation)
	approval.Metadata = maps.Clone(decision.Metadata)
	r.approvals[approval.ApprovalID] = approval
	r.appendEventLocked(task.RunID, task.ID, EventApprovalRequested, map[string]any{
		"approvalId":  approval.ApprovalID,
		"resumeToken": token.TokenID,
		"reason":      reason,
		"decisionId":  decision.DecisionID,
		"operation":   string(request.Operation),
	})
	if err := r.pauseTaskForPolicyLocked(task, reason); err != nil {
		return err
	}
	return r.transitionRunForPolicyLocked(task.RunID, RunStatusWaitingApproval)
}

func (r *Runtime) applyPolicyPauseEffectLocked(request PolicyRequest, decision PolicyDecision) error {
	task, ok := r.policyEffectTaskLocked(request)
	if !ok {
		return nil
	}
	reason := firstNonEmpty(decision.Reason, "policy paused operation")
	if err := r.pauseTaskForPolicyLocked(task, reason); err != nil {
		return err
	}
	return r.transitionRunForPolicyLocked(task.RunID, RunStatusBlocked)
}

func (r *Runtime) policyEffectTaskLocked(request PolicyRequest) (Task, bool) {
	runID := request.RunID
	taskID := request.TaskID
	if runID == "" || taskID == "" {
		return Task{}, false
	}
	task, ok := r.tasks[runID][taskID]
	return task, ok
}

func (r *Runtime) pauseTaskForPolicyLocked(task Task, reason string) error {
	if isTerminalTask(task.Status) {
		return nil
	}
	paused, err := r.transitionTaskLocked(task, TaskStatusPaused)
	if err != nil {
		if errors.Is(err, ErrInvalidTransition) || errors.Is(err, ErrTerminalState) {
			return nil
		}
		return err
	}
	r.releaseActiveLeaseLocked(paused.RunID, paused.ID)
	r.appendEventLocked(paused.RunID, paused.ID, EventTaskPaused, map[string]any{
		"reason": reason,
		"task":   taskEventPayload(paused),
	})
	return nil
}

func (r *Runtime) transitionRunForPolicyLocked(runID string, status RunStatus) error {
	run, ok := r.runs[runID]
	if !ok || isTerminalRun(run.Status) {
		return nil
	}
	if _, err := r.transitionRunLocked(run, status); err != nil {
		if errors.Is(err, ErrInvalidTransition) || errors.Is(err, ErrTerminalState) {
			return nil
		}
		return err
	}
	return nil
}

func requestRunID(request PolicyRequest) string {
	if request.Message != nil {
		return request.Message.RunID
	}
	if request.Handoff != nil {
		return request.Handoff.RunID
	}
	if request.Item != nil {
		return request.Item.RunID
	}
	if request.Action != nil {
		return request.Action.RunID
	}
	return ""
}

func requestTaskID(request PolicyRequest) string {
	if request.Message != nil {
		return request.Message.TaskID
	}
	if request.Handoff != nil {
		return request.Handoff.TaskID
	}
	if request.Item != nil {
		return request.Item.TaskID
	}
	if request.Action != nil {
		return request.Action.TaskID
	}
	return ""
}

func actorFromHolder(holderType HolderType, holderID string) SourceIdentity {
	switch holderType {
	case HolderAgent:
		return SourceIdentity{Type: SourceAgent, ID: holderID}
	case HolderComponent:
		return SourceIdentity{Type: SourceComponent, ID: holderID}
	default:
		return SourceIdentity{}
	}
}
