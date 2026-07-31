package venat

import (
	"context"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/internal/core"
	"github.com/Viking602/venat/internal/core/adapter"
	"github.com/Viking602/venat/internal/core/model"
)

// Deprecated: use ActiveLeaseCountContext.
func (r *Runner) ActiveLeaseCount(runID, taskID string) int {
	return r.ActiveLeaseCountContext(context.Background(), runID, taskID)
}

func (r *Runner) ActiveLeaseCountContext(ctx context.Context, runID, taskID string) int {
	return r.rt.ActiveLeaseCount(ctx, runID, taskID)
}

func (r *Runner) AcquireTaskExecution(ctx context.Context, cmd api.AcquireTaskExecutionCommand) (api.TaskExecutionLease, bool, error) {
	lease, acquired, err := r.rt.AcquireTaskExecution(ctx, core.AcquireTaskExecutionCommand{
		RunID:      cmd.RunID,
		TaskID:     cmd.TaskID,
		EnvelopeID: cmd.EnvelopeID,
		HolderType: model.HolderType(cmd.HolderType),
		HolderID:   cmd.HolderID,
		TTL:        cmd.TTL,
	})
	if err != nil {
		return api.TaskExecutionLease{}, false, adapter.ErrorToAPI(err)
	}
	return adapter.TaskExecutionLeaseFromModel(lease), acquired, nil
}

func (r *Runner) HeartbeatTaskExecution(ctx context.Context, cmd api.HeartbeatTaskExecutionCommand) error {
	return adapter.ErrorToAPI(r.rt.HeartbeatTaskExecution(ctx, core.HeartbeatTaskExecutionCommand{LeaseID: cmd.LeaseID, HolderID: cmd.HolderID, TTL: cmd.TTL}))
}

func (r *Runner) ReleaseTaskExecution(ctx context.Context, cmd api.ReleaseTaskExecutionCommand) error {
	return adapter.ErrorToAPI(r.rt.ReleaseTaskExecution(ctx, core.ReleaseTaskExecutionCommand{LeaseID: cmd.LeaseID, HolderID: cmd.HolderID}))
}

func (r *Runner) InvokeTool(ctx context.Context, cmd api.ToolInvocation) (api.ToolInvocationResult, error) {
	result, err := r.rt.InvokeTool(ctx, adapter.ToolInvocationToCore(cmd))
	if err != nil {
		return api.ToolInvocationResult{}, adapter.ErrorToAPI(err)
	}
	return adapter.ToolInvocationResultFromCore(result), nil
}

func (r *Runner) RequestHandoff(ctx context.Context, cmd api.HandoffCommand) error {
	return adapter.ErrorToAPI(r.rt.RequestHandoff(ctx, core.HandoffCommand{RunID: cmd.RunID, TaskID: cmd.TaskID, FromAgentID: cmd.FromAgentID, ToAgentID: cmd.ToAgentID, TaskVersion: cmd.TaskVersion, HandoffContext: cmd.HandoffContext}))
}

func (r *Runner) RequestApproval(ctx context.Context, cmd api.RequestApprovalCommand) (api.ApprovalRequest, api.ResumeToken, error) {
	approval, token, err := r.rt.RequestApproval(ctx, core.RequestApprovalCommand{RunID: cmd.RunID, TaskID: cmd.TaskID, ActionID: cmd.ActionID, RequesterAgentID: cmd.RequesterAgentID, Reason: cmd.Reason, RiskSummary: cmd.RiskSummary, RequestedAction: cmd.RequestedAction})
	if err != nil {
		return api.ApprovalRequest{}, api.ResumeToken{}, adapter.ErrorToAPI(err)
	}
	return adapter.ApprovalRequestFromModel(approval), adapter.ResumeTokenFromModel(token), nil
}

func (r *Runner) DecideApproval(ctx context.Context, cmd api.DecideApprovalCommand) error {
	return adapter.ErrorToAPI(r.rt.DecideApproval(ctx, core.DecideApprovalCommand{RunID: cmd.RunID, ApprovalID: cmd.ApprovalID, DecidedBy: cmd.DecidedBy, Decision: cmd.Decision, Reason: cmd.Reason}))
}

func (r *Runner) RecoverResumeToken(ctx context.Context, cmd api.RecoverResumeTokenCommand) (api.ResumeToken, error) {
	token, err := r.rt.RecoverResumeToken(ctx, core.RecoverResumeTokenCommand{TokenID: cmd.TokenID})
	if err != nil {
		return api.ResumeToken{}, adapter.ErrorToAPI(err)
	}
	return adapter.ResumeTokenFromModel(token), nil
}

func (r *Runner) ResumeTokens() map[string]api.ResumeToken {
	return adapter.ResumeTokensFromModelMap(r.rt.ResumeTokens())
}

func (r *Runner) StartActionAttempt(ctx context.Context, cmd api.StartActionAttemptCommand) (api.ActionAttempt, error) {
	attempt, err := r.rt.StartActionAttempt(ctx, core.StartActionAttemptCommand{AttemptID: cmd.AttemptID, ActionID: cmd.ActionID, RunID: cmd.RunID, TaskID: cmd.TaskID, LeaseID: cmd.LeaseID, HolderType: model.HolderType(cmd.HolderType), HolderID: cmd.HolderID, TaskVersion: cmd.TaskVersion, ToolName: cmd.ToolName, IdempotencyKey: cmd.IdempotencyKey, InputHash: cmd.InputHash})
	if err != nil {
		return api.ActionAttempt{}, adapter.ErrorToAPI(err)
	}
	return adapter.ActionAttemptFromModel(attempt), nil
}

func (r *Runner) CompleteActionAttempt(ctx context.Context, cmd api.CompleteActionAttemptCommand) (api.ActionAttempt, error) {
	attempt, err := r.rt.CompleteActionAttempt(ctx, core.CompleteActionAttemptCommand{RunID: cmd.RunID, TaskID: cmd.TaskID, LeaseID: cmd.LeaseID, HolderType: model.HolderType(cmd.HolderType), HolderID: cmd.HolderID, TaskVersion: cmd.TaskVersion, AttemptID: cmd.AttemptID, Status: model.ActionAttemptStatus(cmd.Status), ExternalRequestID: cmd.ExternalRequestID, ExternalResultRef: cmd.ExternalResultRef, RequiresReconcile: cmd.RequiresReconcile})
	if err != nil {
		return api.ActionAttempt{}, adapter.ErrorToAPI(err)
	}
	return adapter.ActionAttemptFromModel(attempt), nil
}

func (r *Runner) StartTraceSpan(ctx context.Context, cmd api.StartTraceSpanCommand) (api.TraceSpan, error) {
	span, err := r.rt.StartTraceSpan(ctx, core.StartTraceSpanCommand{RunID: cmd.RunID, TaskID: cmd.TaskID, TraceID: cmd.TraceID, ParentID: cmd.ParentID, Name: cmd.Name, Component: cmd.Component, Metadata: cloneStringMap(cmd.Metadata)})
	if err != nil {
		return api.TraceSpan{}, adapter.ErrorToAPI(err)
	}
	return adapter.TraceSpanFromModel(span), nil
}

func (r *Runner) EndTraceSpan(ctx context.Context, cmd api.EndTraceSpanCommand) error {
	return adapter.ErrorToAPI(r.rt.EndTraceSpan(ctx, core.EndTraceSpanCommand{SpanID: cmd.SpanID, Error: cmd.Error}))
}

// Deprecated: use ListTraceSpans so cancellation and storage errors are observable.
func (r *Runner) TraceSpans(runID string) []api.TraceSpan {
	return adapter.TraceSpansFromModel(r.rt.TraceSpans(context.Background(), runID))
}

func (r *Runner) SaveTraceSpan(ctx context.Context, span api.TraceSpan) error {
	return adapter.ErrorToAPI(r.rt.SaveTraceSpan(ctx, adapter.TraceSpanToModel(span)))
}

func (r *Runner) ListTraceSpans(ctx context.Context, runID string) ([]api.TraceSpan, error) {
	spans, err := r.rt.ListTraceSpans(ctx, runID)
	if err != nil {
		return nil, adapter.ErrorToAPI(err)
	}
	return adapter.TraceSpansFromModel(spans), nil
}

// AppendUsage persists one usage-metering record. The worker runtime
// calls it after every engine run; hosts may also append their own
// records (e.g. for non-engine model calls).
func (r *Runner) AppendUsage(ctx context.Context, record api.UsageRecord) error {
	return adapter.ErrorToAPI(r.rt.AppendUsage(ctx, adapter.UsageRecordToModel(record)))
}

// QueryUsage returns the usage records matching selector.
func (r *Runner) QueryUsage(ctx context.Context, selector api.UsageSelector) ([]api.UsageRecord, error) {
	records, err := r.rt.QueryUsage(ctx, adapter.UsageSelectorToModel(selector))
	if err != nil {
		return nil, adapter.ErrorToAPI(err)
	}
	return adapter.UsageRecordsFromModel(records), nil
}

// SumUsageCredits returns the credit sum over records matching selector.
func (r *Runner) SumUsageCredits(ctx context.Context, selector api.UsageSelector) (int64, error) {
	total, err := r.rt.SumUsageCredits(ctx, adapter.UsageSelectorToModel(selector))
	if err != nil {
		return 0, adapter.ErrorToAPI(err)
	}
	return total, nil
}

// PendingResumeTokens lists unconsumed resume tokens matching selector.
// After a crash, a host enumerates pending tokens with this and recovers
// each via RecoverResumeToken — the bulk-recovery entry point.
func (r *Runner) PendingResumeTokens(ctx context.Context, selector api.ResumeTokenSelector) ([]api.ResumeToken, error) {
	tokens, err := r.rt.PendingResumeTokens(ctx, adapter.ResumeTokenSelectorToModel(selector))
	if err != nil {
		return nil, adapter.ErrorToAPI(err)
	}
	return adapter.ResumeTokensFromModel(tokens), nil
}
