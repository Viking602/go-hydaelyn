package core

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/Viking602/venat/internal/core/model"
	"github.com/Viking602/venat/internal/core/ports"
	"github.com/Viking602/venat/internal/execution"
)

func (r *Runtime) authorizeUoW(ctx context.Context, uow ports.UnitOfWork, request model.PolicyRequest) (model.PolicyDecision, error) {
	if request.RunID == "" {
		request.RunID = requestRunID(request)
	}
	if request.TaskID == "" {
		request.TaskID = requestTaskID(request)
	}
	now := time.Now().UTC()
	decision, engineErr := r.currentPolicyEngine().Authorize(ctx, request)
	if engineErr != nil {
		decision = model.PolicyDecision{
			Effect: model.PolicyEffectDeny,
			Reason: "policy engine failed",
		}
	}
	normalizationErr := normalizePolicyDecision(request, &decision, now)
	if decision.DecisionID == "" {
		decision.DecisionID = r.newID("policy")
	}
	if err := appendPolicyDecisionEventUoW(ctx, uow, request, decision, now); err != nil {
		return model.PolicyDecision{}, err
	}
	if normalizationErr != nil {
		if err := appendPolicyObligationFailure(ctx, uow, request.RunID, request.TaskID, decision, normalizationErr); err != nil {
			return model.PolicyDecision{}, err
		}
	}
	if request.Operation != model.PolicyOperationTraceRead {
		if err := uow.Trace().SaveTraceSpan(ctx, model.TraceSpan{ID: r.newID("span"), RunID: request.RunID, TaskID: request.TaskID, Name: "policy.authorize." + string(request.Operation), Component: "policy", Status: model.TraceSpanEnded, StartedAt: now, EndedAt: now}); err != nil {
			return model.PolicyDecision{}, err
		}
	}
	if engineErr != nil {
		return decision, commitWithError(fmt.Errorf("%w: policy engine: %w", ErrPolicyDenied, engineErr))
	}
	if normalizationErr != nil {
		return decision, commitWithError(normalizationErr)
	}
	switch decision.Effect {
	case model.PolicyEffectDeny, model.PolicyEffectAbort:
		return decision, commitWithError(ErrPolicyDenied)
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

func normalizePolicyDecision(request model.PolicyRequest, decision *model.PolicyDecision, now time.Time) error {
	// Empty Effect is unknown, not allow: a zero PolicyDecision must fail closed.
	if policyEffectPrecedence(decision.Effect) < 0 {
		decision.Effect = model.PolicyEffectDeny
		decision.Reason = "policy returned an unknown effect"
	}
	if !decision.ExpiresAt.IsZero() && !now.Before(decision.ExpiresAt) {
		decision.Effect = model.PolicyEffectDeny
		decision.Reason = "policy decision expired"
	}
	var obligationErr error
	requireApproval := decision.ApprovalRequired
	for _, obligation := range decision.Obligations {
		if err := validatePolicyObligationForOperation(request.Operation, obligation); err != nil {
			if obligationErr == nil {
				obligationErr = fmt.Errorf("%w: %w", model.ErrPolicyObligationFailed, err)
			}
			decision.Effect = model.PolicyEffectDeny
			decision.Reason = err.Error()
			continue
		}
		if obligation.Kind == model.ObligationRequireHumanApproval {
			requireApproval = true
		}
	}
	if requireApproval && policyEffectPrecedence(decision.Effect) < policyEffectPrecedence(model.PolicyEffectRequireApproval) {
		decision.Effect = model.PolicyEffectRequireApproval
	}
	decision.ApprovalRequired = requireApproval || decision.Effect == model.PolicyEffectRequireApproval
	return obligationErr
}

func validatePolicyObligationForOperation(operation model.PolicyOperation, obligation model.PolicyObligation) error {
	if !knownPolicyObligation(obligation.Kind) {
		return fmt.Errorf("unknown policy obligation %q", obligation.Kind)
	}
	if obligation.Target != "" && !knownPolicyTarget(obligation.Target) {
		return fmt.Errorf("unknown policy obligation target %q", obligation.Target)
	}
	if obligation.Kind == model.ObligationSelectorOnly && obligation.Selector == nil {
		return fmt.Errorf("policy obligation %q requires a selector", obligation.Kind)
	}
	expected := policyTargetForOperation(operation)
	if obligation.Target != "" && obligation.Target != expected {
		return fmt.Errorf("policy obligation target %q does not match operation %q", obligation.Target, operation)
	}
	if expected == "" && obligation.Kind != model.ObligationRequireHumanApproval {
		return fmt.Errorf("policy obligation %q is not supported for operation %q", obligation.Kind, operation)
	}
	return nil
}

func policyTargetForOperation(operation model.PolicyOperation) model.PolicyObligationTarget {
	switch operation {
	case model.PolicyOperationBlackboardRead:
		return model.PolicyTargetBlackboardRead
	case model.PolicyOperationBlackboardWrite:
		return model.PolicyTargetBlackboardWrite
	case model.PolicyOperationToolCall:
		return model.PolicyTargetToolResult
	case model.PolicyOperationHandoff:
		return model.PolicyTargetHandoff
	case model.PolicyOperationResponseCompose, model.PolicyOperationResponsePublish:
		return model.PolicyTargetResponse
	case model.PolicyOperationTraceRead:
		return model.PolicyTargetTrace
	default:
		return ""
	}
}

func policyEffectPrecedence(effect model.PolicyEffect) int {
	switch effect {
	case model.PolicyEffectAllow:
		return 0
	case model.PolicyEffectPause:
		return 1
	case model.PolicyEffectRequireApproval:
		return 2
	case model.PolicyEffectDeny:
		return 3
	case model.PolicyEffectAbort:
		return 4
	default:
		return -1
	}
}

func appendPolicyDecisionEventUoW(
	ctx context.Context,
	uow ports.UnitOfWork,
	request model.PolicyRequest,
	decision model.PolicyDecision,
	recordedAt time.Time,
) error {
	obligations := make([]map[string]any, 0, len(decision.Obligations))
	for _, obligation := range decision.Obligations {
		obligations = append(obligations, policyObligationEventPayload(obligation))
	}
	payload := map[string]any{
		"decisionId":       decision.DecisionID,
		"operation":        string(request.Operation),
		"effect":           string(decision.Effect),
		"reason":           decision.Reason,
		"approvalRequired": decision.ApprovalRequired,
		"obligations":      obligations,
		"redactions":       append([]string(nil), decision.Redactions...),
		"actorType":        string(request.Actor.Type),
		"actorId":          request.Actor.ID,
	}
	if request.Tool != nil {
		payload["toolName"] = request.Tool.Name
	}
	if len(request.Metadata) > 0 {
		payload["requestMetadata"] = maps.Clone(request.Metadata)
	}
	if !decision.ExpiresAt.IsZero() {
		payload["expiresAt"] = decision.ExpiresAt
	}
	return uow.Events().AppendEvent(ctx, model.Event{
		RunID:      request.RunID,
		TaskID:     request.TaskID,
		Type:       model.EventPolicyDecisionRecorded,
		Payload:    payload,
		RecordedAt: recordedAt,
	})
}

func policyObligationEventPayload(obligation model.PolicyObligation) map[string]any {
	payload := map[string]any{
		"kind":   string(obligation.Kind),
		"target": string(obligation.Target),
	}
	if obligation.Selector == nil {
		return payload
	}
	selector := *obligation.Selector
	selector.ItemTypes = slices.Clone(selector.ItemTypes)
	selector.SourceTypes = slices.Clone(selector.SourceTypes)
	selector.SourceIDs = slices.Clone(selector.SourceIDs)
	//lint:ignore SA1019 SourceAgentIDs remains part of the durable audit shape for backward compatibility.
	selector.SourceAgentIDs = cloneDeprecatedPolicySelectorAgentIDs(selector)
	selector.Tags = slices.Clone(selector.Tags)
	selector.Keys = slices.Clone(selector.Keys)
	payload["selector"] = selector
	return payload
}

func cloneDeprecatedPolicySelectorAgentIDs(selector model.BlackboardSelector) []string {
	//lint:ignore SA1019 SourceAgentIDs remains part of the durable audit shape for backward compatibility.
	return slices.Clone(selector.SourceAgentIDs)
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
		if err := execution.ReleaseResourceClaims(ctx, uow, lease.ID, time.Now().UTC()); err != nil {
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
