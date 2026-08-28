package venat

import (
	"context"
	"encoding/json"
	"maps"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/internal/core"
)

func (r *Runner) ActiveLeaseCountContext(ctx context.Context, runID, taskID string) (int, error) {
	count, err := r.rt.ActiveLeaseCount(ctx, runID, taskID)
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (r *Runner) AcquireTaskExecution(ctx context.Context, cmd api.AcquireTaskExecutionCommand) (api.TaskExecutionLease, bool, error) {
	result, err := r.AcquireTaskExecutionWithClaims(ctx, cmd)
	return result.Lease, result.Acquired, err
}

// AcquireTaskExecutionWithClaims atomically acquires a task lease and every
// requested shared/exclusive resource claim.
func (r *Runner) AcquireTaskExecutionWithClaims(ctx context.Context, cmd api.AcquireTaskExecutionCommand) (api.AcquireTaskExecutionResult, error) {
	acquired, err := r.rt.AcquireTaskExecutionWithClaims(ctx, core.AcquireTaskExecutionCommand{
		RunID: cmd.RunID, TaskID: cmd.TaskID, EnvelopeID: cmd.EnvelopeID,
		HolderType: api.HolderType(cmd.HolderType), HolderID: cmd.HolderID, TTL: cmd.TTL,
	})
	if err != nil {
		return api.AcquireTaskExecutionResult{}, err
	}
	return api.AcquireTaskExecutionResult{
		Lease: acquired.Lease, Acquired: acquired.Acquired,
		ResourceClaims: acquired.ResourceClaims,
	}, nil
}

func (r *Runner) HeartbeatTaskExecution(ctx context.Context, cmd api.HeartbeatTaskExecutionCommand) error {
	return r.rt.HeartbeatTaskExecution(ctx, core.HeartbeatTaskExecutionCommand(cmd))
}

func (r *Runner) ReleaseTaskExecution(ctx context.Context, cmd api.ReleaseTaskExecutionCommand) error {
	return r.rt.ReleaseTaskExecution(ctx, core.ReleaseTaskExecutionCommand(cmd))
}

func usageRecordToModelPointer(record *api.UsageRecord) *api.UsageRecord {
	if record == nil {
		return nil
	}
	converted := *record
	return &converted
}

func (r *Runner) AppendTaskExecutionEvent(ctx context.Context, cmd api.AppendTaskExecutionEventCommand) error {
	_, err := r.rt.ExecuteCommand(ctx, core.AppendTaskExecutionEventCommand{
		RunID: cmd.RunID, TaskID: cmd.TaskID, LeaseID: cmd.LeaseID,
		HolderType: api.HolderType(cmd.HolderType), HolderID: cmd.HolderID,
		TaskVersion: cmd.TaskVersion, Event: cmd.Event,
		UsageRecords: cmd.UsageRecords,
	})
	return err
}

func (r *Runner) InvokeTool(ctx context.Context, cmd api.ToolInvocation) (api.ToolInvocationResult, error) {
	result, err := r.rt.InvokeTool(ctx, cmd)
	if err != nil {
		return api.ToolInvocationResult{}, err
	}
	return result, nil
}

func (r *Runner) EnforceToolResult(ctx context.Context, request api.ToolResultEnforcementRequest) (api.ToolResultEnforcementResult, error) {
	result, err := r.rt.EnforceToolResult(
		ctx,
		request.RunID,
		request.TaskID,
		request.Decision,
		append(json.RawMessage(nil), request.ToolResult...),
	)
	if err != nil {
		return api.ToolResultEnforcementResult{}, err
	}
	return api.ToolResultEnforcementResult{ToolResult: append(json.RawMessage(nil), result...)}, nil
}

func (r *Runner) RequestHandoff(ctx context.Context, cmd api.HandoffCommand) error {
	return r.rt.RequestHandoff(ctx, core.HandoffCommand(cmd))
}

func (r *Runner) RequestApproval(ctx context.Context, cmd api.RequestApprovalCommand) (api.ApprovalRequest, api.ResumeToken, error) {
	approval, token, err := r.rt.RequestApproval(ctx, core.RequestApprovalCommand{RunID: cmd.RunID, TaskID: cmd.TaskID, ActionID: cmd.ActionID, RequesterAgentID: cmd.RequesterAgentID, Reason: cmd.Reason, RiskSummary: cmd.RiskSummary, RequestedAction: cmd.RequestedAction, Metadata: maps.Clone(cmd.Metadata)})
	if err != nil {
		return api.ApprovalRequest{}, api.ResumeToken{}, err
	}
	return approval, token, nil
}

func (r *Runner) DecideApproval(ctx context.Context, cmd api.DecideApprovalCommand) error {
	return r.rt.DecideApproval(ctx, core.DecideApprovalCommand(cmd))
}

func (r *Runner) RecoverResumeToken(ctx context.Context, cmd api.RecoverResumeTokenCommand) (api.ResumeToken, error) {
	token, err := r.rt.RecoverResumeToken(ctx, core.RecoverResumeTokenCommand(cmd))
	if err != nil {
		return api.ResumeToken{}, err
	}
	return token, nil
}

func (r *Runner) ResumeTokensContext(ctx context.Context) (map[string]api.ResumeToken, error) {
	tokens, err := r.rt.ResumeTokens(ctx)
	if err != nil {
		return nil, err
	}
	return tokens, nil
}

func (r *Runner) StartActionAttempt(ctx context.Context, cmd api.StartActionAttemptCommand) (api.ActionAttempt, error) {
	attempt, err := r.rt.StartActionAttempt(ctx, core.StartActionAttemptCommand{AttemptID: cmd.AttemptID, ActionID: cmd.ActionID, RunID: cmd.RunID, TaskID: cmd.TaskID, LeaseID: cmd.LeaseID, HolderType: api.HolderType(cmd.HolderType), HolderID: cmd.HolderID, TaskVersion: cmd.TaskVersion, ToolName: cmd.ToolName, IdempotencyKey: cmd.IdempotencyKey, InputHash: cmd.InputHash})
	if err != nil {
		return api.ActionAttempt{}, err
	}
	return attempt, nil
}

func (r *Runner) CompleteActionAttempt(ctx context.Context, cmd api.CompleteActionAttemptCommand) (api.ActionAttempt, error) {
	attempt, err := r.rt.CompleteActionAttempt(ctx, core.CompleteActionAttemptCommand{
		RunID: cmd.RunID, TaskID: cmd.TaskID, LeaseID: cmd.LeaseID,
		HolderType: api.HolderType(cmd.HolderType), HolderID: cmd.HolderID,
		TaskVersion: cmd.TaskVersion, AttemptID: cmd.AttemptID,
		Status: api.ActionAttemptStatus(cmd.Status), ExternalRequestID: cmd.ExternalRequestID,
		ExternalResultRef: cmd.ExternalResultRef, ToolResult: append(json.RawMessage(nil), cmd.ToolResult...),
		RequiresReconcile: cmd.RequiresReconcile, UsageRecord: usageRecordToModelPointer(cmd.UsageRecord),
	})
	if err != nil {
		return api.ActionAttempt{}, err
	}
	return attempt, nil
}

func (r *Runner) ResolveActionAttempt(ctx context.Context, cmd api.ResolveActionAttemptCommand) (api.ActionAttempt, error) {
	attempt, err := r.rt.ResolveActionAttempt(ctx, core.ResolveActionAttemptCommand{
		AttemptID:         cmd.AttemptID,
		Status:            api.ActionAttemptStatus(cmd.Status),
		ExternalResultRef: cmd.ExternalResultRef,
		ToolResult:        append(json.RawMessage(nil), cmd.ToolResult...),
	})
	if err != nil {
		return api.ActionAttempt{}, err
	}
	return attempt, nil
}

func (r *Runner) ListActionAttempts(ctx context.Context, selector api.ActionAttemptSelector) ([]api.ActionAttempt, error) {
	attempts, err := r.rt.ListActionAttempts(ctx, selector)
	if err != nil {
		return nil, err
	}
	return append([]api.ActionAttempt(nil), attempts...), nil
}

func (r *Runner) StartTraceSpan(ctx context.Context, cmd api.StartTraceSpanCommand) (api.TraceSpan, error) {
	span, err := r.rt.StartTraceSpan(ctx, core.StartTraceSpanCommand{RunID: cmd.RunID, TaskID: cmd.TaskID, TraceID: cmd.TraceID, ParentID: cmd.ParentID, Name: cmd.Name, Component: cmd.Component, Metadata: cloneStringMap(cmd.Metadata)})
	if err != nil {
		return api.TraceSpan{}, err
	}
	return span, nil
}

func (r *Runner) EndTraceSpan(ctx context.Context, cmd api.EndTraceSpanCommand) error {
	return r.rt.EndTraceSpan(ctx, core.EndTraceSpanCommand(cmd))
}

func (r *Runner) SaveTraceSpan(ctx context.Context, span api.TraceSpan) error {
	return r.rt.SaveTraceSpan(ctx, span)
}

func (r *Runner) ListTraceSpans(ctx context.Context, runID string) ([]api.TraceSpan, error) {
	spans, err := r.rt.ListTraceSpans(ctx, runID)
	if err != nil {
		return nil, err
	}
	return spans, nil
}

// AppendUsage persists one usage-metering record. The worker runtime
// calls it after every engine run; hosts may also append their own
// records (e.g. for non-engine model calls).
func (r *Runner) AppendUsage(ctx context.Context, record api.UsageRecord) error {
	return r.rt.AppendUsage(ctx, record)
}

// QueryUsage returns the usage records matching selector.
func (r *Runner) QueryUsage(ctx context.Context, selector api.UsageSelector) ([]api.UsageRecord, error) {
	records, err := r.rt.QueryUsage(ctx, selector)
	if err != nil {
		return nil, err
	}
	return records, nil
}

// SumUsageCredits returns the credit sum over records matching selector.
func (r *Runner) SumUsageCredits(ctx context.Context, selector api.UsageSelector) (int64, error) {
	total, err := r.rt.SumUsageCredits(ctx, selector)
	if err != nil {
		return 0, err
	}
	return total, nil
}

// PendingResumeTokens lists unconsumed resume tokens matching selector.
// After a crash, a host enumerates pending tokens with this and recovers
// each via RecoverResumeToken — the bulk-recovery entry point.
func (r *Runner) PendingResumeTokens(ctx context.Context, selector api.ResumeTokenSelector) ([]api.ResumeToken, error) {
	tokens, err := r.rt.PendingResumeTokens(ctx, selector)
	if err != nil {
		return nil, err
	}
	return tokens, nil
}
