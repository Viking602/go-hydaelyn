package core

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/internal/core/ports"
	"github.com/Viking602/venat/internal/execution"
)

func (r *Runtime) authorizeUoW(ctx context.Context, uow ports.UnitOfWork, request api.PolicyRequest) (api.PolicyDecision, error) {
	if request.RunID == "" {
		request.RunID = requestRunID(request)
	}
	if request.TaskID == "" {
		request.TaskID = requestTaskID(request)
	}
	now := time.Now().UTC()
	decision, engineErr := r.currentPolicyEngine().Authorize(ctx, request)
	if engineErr != nil {
		decision = api.PolicyDecision{
			Effect: api.PolicyEffectDeny,
			Reason: "policy engine failed",
		}
	}
	normalizationErr := normalizePolicyDecision(request, &decision, now)
	if decision.DecisionID == "" {
		decision.DecisionID = r.newID("policy")
	}
	if err := appendPolicyDecisionEventUoW(ctx, uow, request, decision, now); err != nil {
		return api.PolicyDecision{}, err
	}
	if normalizationErr != nil {
		if err := appendPolicyObligationFailure(ctx, uow, request.RunID, request.TaskID, decision, normalizationErr); err != nil {
			return api.PolicyDecision{}, err
		}
	}
	if request.Operation != api.PolicyOperationTraceRead {
		if err := uow.Trace().SaveTraceSpan(ctx, api.TraceSpan{ID: r.newID("span"), RunID: request.RunID, TaskID: request.TaskID, Name: "policy.authorize." + string(request.Operation), Component: "policy", Status: api.TraceSpanEnded, StartedAt: now, EndedAt: now}); err != nil {
			return api.PolicyDecision{}, err
		}
	}
	if engineErr != nil {
		return decision, commitWithError(fmt.Errorf("%w: policy engine: %w", ErrPolicyDenied, engineErr))
	}
	if normalizationErr != nil {
		return decision, commitWithError(normalizationErr)
	}
	switch decision.Effect {
	case api.PolicyEffectDeny, api.PolicyEffectAbort:
		return decision, commitWithError(ErrPolicyDenied)
	case api.PolicyEffectRequireApproval:
		if err := r.applyPolicyApprovalEffectUoW(ctx, uow, request, decision); err != nil {
			return decision, err
		}
		return decision, commitWithError(ErrPolicyDenied)
	case api.PolicyEffectPause:
		if err := r.applyPolicyPauseEffectUoW(ctx, uow, request, decision); err != nil {
			return decision, err
		}
		return decision, commitWithError(ErrPolicyDenied)
	default:
		return decision, nil
	}
}

func normalizePolicyDecision(request api.PolicyRequest, decision *api.PolicyDecision, now time.Time) error {
	// Empty Effect is unknown, not allow: a zero PolicyDecision must fail closed.
	if policyEffectPrecedence(decision.Effect) < 0 {
		decision.Effect = api.PolicyEffectDeny
		decision.Reason = "policy returned an unknown effect"
	}
	if !decision.ExpiresAt.IsZero() && !now.Before(decision.ExpiresAt) {
		decision.Effect = api.PolicyEffectDeny
		decision.Reason = "policy decision expired"
	}
	var obligationErr error
	requireApproval := decision.ApprovalRequired
	for _, obligation := range decision.Obligations {
		if err := validatePolicyObligationForOperation(request.Operation, obligation); err != nil {
			if obligationErr == nil {
				obligationErr = fmt.Errorf("%w: %w", api.ErrPolicyObligationFailed, err)
			}
			decision.Effect = api.PolicyEffectDeny
			decision.Reason = err.Error()
			continue
		}
		if obligation.Kind == api.ObligationRequireHumanApproval {
			requireApproval = true
		}
	}
	if requireApproval && policyEffectPrecedence(decision.Effect) < policyEffectPrecedence(api.PolicyEffectRequireApproval) {
		decision.Effect = api.PolicyEffectRequireApproval
	}
	decision.ApprovalRequired = requireApproval || decision.Effect == api.PolicyEffectRequireApproval
	return obligationErr
}

func validatePolicyObligationForOperation(operation api.PolicyOperation, obligation api.PolicyObligation) error {
	if !knownPolicyObligation(obligation.Kind) {
		return fmt.Errorf("unknown policy obligation %q", obligation.Kind)
	}
	if obligation.Target != "" && !knownPolicyTarget(obligation.Target) {
		return fmt.Errorf("unknown policy obligation target %q", obligation.Target)
	}
	if obligation.Kind == api.ObligationSelectorOnly && obligation.Selector == nil {
		return fmt.Errorf("policy obligation %q requires a selector", obligation.Kind)
	}
	expected := policyTargetForOperation(operation)
	if obligation.Target != "" && obligation.Target != expected {
		return fmt.Errorf("policy obligation target %q does not match operation %q", obligation.Target, operation)
	}
	if expected == "" && obligation.Kind != api.ObligationRequireHumanApproval {
		return fmt.Errorf("policy obligation %q is not supported for operation %q", obligation.Kind, operation)
	}
	return nil
}

func policyTargetForOperation(operation api.PolicyOperation) api.PolicyObligationTarget {
	switch operation {
	case api.PolicyOperationBlackboardRead:
		return api.PolicyTargetBlackboardRead
	case api.PolicyOperationBlackboardWrite:
		return api.PolicyTargetBlackboardWrite
	case api.PolicyOperationToolCall:
		return api.PolicyTargetToolResult
	case api.PolicyOperationHandoff:
		return api.PolicyTargetHandoff
	case api.PolicyOperationResponseCompose, api.PolicyOperationResponsePublish:
		return api.PolicyTargetResponse
	case api.PolicyOperationTraceRead:
		return api.PolicyTargetTrace
	default:
		return ""
	}
}

func policyEffectPrecedence(effect api.PolicyEffect) int {
	switch effect {
	case api.PolicyEffectAllow:
		return 0
	case api.PolicyEffectPause:
		return 1
	case api.PolicyEffectRequireApproval:
		return 2
	case api.PolicyEffectDeny:
		return 3
	case api.PolicyEffectAbort:
		return 4
	default:
		return -1
	}
}

func appendPolicyDecisionEventUoW(
	ctx context.Context,
	uow ports.UnitOfWork,
	request api.PolicyRequest,
	decision api.PolicyDecision,
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
	return uow.Events().AppendEvent(ctx, api.Event{
		RunID:      request.RunID,
		TaskID:     request.TaskID,
		Type:       api.EventPolicyDecisionRecorded,
		Payload:    payload,
		RecordedAt: recordedAt,
	})
}

func policyObligationEventPayload(obligation api.PolicyObligation) map[string]any {
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

func cloneDeprecatedPolicySelectorAgentIDs(selector api.BlackboardSelector) []string {
	//lint:ignore SA1019 SourceAgentIDs remains part of the durable audit shape for backward compatibility.
	return slices.Clone(selector.SourceAgentIDs)
}

func (r *Runtime) applyPolicyApprovalEffectUoW(ctx context.Context, uow ports.UnitOfWork, request api.PolicyRequest, decision api.PolicyDecision) error {
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
	if err := transitionRunForPolicyUoW(ctx, uow, task.RunID, api.RunStatusWaitingApproval); err != nil {
		return err
	}
	return uow.Events().AppendEvent(ctx, api.Event{RunID: task.RunID, TaskID: task.ID, Type: api.EventApprovalRequested, Payload: map[string]any{"approvalId": approval.ApprovalID, "resumeToken": token.TokenID, "reason": reason, "decisionId": decision.DecisionID, "operation": string(request.Operation)}, RecordedAt: time.Now().UTC()})
}

func (r *Runtime) applyPolicyPauseEffectUoW(ctx context.Context, uow ports.UnitOfWork, request api.PolicyRequest, decision api.PolicyDecision) error {
	task, ok, err := policyEffectTaskUoW(ctx, uow, request)
	if err != nil || !ok {
		return err
	}
	reason := firstNonEmpty(decision.Reason, "policy paused operation")
	if err := pauseTaskForPolicyUoW(ctx, uow, task, reason); err != nil {
		return err
	}
	return transitionRunForPolicyUoW(ctx, uow, task.RunID, api.RunStatusBlocked)
}

func policyEffectTaskUoW(ctx context.Context, uow ports.UnitOfWork, request api.PolicyRequest) (api.Task, bool, error) {
	if request.RunID == "" || request.TaskID == "" {
		return api.Task{}, false, nil
	}
	task, err := uow.Tasks().LoadTask(ctx, request.RunID, request.TaskID)
	if errors.Is(err, ErrNotFound) {
		return api.Task{}, false, nil
	}
	if err != nil {
		return api.Task{}, false, err
	}
	return task, true, nil
}

func pauseTaskForPolicyUoW(ctx context.Context, uow ports.UnitOfWork, task api.Task, reason string) error {
	if isTerminalTask(task.Status) {
		return nil
	}
	paused, err := transitionTaskPure(task, api.TaskStatusPaused, true)
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
		lease.Status = api.LeaseStatusReleased
		if err := uow.Leases().SaveLease(ctx, lease); err != nil {
			return err
		}
		if err := execution.ReleaseResourceClaims(ctx, uow, lease.ID, time.Now().UTC()); err != nil {
			return err
		}
	}
	return uow.Events().AppendEvent(ctx, api.Event{RunID: paused.RunID, TaskID: paused.ID, Type: api.EventTaskPaused, Payload: map[string]any{"reason": reason, "task": taskEventPayload(paused)}, RecordedAt: time.Now().UTC()})
}

func transitionRunForPolicyUoW(ctx context.Context, uow ports.UnitOfWork, runID string, status api.RunStatus) error {
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
	return uow.Events().AppendEvent(ctx, api.Event{RunID: next.ID, TaskID: next.RootTaskID, Type: api.EventRunStatusChanged, Payload: map[string]any{"from": string(run.Status), "to": string(next.Status), "run": runPayload(next)}, RecordedAt: time.Now().UTC()})
}

func appendResumeTokenCreatedEventUoW(ctx context.Context, uow ports.UnitOfWork, token api.ResumeToken) error {
	return uow.Events().AppendEvent(ctx, api.Event{RunID: token.RunID, TaskID: token.TaskID, Type: api.EventResumeTokenCreated, Payload: map[string]any{"tokenId": token.TokenID, "approvalId": token.ApprovalID, "expiresAt": token.ExpiresAt}, RecordedAt: time.Now().UTC()})
}
