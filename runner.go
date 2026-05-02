package hydaelyn

import (
	"context"
	"time"

	"github.com/Viking602/go-hydaelyn/api"
	core "github.com/Viking602/go-hydaelyn/internal/core"
	"github.com/Viking602/go-hydaelyn/internal/core/adapter"
)

// Runner is the public façade over the internal runtime. All public contract
// values crossing this boundary use api package types, not internal/core types.
type Runner struct {
	rt *core.Runtime
}

func (r *Runner) QueueRun(ctx context.Context, cmd api.StartRunCommand) (api.Run, error) {
	run, err := r.rt.QueueRun(ctx, adapter.StartRunCommandToCore(cmd))
	if err != nil {
		return api.Run{}, adapter.ErrorToAPI(err)
	}
	return adapter.RunFromModel(run), nil
}

func (r *Runner) StartRun(ctx context.Context, cmd api.StartRunCommand) (api.Run, api.Task, error) {
	run, task, err := r.rt.StartRun(ctx, adapter.StartRunCommandToCore(cmd))
	if err != nil {
		return api.Run{}, api.Task{}, adapter.ErrorToAPI(err)
	}
	return adapter.RunFromModel(run), adapter.TaskFromModel(task), nil
}

func (r *Runner) CreateTask(ctx context.Context, cmd api.CreateTaskCommand) (api.Task, error) {
	task, err := r.rt.CreateTask(ctx, adapter.CreateTaskCommandToCore(cmd))
	if err != nil {
		return api.Task{}, adapter.ErrorToAPI(err)
	}
	return adapter.TaskFromModel(task), nil
}

func (r *Runner) Run(ctx context.Context, runID string) (api.Run, error) {
	run, err := r.rt.Run(ctx, runID)
	if err != nil {
		return api.Run{}, adapter.ErrorToAPI(err)
	}
	return adapter.RunFromModel(run), nil
}

func (r *Runner) Task(ctx context.Context, runID, taskID string) (api.Task, error) {
	task, err := r.rt.Task(ctx, runID, taskID)
	if err != nil {
		return api.Task{}, adapter.ErrorToAPI(err)
	}
	return adapter.TaskFromModel(task), nil
}

func (r *Runner) ReadyTasks(runID string) []api.Task {
	return adapter.TasksFromModel(r.rt.ReadyTasks(runID))
}

func (r *Runner) Events(runID string) []api.Event {
	return adapter.EventsFromModel(r.rt.Events(runID))
}

func (r *Runner) RunEvents(ctx context.Context, runID string) ([]api.Event, error) {
	events, err := r.rt.RunEvents(ctx, runID)
	if err != nil {
		return nil, adapter.ErrorToAPI(err)
	}
	return adapter.EventsFromModel(events), nil
}

func (r *Runner) ActiveLeaseCount(runID, taskID string) int {
	return r.rt.ActiveLeaseCount(runID, taskID)
}

func (r *Runner) TransitionRun(ctx context.Context, cmd api.TransitionRunCommand) error {
	return adapter.ErrorToAPI(r.rt.TransitionRun(ctx, adapter.TransitionRunCommandToCore(cmd)))
}

func (r *Runner) TransitionTask(ctx context.Context, cmd api.TransitionTaskCommand) error {
	return adapter.ErrorToAPI(r.rt.TransitionTask(ctx, adapter.TransitionTaskCommandToCore(cmd)))
}

func (r *Runner) AdvanceRun(ctx context.Context, cmd api.AdvanceRunCommand) (api.Run, error) {
	run, err := r.rt.AdvanceRun(ctx, core.AdvanceRunCommand{RunID: cmd.RunID})
	if err != nil {
		return api.Run{}, adapter.ErrorToAPI(err)
	}
	return adapter.RunFromModel(run), nil
}

func (r *Runner) DispatchTask(ctx context.Context, cmd api.DispatchTaskCommand) (api.TaskEnvelope, error) {
	envelope, err := r.rt.DispatchTask(ctx, core.DispatchTaskCommand{
		RunID:           cmd.RunID,
		TaskID:          cmd.TaskID,
		TargetAgentID:   cmd.TargetAgentID,
		TargetComponent: cmd.TargetComponent,
		Payload:         cloneAnyMap(cmd.Payload),
	})
	if err != nil {
		return api.TaskEnvelope{}, adapter.ErrorToAPI(err)
	}
	return adapter.TaskEnvelopeFromModel(envelope), nil
}

func (r *Runner) DispatchTaskFanOut(ctx context.Context, cmd api.FanOutDispatchTaskCommand) ([]api.TaskEnvelope, error) {
	envelopes, err := r.rt.DispatchTaskFanOut(ctx, core.FanOutDispatchTaskCommand{
		RunID:   cmd.RunID,
		TaskID:  cmd.TaskID,
		To:      adapter.AddressToModel(cmd.To),
		Payload: cloneAnyMap(cmd.Payload),
	})
	if err != nil {
		return nil, adapter.ErrorToAPI(err)
	}
	return adapter.TaskEnvelopesFromModel(envelopes), nil
}

func (r *Runner) AckEnvelope(ctx context.Context, cmd api.AckEnvelopeCommand) error {
	return adapter.ErrorToAPI(r.rt.AckEnvelope(ctx, core.AckEnvelopeCommand{EnvelopeID: cmd.EnvelopeID, HolderID: cmd.HolderID}))
}

func (r *Runner) DeadLetter(ctx context.Context, cmd api.DeadLetterCommand) error {
	return adapter.ErrorToAPI(r.rt.DeadLetter(ctx, core.DeadLetterCommand{EnvelopeID: cmd.EnvelopeID, Reason: cmd.Reason}))
}

func (r *Runner) AcquireTaskExecution(ctx context.Context, cmd api.AcquireTaskExecutionCommand) (api.TaskExecutionLease, bool, error) {
	lease, acquired, err := r.rt.AcquireTaskExecution(ctx, core.AcquireTaskExecutionCommand{
		RunID:      cmd.RunID,
		TaskID:     cmd.TaskID,
		EnvelopeID: cmd.EnvelopeID,
		HolderType: core.HolderType(cmd.HolderType),
		HolderID:   cmd.HolderID,
		TTL:        cmd.TTL,
	})
	if err != nil {
		return api.TaskExecutionLease{}, false, adapter.ErrorToAPI(err)
	}
	return adapter.TaskExecutionLeaseFromModel(lease), acquired, nil
}

func (r *Runner) HeartbeatTaskExecution(ctx context.Context, cmd api.HeartbeatTaskExecutionCommand) error {
	return adapter.ErrorToAPI(r.rt.HeartbeatTaskExecution(ctx, core.HeartbeatTaskExecutionCommand{LeaseID: cmd.LeaseID, TTL: cmd.TTL}))
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

func (r *Runner) WriteItem(ctx context.Context, item api.BlackboardItem) error {
	return adapter.ErrorToAPI(r.rt.WriteItem(ctx, adapter.BlackboardItemToModel(item)))
}

func (r *Runner) SelectItems(ctx context.Context, runID string, selector api.BlackboardSelector) ([]api.BlackboardItem, error) {
	items, err := r.rt.SelectItems(ctx, runID, adapter.BlackboardSelectorToModel(selector))
	if err != nil {
		return nil, adapter.ErrorToAPI(err)
	}
	return adapter.BlackboardItemsFromModel(items), nil
}

func (r *Runner) Subscribe(ctx context.Context, runID string, filter api.BlackboardFilter) (<-chan api.BlackboardItem, func() error, error) {
	items, cancel, err := r.rt.Subscribe(ctx, runID, adapter.BlackboardSelectorToModel(filter))
	if err != nil {
		return nil, nil, adapter.ErrorToAPI(err)
	}
	out := make(chan api.BlackboardItem)
	go func() {
		defer close(out)
		for item := range items {
			out <- adapter.BlackboardItemFromModel(item)
		}
	}()
	return out, func() error { return adapter.ErrorToAPI(cancel()) }, nil
}

func (r *Runner) WaitForBlackboard(ctx context.Context, runID string, filter api.BlackboardFilter, predicate func([]api.BlackboardItem) bool, timeout time.Duration) ([]api.BlackboardItem, error) {
	items, err := r.rt.WaitForBlackboard(ctx, runID, adapter.BlackboardSelectorToModel(filter), func(items []core.BlackboardItem) bool {
		return predicate(adapter.BlackboardItemsFromModel(items))
	}, timeout)
	if err != nil {
		return nil, adapter.ErrorToAPI(err)
	}
	return adapter.BlackboardItemsFromModel(items), nil
}

func (r *Runner) SubmitTypedReport(ctx context.Context, cmd api.SubmitTypedReportCommand) error {
	return adapter.ErrorToAPI(r.rt.SubmitTypedReport(ctx, adapter.SubmitTypedReportCommandToCore(cmd)))
}

func (r *Runner) SubmitUserInput(ctx context.Context, cmd api.SubmitUserInputCommand) error {
	return adapter.ErrorToAPI(r.rt.SubmitUserInput(ctx, core.SubmitUserInputCommand{RunID: cmd.RunID, TaskID: cmd.TaskID, Input: cmd.Input}))
}

func (r *Runner) RequestHandoff(ctx context.Context, cmd api.HandoffCommand) error {
	return adapter.ErrorToAPI(r.rt.RequestHandoff(ctx, core.HandoffCommand{RunID: cmd.RunID, TaskID: cmd.TaskID, FromAgentID: cmd.FromAgentID, ToAgentID: cmd.ToAgentID, TaskVersion: cmd.TaskVersion, HandoffContext: cmd.HandoffContext}))
}

func (r *Runner) SubmitResponseOutput(ctx context.Context, cmd api.SubmitResponseOutputCommand) error {
	return adapter.ErrorToAPI(r.rt.SubmitResponseOutput(ctx, core.SubmitResponseOutputCommand{RunID: cmd.RunID, TaskID: cmd.TaskID, LeaseID: cmd.LeaseID, HolderType: core.HolderType(cmd.HolderType), HolderID: cmd.HolderID, TaskVersion: cmd.TaskVersion, Type: core.UserMessageType(cmd.Type), Title: cmd.Title, Payload: cmd.Payload, IdempotencyKey: cmd.IdempotencyKey}))
}

func (r *Runner) PublishResponse(ctx context.Context, cmd api.PublishResponseCommand) error {
	return adapter.ErrorToAPI(r.rt.PublishResponse(ctx, core.PublishResponseCommand{RunID: cmd.RunID, MessageID: cmd.MessageID}))
}

func (r *Runner) DrainResponseOutbox(ctx context.Context) (int, error) {
	published, err := r.rt.DrainResponseOutbox(ctx)
	return published, adapter.ErrorToAPI(err)
}

func (r *Runner) ResponseOutbox(runID string) []api.UserMessage {
	return adapter.UserMessagesFromModel(r.rt.ResponseOutbox(runID))
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

func (r *Runner) StartActionAttempt(ctx context.Context, cmd api.StartActionAttemptCommand) (api.ActionAttempt, error) {
	attempt, err := r.rt.StartActionAttempt(ctx, core.StartActionAttemptCommand{AttemptID: cmd.AttemptID, ActionID: cmd.ActionID, RunID: cmd.RunID, TaskID: cmd.TaskID, LeaseID: cmd.LeaseID, HolderType: core.HolderType(cmd.HolderType), HolderID: cmd.HolderID, TaskVersion: cmd.TaskVersion, ToolName: cmd.ToolName, IdempotencyKey: cmd.IdempotencyKey, InputHash: cmd.InputHash})
	if err != nil {
		return api.ActionAttempt{}, adapter.ErrorToAPI(err)
	}
	return adapter.ActionAttemptFromModel(attempt), nil
}

func (r *Runner) CompleteActionAttempt(ctx context.Context, cmd api.CompleteActionAttemptCommand) (api.ActionAttempt, error) {
	attempt, err := r.rt.CompleteActionAttempt(ctx, core.CompleteActionAttemptCommand{RunID: cmd.RunID, TaskID: cmd.TaskID, LeaseID: cmd.LeaseID, HolderType: core.HolderType(cmd.HolderType), HolderID: cmd.HolderID, TaskVersion: cmd.TaskVersion, AttemptID: cmd.AttemptID, Status: core.ActionAttemptStatus(cmd.Status), ExternalRequestID: cmd.ExternalRequestID, ExternalResultRef: cmd.ExternalResultRef, RequiresReconcile: cmd.RequiresReconcile})
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

func (r *Runner) TraceSpans(runID string) []api.TraceSpan {
	return adapter.TraceSpansFromModel(r.rt.TraceSpans(runID))
}

func (r *Runner) Replay(runID string, mode api.ReplayMode) (api.Projection, error) {
	projection, err := r.rt.Replay(runID, core.ReplayMode(mode))
	if err != nil {
		return api.Projection{}, adapter.ErrorToAPI(err)
	}
	return adapter.ProjectionFromModel(projection), nil
}

func (r *Runner) ReplayRunState(runID string) (api.Projection, error) {
	projection, err := r.rt.ReplayRunState(runID)
	if err != nil {
		return api.Projection{}, adapter.ErrorToAPI(err)
	}
	return adapter.ProjectionFromModel(projection), nil
}

func (r *Runner) Recover(ctx context.Context, runID string) (api.Projection, error) {
	projection, err := r.rt.Recover(ctx, runID)
	if err != nil {
		return api.Projection{}, adapter.ErrorToAPI(err)
	}
	return adapter.ProjectionFromModel(projection), nil
}

func (r *Runner) RunTimeline(ctx context.Context, runID string) ([]api.RunTimelineItem, error) {
	items, err := r.rt.RunTimeline(ctx, runID)
	if err != nil {
		return nil, adapter.ErrorToAPI(err)
	}
	return adapter.RunTimelineItemsFromModel(items), nil
}

func (r *Runner) RegisterAgent(profile api.AgentProfile) {
	r.rt.RegisterAgent(adapter.AgentProfileToModel(profile))
}

func (r *Runner) Agents() []api.AgentProfile {
	return adapter.AgentProfilesFromModel(r.rt.Agents())
}

func (r *Runner) RegisterTool(tool api.Tool) {
	r.rt.RegisterTool(adapter.ToolToModel(tool))
}

func (r *Runner) RegisterFlow(flow api.Flow) error {
	return adapter.ErrorToAPI(r.rt.RegisterFlow(adapter.FlowToModel(flow)))
}

func (r *Runner) SetMessagePolicy(policy api.MessagePolicyChecker) {
	if policy == nil {
		r.rt.SetMessagePolicy(nil)
		return
	}
	r.rt.SetMessagePolicy(func(message core.UserMessage) core.PolicyDecision {
		return adapter.PolicyDecisionToModel(policy(adapter.UserMessageFromModel(message)))
	})
}

func (r *Runner) SetPolicyEngine(policy api.PolicyEngine) {
	r.rt.SetPolicyEngine(adapter.PolicyEngineToCore(policy))
}

func (r *Runner) SetOutputGateway(gateway api.OutputGateway) {
	r.rt.SetOutputGateway(adapter.OutputGatewayToCore(gateway))
}

func (r *Runner) SetPipeline(components api.PipelineComponents) {
	r.rt.SetPipeline(adapter.PipelineToCore(components))
}

func (r *Runner) StoreProvider() api.StoreProvider {
	return adapter.StoreProviderFromCore(r.rt.StoreProvider())
}

func (r *Runner) Begin(ctx context.Context) (api.UnitOfWork, error) {
	uow, err := r.rt.Begin(ctx)
	if err != nil {
		return nil, adapter.ErrorToAPI(err)
	}
	return adapter.UnitOfWorkFromCore(uow), nil
}

func (r *Runner) SaveRun(ctx context.Context, run api.Run) error {
	return adapter.ErrorToAPI(r.rt.SaveRun(ctx, adapter.RunToModel(run)))
}

func (r *Runner) LoadRun(ctx context.Context, runID string) (api.Run, error) {
	run, err := r.rt.LoadRun(ctx, runID)
	if err != nil {
		return api.Run{}, adapter.ErrorToAPI(err)
	}
	return adapter.RunFromModel(run), nil
}

func (r *Runner) SaveTask(ctx context.Context, task api.Task) error {
	return adapter.ErrorToAPI(r.rt.SaveTask(ctx, adapter.TaskToModel(task)))
}

func (r *Runner) LoadTask(ctx context.Context, runID, taskID string) (api.Task, error) {
	task, err := r.rt.LoadTask(ctx, runID, taskID)
	if err != nil {
		return api.Task{}, adapter.ErrorToAPI(err)
	}
	return adapter.TaskFromModel(task), nil
}

func (r *Runner) ListTasks(ctx context.Context, runID string) ([]api.Task, error) {
	tasks, err := r.rt.ListTasks(ctx, runID)
	if err != nil {
		return nil, adapter.ErrorToAPI(err)
	}
	return adapter.TasksFromModel(tasks), nil
}

func (r *Runner) AppendEvent(ctx context.Context, event api.Event) error {
	return adapter.ErrorToAPI(r.rt.AppendEvent(ctx, adapter.EventToModel(event)))
}

func (r *Runner) ListEvents(ctx context.Context, runID string) ([]api.Event, error) {
	events, err := r.rt.ListEvents(ctx, runID)
	if err != nil {
		return nil, adapter.ErrorToAPI(err)
	}
	return adapter.EventsFromModel(events), nil
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

func (r *Runner) QueueMessage(ctx context.Context, message api.UserMessage) error {
	return adapter.ErrorToAPI(r.rt.QueueMessage(ctx, adapter.UserMessageToModel(message)))
}

func (r *Runner) LoadMessage(ctx context.Context, runID, messageID string) (api.UserMessage, error) {
	message, err := r.rt.LoadMessage(ctx, runID, messageID)
	if err != nil {
		return api.UserMessage{}, adapter.ErrorToAPI(err)
	}
	return adapter.UserMessageFromModel(message), nil
}

func (r *Runner) UpdateMessage(ctx context.Context, message api.UserMessage) error {
	return adapter.ErrorToAPI(r.rt.UpdateMessage(ctx, adapter.UserMessageToModel(message)))
}

func (r *Runner) ListMessages(ctx context.Context, runID string) ([]api.UserMessage, error) {
	messages, err := r.rt.ListMessages(ctx, runID)
	if err != nil {
		return nil, adapter.ErrorToAPI(err)
	}
	return adapter.UserMessagesFromModel(messages), nil
}

func (r *Runner) ResumeTokens() map[string]api.ResumeToken {
	return adapter.ResumeTokensFromModelMap(r.rt.ResumeTokens())
}

func (r *Runner) ListQueuedMessages(ctx context.Context) ([]api.UserMessage, error) {
	messages, err := r.rt.ListQueuedMessages(ctx)
	if err != nil {
		return nil, adapter.ErrorToAPI(err)
	}
	return adapter.UserMessagesFromModel(messages), nil
}

func (r *Runner) QueueEnvelope(ctx context.Context, env api.TaskEnvelope) error {
	return adapter.ErrorToAPI(r.rt.QueueEnvelope(ctx, adapter.TaskEnvelopeToModel(env)))
}

func (r *Runner) LoadEnvelope(ctx context.Context, envelopeID string) (api.TaskEnvelope, error) {
	envelope, err := r.rt.LoadEnvelope(ctx, envelopeID)
	if err != nil {
		return api.TaskEnvelope{}, adapter.ErrorToAPI(err)
	}
	return adapter.TaskEnvelopeFromModel(envelope), nil
}

func (r *Runner) UpdateEnvelope(ctx context.Context, env api.TaskEnvelope) error {
	return adapter.ErrorToAPI(r.rt.UpdateEnvelope(ctx, adapter.TaskEnvelopeToModel(env)))
}

func (r *Runner) ListEnvelopes(ctx context.Context, runID string) ([]api.TaskEnvelope, error) {
	envelopes, err := r.rt.ListEnvelopes(ctx, runID)
	if err != nil {
		return nil, adapter.ErrorToAPI(err)
	}
	return adapter.TaskEnvelopesFromModel(envelopes), nil
}

func (r *Runner) ExecuteCommand(ctx context.Context, command api.Command) (any, error) {
	coreCommand, ok := adapter.CommandToCore(command)
	if !ok {
		return nil, api.ErrInvalidCommand
	}
	result, err := r.rt.ExecuteCommand(ctx, coreCommand)
	if err != nil {
		return nil, adapter.ErrorToAPI(err)
	}
	return commandResultFromCore(command, result), nil
}

func commandResultFromCore(command api.Command, result any) any {
	if converted, ok := runTaskResultFromCore(command, result); ok {
		return converted
	}
	if converted, ok := mailboxResultFromCore(command, result); ok {
		return converted
	}
	if converted, ok := governanceResultFromCore(command, result); ok {
		return converted
	}
	return result
}

func runTaskResultFromCore(command api.Command, result any) (any, bool) {
	switch command.(type) {
	case api.StartRunCommand:
		started, ok := result.(core.StartRunResult)
		if !ok {
			return result, true
		}
		return []any{adapter.RunFromModel(started.Run), adapter.TaskFromModel(started.Root)}, true
	case api.CreateTaskCommand:
		if task, ok := result.(core.Task); ok {
			return adapter.TaskFromModel(task), true
		}
		return result, true
	case api.AdvanceRunCommand:
		if run, ok := result.(core.Run); ok {
			return adapter.RunFromModel(run), true
		}
		return result, true
	default:
		return nil, false
	}
}

func mailboxResultFromCore(command api.Command, result any) (any, bool) {
	switch command.(type) {
	case api.DispatchTaskCommand:
		if envelope, ok := result.(core.TaskEnvelope); ok {
			return adapter.TaskEnvelopeFromModel(envelope), true
		}
		return result, true
	case api.FanOutDispatchTaskCommand:
		if envelopes, ok := result.([]core.TaskEnvelope); ok {
			return adapter.TaskEnvelopesFromModel(envelopes), true
		}
		return result, true
	default:
		return nil, false
	}
}

func governanceResultFromCore(command api.Command, result any) (any, bool) {
	switch command.(type) {
	case api.AcquireTaskExecutionCommand:
		if acquired, ok := result.(core.AcquireTaskExecutionResult); ok {
			return struct {
				Lease    api.TaskExecutionLease
				Acquired bool
			}{Lease: adapter.TaskExecutionLeaseFromModel(acquired.Lease), Acquired: acquired.Acquired}, true
		}
		return result, true
	case api.ToolInvocation:
		if toolResult, ok := result.(core.ToolInvocationResult); ok {
			return adapter.ToolInvocationResultFromCore(toolResult), true
		}
		return result, true
	case api.RequestApprovalCommand:
		requested, ok := result.(core.RequestApprovalResult)
		if !ok {
			return result, true
		}
		return []any{adapter.ApprovalRequestFromModel(requested.Approval), adapter.ResumeTokenFromModel(requested.Token)}, true
	case api.RecoverResumeTokenCommand:
		if token, ok := result.(core.ResumeToken); ok {
			return adapter.ResumeTokenFromModel(token), true
		}
		return result, true
	case api.StartActionAttemptCommand, api.CompleteActionAttemptCommand:
		if attempt, ok := result.(core.ActionAttempt); ok {
			return adapter.ActionAttemptFromModel(attempt), true
		}
		return result, true
	case api.StartTraceSpanCommand:
		if span, ok := result.(core.TraceSpan); ok {
			return adapter.TraceSpanFromModel(span), true
		}
		return result, true
	default:
		return nil, false
	}
}

func cloneAnyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
