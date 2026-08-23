package venat

import (
	"context"
	"time"

	"github.com/Viking602/venat/api"
)

// RunAdmin is the registration, configuration, and raw-store administration surface.
type RunAdmin struct{ runner *Runner }

// Governance is the lease, admission, approval, action-attempt, usage, and trace surface.
type Governance struct{ runner *Runner }

// Blackboard is the run-scoped working-memory surface.
type Blackboard struct{ runner *Runner }

// Admin returns the administration sub-façade.
func (r *Runner) Admin() RunAdmin { return RunAdmin{runner: r} }

// Governance returns the governance sub-façade.
func (r *Runner) Governance() Governance { return Governance{runner: r} }

// Blackboard returns the blackboard sub-façade.
func (r *Runner) Blackboard() Blackboard { return Blackboard{runner: r} }

func (f RunAdmin) RegisterAgent(profile api.AgentProfile) { f.runner.RegisterAgent(profile) }
func (f RunAdmin) Agents() []api.AgentProfile             { return f.runner.Agents() }
func (f RunAdmin) RegisterTool(tool api.Tool)             { f.runner.RegisterTool(tool) }
func (f RunAdmin) RegisterToolForInvocation(runID, taskID string, holderType api.HolderType, holderID string, tool api.Tool) {
	f.runner.RegisterToolForInvocation(runID, taskID, holderType, holderID, tool)
}

func (f RunAdmin) RemoveToolsForInvocation(runID, taskID string, holderType api.HolderType, holderID string) {
	f.runner.RemoveToolsForInvocation(runID, taskID, holderType, holderID)
}
func (f RunAdmin) RegisterFlow(flow api.Flow) error { return f.runner.RegisterFlow(flow) }
func (f RunAdmin) SetMessagePolicy(policy api.MessagePolicyChecker) {
	f.runner.SetMessagePolicy(policy)
}
func (f RunAdmin) SetPolicyEngine(policy api.PolicyEngine)       { f.runner.SetPolicyEngine(policy) }
func (f RunAdmin) SetOutputGateway(gateway api.OutputGateway)    { f.runner.SetOutputGateway(gateway) }
func (f RunAdmin) SetPipeline(components api.PipelineComponents) { f.runner.SetPipeline(components) }
func (f RunAdmin) StoreProvider() api.StoreProvider              { return f.runner.StoreProvider() }
func (f RunAdmin) StoreCapabilities(ctx context.Context) (api.StoreCapabilities, error) {
	return f.runner.StoreCapabilities(ctx)
}
func (f RunAdmin) Begin(ctx context.Context) (api.UnitOfWork, error) { return f.runner.Begin(ctx) }
func (f RunAdmin) SaveRun(ctx context.Context, run api.Run) error    { return f.runner.SaveRun(ctx, run) }
func (f RunAdmin) LoadRun(ctx context.Context, runID string) (api.Run, error) {
	return f.runner.LoadRun(ctx, runID)
}

func (f RunAdmin) AppendEvent(ctx context.Context, event api.Event) error {
	return f.runner.AppendEvent(ctx, event)
}

func (f RunAdmin) ListEvents(ctx context.Context, runID string) ([]api.Event, error) {
	return f.runner.ListEvents(ctx, runID)
}

func (f RunAdmin) SaveAgentDefinitionSnapshot(ctx context.Context, snapshot api.AgentDefinitionSnapshot) error {
	return f.runner.SaveAgentDefinitionSnapshot(ctx, snapshot)
}

func (f RunAdmin) LoadAgentDefinitionSnapshot(ctx context.Context, definitionID, version string) (api.AgentDefinitionSnapshot, error) {
	return f.runner.LoadAgentDefinitionSnapshot(ctx, definitionID, version)
}

func (f RunAdmin) ListAgentDefinitionSnapshots(ctx context.Context, selector api.AgentDefinitionSnapshotSelector) ([]api.AgentDefinitionSnapshot, error) {
	return f.runner.ListAgentDefinitionSnapshots(ctx, selector)
}

func (f Governance) PreviewAdmission(ctx context.Context, request api.AdmissionRequest) (api.AdmissionDecision, error) {
	return f.runner.PreviewAdmission(ctx, request)
}

func (f Governance) ReserveAdmission(ctx context.Context, request api.AdmissionRequest) (api.AdmissionDecision, error) {
	return f.runner.ReserveAdmission(ctx, request)
}

func (f Governance) TransitionAdmission(ctx context.Context, transition api.AdmissionTransition) (api.AdmissionDecision, error) {
	return f.runner.TransitionAdmission(ctx, transition)
}

func (f Governance) LoadAdmissionReservation(ctx context.Context, id string) (api.AdmissionReservation, error) {
	return f.runner.LoadAdmissionReservation(ctx, id)
}

func (f Governance) ListAdmissionReservations(ctx context.Context, selector api.AdmissionReservationSelector) ([]api.AdmissionReservation, error) {
	return f.runner.ListAdmissionReservations(ctx, selector)
}

func (f Governance) AcquireResourceClaims(ctx context.Context, request api.ResourceClaimRequest) (api.ResourceClaimDecision, error) {
	return f.runner.AcquireResourceClaims(ctx, request)
}

func (f Governance) TransitionResourceClaims(ctx context.Context, request api.ResourceClaimTransitionRequest) (api.ResourceClaimDecision, error) {
	return f.runner.TransitionResourceClaims(ctx, request)
}

func (f Governance) LoadResourceClaim(ctx context.Context, id string) (api.ResourceClaim, error) {
	return f.runner.LoadResourceClaim(ctx, id)
}

func (f Governance) ListResourceClaims(ctx context.Context, selector api.ResourceClaimSelector) ([]api.ResourceClaim, error) {
	return f.runner.ListResourceClaims(ctx, selector)
}

func (f Governance) ActiveLeaseCountContext(ctx context.Context, runID, taskID string) (int, error) {
	return f.runner.ActiveLeaseCountContext(ctx, runID, taskID)
}

func (f Governance) AcquireTaskExecution(ctx context.Context, cmd api.AcquireTaskExecutionCommand) (api.TaskExecutionLease, bool, error) {
	return f.runner.AcquireTaskExecution(ctx, cmd)
}

func (f Governance) AcquireTaskExecutionWithClaims(ctx context.Context, cmd api.AcquireTaskExecutionCommand) (api.AcquireTaskExecutionResult, error) {
	return f.runner.AcquireTaskExecutionWithClaims(ctx, cmd)
}

func (f Governance) HeartbeatTaskExecution(ctx context.Context, cmd api.HeartbeatTaskExecutionCommand) error {
	return f.runner.HeartbeatTaskExecution(ctx, cmd)
}

func (f Governance) ReleaseTaskExecution(ctx context.Context, cmd api.ReleaseTaskExecutionCommand) error {
	return f.runner.ReleaseTaskExecution(ctx, cmd)
}

func (f Governance) AppendTaskExecutionEvent(ctx context.Context, cmd api.AppendTaskExecutionEventCommand) error {
	return f.runner.AppendTaskExecutionEvent(ctx, cmd)
}

func (f Governance) InvokeTool(ctx context.Context, cmd api.ToolInvocation) (api.ToolInvocationResult, error) {
	return f.runner.InvokeTool(ctx, cmd)
}

func (f Governance) EnforceToolResult(ctx context.Context, request api.ToolResultEnforcementRequest) (api.ToolResultEnforcementResult, error) {
	return f.runner.EnforceToolResult(ctx, request)
}

func (f Governance) RequestHandoff(ctx context.Context, cmd api.HandoffCommand) error {
	return f.runner.RequestHandoff(ctx, cmd)
}

func (f Governance) RequestApproval(ctx context.Context, cmd api.RequestApprovalCommand) (api.ApprovalRequest, api.ResumeToken, error) {
	return f.runner.RequestApproval(ctx, cmd)
}

func (f Governance) DecideApproval(ctx context.Context, cmd api.DecideApprovalCommand) error {
	return f.runner.DecideApproval(ctx, cmd)
}

func (f Governance) RecoverResumeToken(ctx context.Context, cmd api.RecoverResumeTokenCommand) (api.ResumeToken, error) {
	return f.runner.RecoverResumeToken(ctx, cmd)
}

func (f Governance) ResumeTokensContext(ctx context.Context) (map[string]api.ResumeToken, error) {
	return f.runner.ResumeTokensContext(ctx)
}

func (f Governance) PendingResumeTokens(ctx context.Context, selector api.ResumeTokenSelector) ([]api.ResumeToken, error) {
	return f.runner.PendingResumeTokens(ctx, selector)
}

func (f Governance) StartActionAttempt(ctx context.Context, cmd api.StartActionAttemptCommand) (api.ActionAttempt, error) {
	return f.runner.StartActionAttempt(ctx, cmd)
}

func (f Governance) CompleteActionAttempt(ctx context.Context, cmd api.CompleteActionAttemptCommand) (api.ActionAttempt, error) {
	return f.runner.CompleteActionAttempt(ctx, cmd)
}

func (f Governance) ResolveActionAttempt(ctx context.Context, cmd api.ResolveActionAttemptCommand) (api.ActionAttempt, error) {
	return f.runner.ResolveActionAttempt(ctx, cmd)
}

func (f Governance) ListActionAttempts(ctx context.Context, selector api.ActionAttemptSelector) ([]api.ActionAttempt, error) {
	return f.runner.ListActionAttempts(ctx, selector)
}

func (f Governance) StartTraceSpan(ctx context.Context, cmd api.StartTraceSpanCommand) (api.TraceSpan, error) {
	return f.runner.StartTraceSpan(ctx, cmd)
}

func (f Governance) EndTraceSpan(ctx context.Context, cmd api.EndTraceSpanCommand) error {
	return f.runner.EndTraceSpan(ctx, cmd)
}

func (f Governance) SaveTraceSpan(ctx context.Context, span api.TraceSpan) error {
	return f.runner.SaveTraceSpan(ctx, span)
}

func (f Governance) ListTraceSpans(ctx context.Context, runID string) ([]api.TraceSpan, error) {
	return f.runner.ListTraceSpans(ctx, runID)
}

func (f Governance) AppendUsage(ctx context.Context, record api.UsageRecord) error {
	return f.runner.AppendUsage(ctx, record)
}

func (f Governance) QueryUsage(ctx context.Context, selector api.UsageSelector) ([]api.UsageRecord, error) {
	return f.runner.QueryUsage(ctx, selector)
}

func (f Governance) SumUsageCredits(ctx context.Context, selector api.UsageSelector) (int64, error) {
	return f.runner.SumUsageCredits(ctx, selector)
}

func (f Blackboard) WriteItem(ctx context.Context, item api.BlackboardItem) error {
	return f.runner.WriteItem(ctx, item)
}

func (f Blackboard) SelectItems(ctx context.Context, runID string, selector api.BlackboardSelector) ([]api.BlackboardItem, error) {
	return f.runner.SelectItems(ctx, runID, selector)
}

func (f Blackboard) Subscribe(ctx context.Context, runID string, filter api.BlackboardFilter) (<-chan api.BlackboardItem, func() error, error) {
	return f.runner.Subscribe(ctx, runID, filter)
}

func (f Blackboard) WaitForBlackboard(ctx context.Context, runID string, filter api.BlackboardFilter, predicate func([]api.BlackboardItem) bool, timeout time.Duration) ([]api.BlackboardItem, error) {
	return f.runner.WaitForBlackboard(ctx, runID, filter, predicate, timeout)
}
