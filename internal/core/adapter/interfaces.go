package adapter

import (
	"context"
	"fmt"
	"time"

	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/internal/core"
	"github.com/Viking602/go-hydaelyn/internal/core/model"
	"github.com/Viking602/go-hydaelyn/internal/core/ports"
)

func ConfigToCore(config api.Config) core.Config {
	return core.Config{
		StoreProvider: StoreProviderToCore(config.StoreProvider),
		PolicyEngine:  PolicyEngineToCore(config.PolicyEngine),
		OutputGateway: OutputGatewayToCore(config.OutputGateway),
		Pipeline:      PipelineToCore(config.Pipeline),
	}
}

func PolicyEngineToCore(inner api.PolicyEngine) core.PolicyEngine {
	if inner == nil {
		return nil
	}
	return apiPolicyEngineAdapter{inner: inner}
}

type apiPolicyEngineAdapter struct{ inner api.PolicyEngine }

func (a apiPolicyEngineAdapter) Authorize(ctx context.Context, request model.PolicyRequest) (model.PolicyDecision, error) {
	decision, err := a.inner.Authorize(ctx, PolicyRequestFromModel(request))
	if err != nil {
		return model.PolicyDecision{}, ErrorToCore(err)
	}
	return PolicyDecisionToModel(decision), nil
}

func OutputGatewayToCore(inner api.OutputGateway) core.OutputGateway {
	if inner == nil {
		return nil
	}
	return apiOutputGatewayAdapter{inner: inner}
}

type apiOutputGatewayAdapter struct{ inner api.OutputGateway }

func (a apiOutputGatewayAdapter) Publish(ctx context.Context, message model.UserMessage) error {
	return ErrorToCore(a.inner.Publish(ctx, UserMessageFromModel(message)))
}

func PipelineToCore(components api.PipelineComponents) core.PipelineComponents {
	return core.PipelineComponents{
		IntentAnalyzer: intentAnalyzerToCore(components.IntentAnalyzer),
		Planner:        plannerToCore(components.Planner),
		Validator:      planValidatorToCore(components.Validator),
		Router:         taskRouterToCore(components.Router),
		Dispatcher:     dispatcherToCore(components.Dispatcher),
		TaskMonitor:    taskMonitorToCore(components.TaskMonitor),
	}
}

func intentAnalyzerToCore(inner api.IntentAnalyzer) core.IntentAnalyzer {
	if inner == nil {
		return nil
	}
	return apiIntentAnalyzerAdapter{inner: inner}
}

type apiIntentAnalyzerAdapter struct{ inner api.IntentAnalyzer }

func (a apiIntentAnalyzerAdapter) AnalyzeIntent(ctx context.Context, run model.Run) (model.Intent, error) {
	intent, err := a.inner.AnalyzeIntent(ctx, RunFromModel(run))
	if err != nil {
		return model.Intent{}, ErrorToCore(err)
	}
	return IntentToModel(intent), nil
}

func plannerToCore(inner api.Planner) core.Planner {
	if inner == nil {
		return nil
	}
	return apiPlannerAdapter{inner: inner}
}

type apiPlannerAdapter struct{ inner api.Planner }

func (a apiPlannerAdapter) CreatePlan(ctx context.Context, intent model.Intent) (model.TodoPlan, error) {
	plan, err := a.inner.CreatePlan(ctx, IntentFromModel(intent))
	if err != nil {
		return model.TodoPlan{}, ErrorToCore(err)
	}
	return TodoPlanToModel(plan), nil
}

func planValidatorToCore(inner api.PlanValidator) core.PlanValidator {
	if inner == nil {
		return nil
	}
	return apiPlanValidatorAdapter{inner: inner}
}

type apiPlanValidatorAdapter struct{ inner api.PlanValidator }

func (a apiPlanValidatorAdapter) ValidatePlan(ctx context.Context, plan model.TodoPlan) error {
	return ErrorToCore(a.inner.ValidatePlan(ctx, TodoPlanFromModel(plan)))
}

func taskRouterToCore(inner api.TaskRouter) core.TaskRouter {
	if inner == nil {
		return nil
	}
	return apiTaskRouterAdapter{inner: inner}
}

type apiTaskRouterAdapter struct{ inner api.TaskRouter }

func (a apiTaskRouterAdapter) RouteTasks(ctx context.Context, plan model.TodoPlan) (model.RoutingPlan, error) {
	routing, err := a.inner.RouteTasks(ctx, TodoPlanFromModel(plan))
	if err != nil {
		return model.RoutingPlan{}, ErrorToCore(err)
	}
	return RoutingPlanToModel(routing), nil
}

func dispatcherToCore(inner api.Dispatcher) core.Dispatcher {
	if inner == nil {
		return nil
	}
	return apiDispatcherAdapter{inner: inner}
}

type apiDispatcherAdapter struct{ inner api.Dispatcher }

func (a apiDispatcherAdapter) Dispatch(ctx context.Context, routing model.RoutingPlan) ([]model.TaskEnvelope, error) {
	envelopes, err := a.inner.Dispatch(ctx, RoutingPlanFromModel(routing))
	if err != nil {
		return nil, ErrorToCore(err)
	}
	return TaskEnvelopesToModel(envelopes), nil
}

func taskMonitorToCore(inner api.TaskMonitor) core.TaskMonitor {
	if inner == nil {
		return nil
	}
	return apiTaskMonitorAdapter{inner: inner}
}

type apiTaskMonitorAdapter struct{ inner api.TaskMonitor }

func (a apiTaskMonitorAdapter) Advance(ctx context.Context, run model.Run) error {
	return ErrorToCore(a.inner.Advance(ctx, RunFromModel(run)))
}

func (a apiTaskMonitorAdapter) DecideDeadLetter(ctx context.Context, env model.TaskEnvelope, reason string) (model.TaskMonitorDecision, error) {
	decision, err := a.inner.DecideDeadLetter(ctx, TaskEnvelopeFromModel(env), reason)
	if err != nil {
		return model.TaskMonitorDecision{}, ErrorToCore(err)
	}
	return TaskMonitorDecisionToModel(decision), nil
}

func StoreProviderToCore(inner api.StoreProvider) core.StoreProvider {
	if inner == nil {
		return nil
	}
	base := apiStoreProviderAdapter{inner: inner}
	if subscriber, ok := inner.(api.BlackboardSubscriber); ok {
		return apiStoreProviderSubscriberAdapter{apiStoreProviderAdapter: base, subscriber: subscriber}
	}
	return base
}

type apiStoreProviderAdapter struct{ inner api.StoreProvider }

func (a apiStoreProviderAdapter) Begin(ctx context.Context) (ports.UnitOfWork, error) {
	uow, err := a.inner.Begin(ctx)
	if err != nil {
		return nil, ErrorToCore(err)
	}
	return UnitOfWorkToCore(uow), nil
}

type apiStoreProviderSubscriberAdapter struct {
	apiStoreProviderAdapter
	subscriber api.BlackboardSubscriber
}

func (a apiStoreProviderSubscriberAdapter) Subscribe(ctx context.Context, runID string, selector model.BlackboardSelector) (<-chan model.BlackboardItem, func() error, error) {
	items, cancel, err := a.subscriber.Subscribe(ctx, runID, BlackboardSelectorFromModel(selector))
	if err != nil {
		return nil, nil, ErrorToCore(err)
	}
	out := make(chan model.BlackboardItem)
	go func() {
		defer close(out)
		for item := range items {
			out <- BlackboardItemToModel(item)
		}
	}()
	return out, func() error { return ErrorToCore(cancel()) }, nil
}

func UnitOfWorkToCore(inner api.UnitOfWork) ports.UnitOfWork {
	if inner == nil {
		return nil
	}
	return apiUnitOfWorkAdapter{inner: inner}
}

type apiUnitOfWorkAdapter struct{ inner api.UnitOfWork }

func (a apiUnitOfWorkAdapter) Runs() ports.RunStore { return apiRunStoreAdapter{inner: a.inner.Runs()} }
func (a apiUnitOfWorkAdapter) Tasks() ports.TaskStore {
	return apiTaskStoreAdapter{inner: a.inner.Tasks()}
}

func (a apiUnitOfWorkAdapter) Events() ports.EventStore {
	return apiEventStoreAdapter{inner: a.inner.Events()}
}

func (a apiUnitOfWorkAdapter) Blackboard() ports.BlackboardReadWriter {
	return apiBlackboardStoreAdapter{inner: a.inner.Blackboard()}
}

func (a apiUnitOfWorkAdapter) MailboxOutbox() ports.MailboxOutboxStore {
	return apiMailboxOutboxStoreAdapter{inner: a.inner.MailboxOutbox()}
}

func (a apiUnitOfWorkAdapter) UserMessages() ports.UserMessageStore {
	return apiUserMessageStoreAdapter{inner: a.inner.UserMessages()}
}

func (a apiUnitOfWorkAdapter) Trace() ports.TraceStore {
	return apiTraceStoreAdapter{inner: a.inner.Trace()}
}

func (a apiUnitOfWorkAdapter) Leases() ports.LeaseStore {
	return apiLeaseStoreAdapter{inner: a.inner.Leases()}
}

func (a apiUnitOfWorkAdapter) Approvals() ports.ApprovalStore {
	return apiApprovalStoreAdapter{inner: a.inner.Approvals()}
}

func (a apiUnitOfWorkAdapter) ResumeTokens() ports.ResumeTokenStore {
	return apiResumeTokenStoreAdapter{inner: a.inner.ResumeTokens()}
}

func (a apiUnitOfWorkAdapter) ActionAttempts() ports.ActionAttemptStore {
	return apiActionAttemptStoreAdapter{inner: a.inner.ActionAttempts()}
}

func (a apiUnitOfWorkAdapter) AgentProfiles() ports.AgentProfileStore {
	return apiAgentProfileStoreAdapter{inner: a.inner.AgentProfiles()}
}

func (a apiUnitOfWorkAdapter) CapabilityCatalog() ports.CapabilityStore {
	return apiCapabilityStoreAdapter{inner: a.inner.CapabilityCatalog()}
}

func (a apiUnitOfWorkAdapter) UsageRecords() ports.UsageStore {
	return apiUsageStoreAdapter{inner: a.inner.UsageRecords()}
}

func (a apiUnitOfWorkAdapter) DeadLetters() ports.DeadLetterStore {
	return apiDeadLetterStoreAdapter{inner: a.inner.DeadLetters()}
}

func (a apiUnitOfWorkAdapter) Commit(ctx context.Context) error {
	return ErrorToCore(a.inner.Commit(ctx))
}

func (a apiUnitOfWorkAdapter) Rollback(ctx context.Context) error {
	return ErrorToCore(a.inner.Rollback(ctx))
}

type apiRunStoreAdapter struct{ inner api.RunStore }

func (a apiRunStoreAdapter) SaveRun(ctx context.Context, run model.Run) error {
	return ErrorToCore(a.inner.SaveRun(ctx, RunFromModel(run)))
}

func (a apiRunStoreAdapter) LoadRun(ctx context.Context, runID string) (model.Run, error) {
	run, err := a.inner.LoadRun(ctx, runID)
	if err != nil {
		return model.Run{}, ErrorToCore(err)
	}
	return RunToModel(run), nil
}

func (a apiRunStoreAdapter) ListRuns(ctx context.Context, sel model.RunSelector) ([]model.Run, error) {
	runs, err := a.inner.ListRuns(ctx, RunSelectorFromModel(sel))
	if err != nil {
		return nil, ErrorToCore(err)
	}
	return RunsToModel(runs), nil
}

type apiTaskStoreAdapter struct{ inner api.TaskStore }

func (a apiTaskStoreAdapter) SaveTask(ctx context.Context, task model.Task) error {
	return ErrorToCore(a.inner.SaveTask(ctx, TaskFromModel(task)))
}

func (a apiTaskStoreAdapter) LoadTask(ctx context.Context, runID, taskID string) (model.Task, error) {
	task, err := a.inner.LoadTask(ctx, runID, taskID)
	if err != nil {
		return model.Task{}, ErrorToCore(err)
	}
	return TaskToModel(task), nil
}

func (a apiTaskStoreAdapter) ListTasks(ctx context.Context, runID string) ([]model.Task, error) {
	tasks, err := a.inner.ListTasks(ctx, runID)
	if err != nil {
		return nil, ErrorToCore(err)
	}
	return TasksToModel(tasks), nil
}

type apiEventStoreAdapter struct{ inner api.EventStore }

func (a apiEventStoreAdapter) AppendEvent(ctx context.Context, event model.Event) error {
	return ErrorToCore(a.inner.AppendEvent(ctx, EventFromModel(event)))
}

func (a apiEventStoreAdapter) ListEvents(ctx context.Context, runID string) ([]model.Event, error) {
	events, err := a.inner.ListEvents(ctx, runID)
	if err != nil {
		return nil, ErrorToCore(err)
	}
	return EventsToModel(events), nil
}

func (a apiEventStoreAdapter) ListAfter(ctx context.Context, runID string, afterSeq uint64) ([]model.Event, error) {
	events, err := a.inner.ListAfter(ctx, runID, afterSeq)
	if err != nil {
		return nil, ErrorToCore(err)
	}
	return EventsToModel(events), nil
}

type apiTraceStoreAdapter struct{ inner api.TraceStore }

func (a apiTraceStoreAdapter) SaveTraceSpan(ctx context.Context, span model.TraceSpan) error {
	return ErrorToCore(a.inner.SaveTraceSpan(ctx, TraceSpanFromModel(span)))
}

func (a apiTraceStoreAdapter) ListTraceSpans(ctx context.Context, runID string) ([]model.TraceSpan, error) {
	spans, err := a.inner.ListTraceSpans(ctx, runID)
	if err != nil {
		return nil, ErrorToCore(err)
	}
	return TraceSpansToModel(spans), nil
}

func (a apiTraceStoreAdapter) LoadTraceSpan(ctx context.Context, spanID string) (model.TraceSpan, error) {
	updater, ok := a.inner.(api.TraceSpanUpdater)
	if !ok {
		return model.TraceSpan{}, fmt.Errorf("trace store does not implement TraceSpanUpdater: %w", model.ErrInvalidConfiguration)
	}
	span, err := updater.LoadTraceSpan(ctx, spanID)
	if err != nil {
		return model.TraceSpan{}, ErrorToCore(err)
	}
	return TraceSpanToModel(span), nil
}

func (a apiTraceStoreAdapter) UpdateTraceSpan(ctx context.Context, span model.TraceSpan) error {
	updater, ok := a.inner.(api.TraceSpanUpdater)
	if !ok {
		return fmt.Errorf("trace store does not implement TraceSpanUpdater: %w", model.ErrInvalidConfiguration)
	}
	return ErrorToCore(updater.UpdateTraceSpan(ctx, TraceSpanFromModel(span)))
}

type apiBlackboardStoreAdapter struct{ inner api.BlackboardReadWriter }

func (a apiBlackboardStoreAdapter) WriteItem(ctx context.Context, item model.BlackboardItem) error {
	return ErrorToCore(a.inner.WriteItem(ctx, BlackboardItemFromModel(item)))
}

func (a apiBlackboardStoreAdapter) SelectItems(ctx context.Context, runID string, selector model.BlackboardSelector) ([]model.BlackboardItem, error) {
	items, err := a.inner.SelectItems(ctx, runID, BlackboardSelectorFromModel(selector))
	if err != nil {
		return nil, ErrorToCore(err)
	}
	return BlackboardItemsToModel(items), nil
}

type apiMailboxOutboxStoreAdapter struct{ inner api.MailboxOutboxStore }

func (a apiMailboxOutboxStoreAdapter) QueueEnvelope(ctx context.Context, env model.TaskEnvelope) error {
	return ErrorToCore(a.inner.QueueEnvelope(ctx, TaskEnvelopeFromModel(env)))
}

func (a apiMailboxOutboxStoreAdapter) LoadEnvelope(ctx context.Context, envelopeID string) (model.TaskEnvelope, error) {
	env, err := a.inner.LoadEnvelope(ctx, envelopeID)
	if err != nil {
		return model.TaskEnvelope{}, ErrorToCore(err)
	}
	return TaskEnvelopeToModel(env), nil
}

func (a apiMailboxOutboxStoreAdapter) UpdateEnvelope(ctx context.Context, env model.TaskEnvelope) error {
	return ErrorToCore(a.inner.UpdateEnvelope(ctx, TaskEnvelopeFromModel(env)))
}

func (a apiMailboxOutboxStoreAdapter) ListEnvelopes(ctx context.Context, runID string) ([]model.TaskEnvelope, error) {
	envelopes, err := a.inner.ListEnvelopes(ctx, runID)
	if err != nil {
		return nil, ErrorToCore(err)
	}
	return TaskEnvelopesToModel(envelopes), nil
}

type apiUserMessageStoreAdapter struct{ inner api.UserMessageStore }

func (a apiUserMessageStoreAdapter) QueueMessage(ctx context.Context, message model.UserMessage) error {
	return ErrorToCore(a.inner.QueueMessage(ctx, UserMessageFromModel(message)))
}

func (a apiUserMessageStoreAdapter) LoadMessage(ctx context.Context, runID, messageID string) (model.UserMessage, error) {
	message, err := a.inner.LoadMessage(ctx, runID, messageID)
	if err != nil {
		return model.UserMessage{}, ErrorToCore(err)
	}
	return UserMessageToModel(message), nil
}

func (a apiUserMessageStoreAdapter) UpdateMessage(ctx context.Context, message model.UserMessage) error {
	return ErrorToCore(a.inner.UpdateMessage(ctx, UserMessageFromModel(message)))
}

func (a apiUserMessageStoreAdapter) ListMessages(ctx context.Context, runID string) ([]model.UserMessage, error) {
	messages, err := a.inner.ListMessages(ctx, runID)
	if err != nil {
		return nil, ErrorToCore(err)
	}
	return UserMessagesToModel(messages), nil
}

func (a apiUserMessageStoreAdapter) ListPendingFor(ctx context.Context, sel model.UserMessageSelector) ([]model.UserMessage, error) {
	messages, err := a.inner.ListPendingFor(ctx, UserMessageSelectorFromModel(sel))
	if err != nil {
		return nil, ErrorToCore(err)
	}
	return UserMessagesToModel(messages), nil
}

func (a apiUserMessageStoreAdapter) ListQueuedMessages(ctx context.Context) ([]model.UserMessage, error) {
	scanner, ok := a.inner.(api.UserMessageOutboxScanner)
	if !ok {
		return nil, fmt.Errorf("user message store does not support queued outbox scanning: %w", model.ErrInvalidConfiguration)
	}
	messages, err := scanner.ListQueuedMessages(ctx)
	if err != nil {
		return nil, ErrorToCore(err)
	}
	return UserMessagesToModel(messages), nil
}

type apiLeaseStoreAdapter struct{ inner api.LeaseStore }

func (a apiLeaseStoreAdapter) SaveLease(ctx context.Context, lease model.TaskExecutionLease) error {
	return ErrorToCore(a.inner.SaveLease(ctx, TaskExecutionLeaseFromModel(lease)))
}

func (a apiLeaseStoreAdapter) LoadLease(ctx context.Context, leaseID string) (model.TaskExecutionLease, error) {
	lease, err := a.inner.LoadLease(ctx, leaseID)
	if err != nil {
		return model.TaskExecutionLease{}, ErrorToCore(err)
	}
	return TaskExecutionLeaseToModel(lease), nil
}

func (a apiLeaseStoreAdapter) ActiveLeaseForTask(ctx context.Context, runID, taskID string) (model.TaskExecutionLease, bool, error) {
	lease, ok, err := a.inner.ActiveLeaseForTask(ctx, runID, taskID)
	if err != nil {
		return model.TaskExecutionLease{}, false, ErrorToCore(err)
	}
	return TaskExecutionLeaseToModel(lease), ok, nil
}

func (a apiLeaseStoreAdapter) AcquireWithExpectedVersion(ctx context.Context, lease model.TaskExecutionLease, expectedVersion uint64) (bool, error) {
	ok, err := a.inner.AcquireWithExpectedVersion(ctx, TaskExecutionLeaseFromModel(lease), expectedVersion)
	if err != nil {
		return false, ErrorToCore(err)
	}
	return ok, nil
}

func (a apiLeaseStoreAdapter) ExtendLease(ctx context.Context, leaseID string, workerID string, newExpiry time.Time) (bool, error) {
	ok, err := a.inner.ExtendLease(ctx, leaseID, workerID, newExpiry)
	if err != nil {
		return false, ErrorToCore(err)
	}
	return ok, nil
}

type apiApprovalStoreAdapter struct{ inner api.ApprovalStore }

func (a apiApprovalStoreAdapter) SaveApproval(ctx context.Context, approval model.ApprovalRequest) error {
	return ErrorToCore(a.inner.SaveApproval(ctx, ApprovalRequestFromModel(approval)))
}

func (a apiApprovalStoreAdapter) LoadApproval(ctx context.Context, approvalID string) (model.ApprovalRequest, error) {
	approval, err := a.inner.LoadApproval(ctx, approvalID)
	if err != nil {
		return model.ApprovalRequest{}, ErrorToCore(err)
	}
	return ApprovalRequestToModel(approval), nil
}

type apiResumeTokenStoreAdapter struct{ inner api.ResumeTokenStore }

func (a apiResumeTokenStoreAdapter) SaveResumeToken(ctx context.Context, token model.ResumeToken) error {
	return ErrorToCore(a.inner.SaveResumeToken(ctx, ResumeTokenFromModel(token)))
}

func (a apiResumeTokenStoreAdapter) LoadResumeToken(ctx context.Context, tokenID string) (model.ResumeToken, error) {
	token, err := a.inner.LoadResumeToken(ctx, tokenID)
	if err != nil {
		return model.ResumeToken{}, ErrorToCore(err)
	}
	return ResumeTokenToModel(token), nil
}

func (a apiResumeTokenStoreAdapter) ListPending(ctx context.Context, sel model.ResumeTokenSelector) ([]model.ResumeToken, error) {
	tokens, err := a.inner.ListPending(ctx, ResumeTokenSelectorFromModel(sel))
	if err != nil {
		return nil, ErrorToCore(err)
	}
	return ResumeTokensToModel(tokens), nil
}

type apiActionAttemptStoreAdapter struct{ inner api.ActionAttemptStore }

func (a apiActionAttemptStoreAdapter) SaveActionAttempt(ctx context.Context, attempt model.ActionAttempt) error {
	return ErrorToCore(a.inner.SaveActionAttempt(ctx, ActionAttemptFromModel(attempt)))
}

func (a apiActionAttemptStoreAdapter) LoadActionAttempt(ctx context.Context, attemptID string) (model.ActionAttempt, error) {
	attempt, err := a.inner.LoadActionAttempt(ctx, attemptID)
	if err != nil {
		return model.ActionAttempt{}, ErrorToCore(err)
	}
	return ActionAttemptToModel(attempt), nil
}

func (a apiActionAttemptStoreAdapter) LoadActionAttemptByIdempotencyKey(ctx context.Context, runID string, taskID string, toolName string, key string) (model.ActionAttempt, error) {
	attempt, err := a.inner.LoadActionAttemptByIdempotencyKey(ctx, runID, taskID, toolName, key)
	if err != nil {
		return model.ActionAttempt{}, ErrorToCore(err)
	}
	return ActionAttemptToModel(attempt), nil
}

type apiAgentProfileStoreAdapter struct{ inner api.AgentProfileStore }

func (a apiAgentProfileStoreAdapter) SaveAgentProfile(ctx context.Context, profile model.AgentProfile) error {
	return ErrorToCore(a.inner.SaveAgentProfile(ctx, AgentProfileFromModel(profile)))
}

func (a apiAgentProfileStoreAdapter) LoadAgentProfile(ctx context.Context, id string) (model.AgentProfile, error) {
	profile, err := a.inner.LoadAgentProfile(ctx, id)
	if err != nil {
		return model.AgentProfile{}, ErrorToCore(err)
	}
	return AgentProfileToModel(profile), nil
}

func (a apiAgentProfileStoreAdapter) ListAgentProfiles(ctx context.Context, sel model.AgentSelector) ([]model.AgentProfile, error) {
	profiles, err := a.inner.ListAgentProfiles(ctx, AgentSelectorFromModel(sel))
	if err != nil {
		return nil, ErrorToCore(err)
	}
	return AgentProfilesToModel(profiles), nil
}

type apiCapabilityStoreAdapter struct{ inner api.CapabilityStore }

func (a apiCapabilityStoreAdapter) SaveCapability(ctx context.Context, capability model.Capability) error {
	return ErrorToCore(a.inner.SaveCapability(ctx, CapabilityFromModel(capability)))
}

func (a apiCapabilityStoreAdapter) LoadCapability(ctx context.Context, name string, agentID string) (model.Capability, error) {
	capability, err := a.inner.LoadCapability(ctx, name, agentID)
	if err != nil {
		return model.Capability{}, ErrorToCore(err)
	}
	return CapabilityToModel(capability), nil
}

func (a apiCapabilityStoreAdapter) ListCapabilities(ctx context.Context, sel model.CapabilitySelector) ([]model.Capability, error) {
	caps, err := a.inner.ListCapabilities(ctx, CapabilitySelectorFromModel(sel))
	if err != nil {
		return nil, ErrorToCore(err)
	}
	return CapabilitiesToModel(caps), nil
}

type apiUsageStoreAdapter struct{ inner api.UsageStore }

func (a apiUsageStoreAdapter) AppendUsage(ctx context.Context, rec model.UsageRecord) error {
	return ErrorToCore(a.inner.AppendUsage(ctx, UsageRecordFromModel(rec)))
}

func (a apiUsageStoreAdapter) QueryUsage(ctx context.Context, sel model.UsageSelector) ([]model.UsageRecord, error) {
	records, err := a.inner.QueryUsage(ctx, UsageSelectorFromModel(sel))
	if err != nil {
		return nil, ErrorToCore(err)
	}
	return UsageRecordsToModel(records), nil
}

func (a apiUsageStoreAdapter) SumCredits(ctx context.Context, sel model.UsageSelector) (int64, error) {
	sum, err := a.inner.SumCredits(ctx, UsageSelectorFromModel(sel))
	if err != nil {
		return 0, ErrorToCore(err)
	}
	return sum, nil
}

type apiDeadLetterStoreAdapter struct{ inner api.DeadLetterStore }

func (a apiDeadLetterStoreAdapter) AppendDeadLetter(ctx context.Context, entry model.DeadLetterEntry) error {
	return ErrorToCore(a.inner.AppendDeadLetter(ctx, DeadLetterEntryFromModel(entry)))
}

func (a apiDeadLetterStoreAdapter) ListDeadLetters(ctx context.Context, sel model.DeadLetterSelector) ([]model.DeadLetterEntry, error) {
	entries, err := a.inner.ListDeadLetters(ctx, DeadLetterSelectorFromModel(sel))
	if err != nil {
		return nil, ErrorToCore(err)
	}
	return DeadLetterEntriesToModel(entries), nil
}

func (a apiDeadLetterStoreAdapter) Requeue(ctx context.Context, deadLetterID string) error {
	return ErrorToCore(a.inner.Requeue(ctx, deadLetterID))
}

func StoreProviderFromCore(inner core.StoreProvider) api.StoreProvider {
	if inner == nil {
		return nil
	}
	base := coreStoreProviderAdapter{inner: inner}
	if subscriber, ok := inner.(core.BlackboardSubscriber); ok {
		return coreStoreProviderSubscriberAdapter{coreStoreProviderAdapter: base, subscriber: subscriber}
	}
	return base
}

type coreStoreProviderAdapter struct{ inner core.StoreProvider }

func (a coreStoreProviderAdapter) Begin(ctx context.Context) (api.UnitOfWork, error) {
	uow, err := a.inner.Begin(ctx)
	if err != nil {
		return nil, ErrorToAPI(err)
	}
	return UnitOfWorkFromCore(uow), nil
}

type coreStoreProviderSubscriberAdapter struct {
	coreStoreProviderAdapter
	subscriber core.BlackboardSubscriber
}

func (a coreStoreProviderSubscriberAdapter) Subscribe(ctx context.Context, runID string, selector api.BlackboardSelector) (<-chan api.BlackboardItem, func() error, error) {
	items, cancel, err := a.subscriber.Subscribe(ctx, runID, BlackboardSelectorToModel(selector))
	if err != nil {
		return nil, nil, ErrorToAPI(err)
	}
	out := make(chan api.BlackboardItem)
	go func() {
		defer close(out)
		for item := range items {
			out <- BlackboardItemFromModel(item)
		}
	}()
	return out, func() error { return ErrorToAPI(cancel()) }, nil
}

func UnitOfWorkFromCore(inner core.UnitOfWork) api.UnitOfWork {
	if inner == nil {
		return nil
	}
	return coreUnitOfWorkAdapter{inner: inner}
}

type coreUnitOfWorkAdapter struct{ inner core.UnitOfWork }

func (a coreUnitOfWorkAdapter) Runs() api.RunStore { return coreRunStoreAdapter{inner: a.inner.Runs()} }
func (a coreUnitOfWorkAdapter) Tasks() api.TaskStore {
	return coreTaskStoreAdapter{inner: a.inner.Tasks()}
}

func (a coreUnitOfWorkAdapter) Events() api.EventStore {
	return coreEventStoreAdapter{inner: a.inner.Events()}
}

func (a coreUnitOfWorkAdapter) Blackboard() api.BlackboardReadWriter {
	return coreBlackboardStoreAdapter{inner: a.inner.Blackboard()}
}

func (a coreUnitOfWorkAdapter) MailboxOutbox() api.MailboxOutboxStore {
	return coreMailboxOutboxStoreAdapter{inner: a.inner.MailboxOutbox()}
}

func (a coreUnitOfWorkAdapter) UserMessages() api.UserMessageStore {
	return coreUserMessageStoreAdapter{inner: a.inner.UserMessages()}
}

func (a coreUnitOfWorkAdapter) Trace() api.TraceStore {
	return coreTraceStoreAdapter{inner: a.inner.Trace()}
}

func (a coreUnitOfWorkAdapter) Leases() api.LeaseStore {
	return coreLeaseStoreAdapter{inner: a.inner.Leases()}
}

func (a coreUnitOfWorkAdapter) Approvals() api.ApprovalStore {
	return coreApprovalStoreAdapter{inner: a.inner.Approvals()}
}

func (a coreUnitOfWorkAdapter) ResumeTokens() api.ResumeTokenStore {
	return coreResumeTokenStoreAdapter{inner: a.inner.ResumeTokens()}
}

func (a coreUnitOfWorkAdapter) ActionAttempts() api.ActionAttemptStore {
	return coreActionAttemptStoreAdapter{inner: a.inner.ActionAttempts()}
}

func (a coreUnitOfWorkAdapter) AgentProfiles() api.AgentProfileStore {
	return coreAgentProfileStoreAdapter{inner: a.inner.AgentProfiles()}
}

func (a coreUnitOfWorkAdapter) CapabilityCatalog() api.CapabilityStore {
	return coreCapabilityStoreAdapter{inner: a.inner.CapabilityCatalog()}
}

func (a coreUnitOfWorkAdapter) UsageRecords() api.UsageStore {
	return coreUsageStoreAdapter{inner: a.inner.UsageRecords()}
}

func (a coreUnitOfWorkAdapter) DeadLetters() api.DeadLetterStore {
	return coreDeadLetterStoreAdapter{inner: a.inner.DeadLetters()}
}

func (a coreUnitOfWorkAdapter) Commit(ctx context.Context) error {
	return ErrorToAPI(a.inner.Commit(ctx))
}

func (a coreUnitOfWorkAdapter) Rollback(ctx context.Context) error {
	return ErrorToAPI(a.inner.Rollback(ctx))
}

type coreRunStoreAdapter struct{ inner core.RunStore }

func (a coreRunStoreAdapter) SaveRun(ctx context.Context, run api.Run) error {
	return ErrorToAPI(a.inner.SaveRun(ctx, RunToModel(run)))
}

func (a coreRunStoreAdapter) LoadRun(ctx context.Context, runID string) (api.Run, error) {
	run, err := a.inner.LoadRun(ctx, runID)
	if err != nil {
		return api.Run{}, ErrorToAPI(err)
	}
	return RunFromModel(run), nil
}

func (a coreRunStoreAdapter) ListRuns(ctx context.Context, sel api.RunSelector) ([]api.Run, error) {
	runs, err := a.inner.ListRuns(ctx, RunSelectorToModel(sel))
	if err != nil {
		return nil, ErrorToAPI(err)
	}
	return RunsFromModel(runs), nil
}

type coreTaskStoreAdapter struct{ inner core.TaskStore }

func (a coreTaskStoreAdapter) SaveTask(ctx context.Context, task api.Task) error {
	return ErrorToAPI(a.inner.SaveTask(ctx, TaskToModel(task)))
}

func (a coreTaskStoreAdapter) LoadTask(ctx context.Context, runID, taskID string) (api.Task, error) {
	task, err := a.inner.LoadTask(ctx, runID, taskID)
	if err != nil {
		return api.Task{}, ErrorToAPI(err)
	}
	return TaskFromModel(task), nil
}

func (a coreTaskStoreAdapter) ListTasks(ctx context.Context, runID string) ([]api.Task, error) {
	tasks, err := a.inner.ListTasks(ctx, runID)
	if err != nil {
		return nil, ErrorToAPI(err)
	}
	return TasksFromModel(tasks), nil
}

type coreEventStoreAdapter struct{ inner core.EventStore }

func (a coreEventStoreAdapter) AppendEvent(ctx context.Context, event api.Event) error {
	return ErrorToAPI(a.inner.AppendEvent(ctx, EventToModel(event)))
}

func (a coreEventStoreAdapter) ListEvents(ctx context.Context, runID string) ([]api.Event, error) {
	events, err := a.inner.ListEvents(ctx, runID)
	if err != nil {
		return nil, ErrorToAPI(err)
	}
	return EventsFromModel(events), nil
}

func (a coreEventStoreAdapter) ListAfter(ctx context.Context, runID string, afterSeq uint64) ([]api.Event, error) {
	events, err := a.inner.ListAfter(ctx, runID, afterSeq)
	if err != nil {
		return nil, ErrorToAPI(err)
	}
	return EventsFromModel(events), nil
}

type coreTraceStoreAdapter struct{ inner core.TraceStore }

func (a coreTraceStoreAdapter) SaveTraceSpan(ctx context.Context, span api.TraceSpan) error {
	return ErrorToAPI(a.inner.SaveTraceSpan(ctx, TraceSpanToModel(span)))
}

func (a coreTraceStoreAdapter) ListTraceSpans(ctx context.Context, runID string) ([]api.TraceSpan, error) {
	spans, err := a.inner.ListTraceSpans(ctx, runID)
	if err != nil {
		return nil, ErrorToAPI(err)
	}
	return TraceSpansFromModel(spans), nil
}

func (a coreTraceStoreAdapter) LoadTraceSpan(ctx context.Context, spanID string) (api.TraceSpan, error) {
	updater, ok := a.inner.(ports.TraceSpanUpdater)
	if !ok {
		return api.TraceSpan{}, ErrorToAPI(fmt.Errorf("trace store does not implement TraceSpanUpdater: %w", model.ErrInvalidConfiguration))
	}
	span, err := updater.LoadTraceSpan(ctx, spanID)
	if err != nil {
		return api.TraceSpan{}, ErrorToAPI(err)
	}
	return TraceSpanFromModel(span), nil
}

func (a coreTraceStoreAdapter) UpdateTraceSpan(ctx context.Context, span api.TraceSpan) error {
	updater, ok := a.inner.(ports.TraceSpanUpdater)
	if !ok {
		return ErrorToAPI(fmt.Errorf("trace store does not implement TraceSpanUpdater: %w", model.ErrInvalidConfiguration))
	}
	return ErrorToAPI(updater.UpdateTraceSpan(ctx, TraceSpanToModel(span)))
}

type coreBlackboardStoreAdapter struct{ inner core.BlackboardReadWriter }

func (a coreBlackboardStoreAdapter) WriteItem(ctx context.Context, item api.BlackboardItem) error {
	return ErrorToAPI(a.inner.WriteItem(ctx, BlackboardItemToModel(item)))
}

func (a coreBlackboardStoreAdapter) SelectItems(ctx context.Context, runID string, selector api.BlackboardSelector) ([]api.BlackboardItem, error) {
	items, err := a.inner.SelectItems(ctx, runID, BlackboardSelectorToModel(selector))
	if err != nil {
		return nil, ErrorToAPI(err)
	}
	return BlackboardItemsFromModel(items), nil
}

type coreMailboxOutboxStoreAdapter struct{ inner core.MailboxOutboxStore }

func (a coreMailboxOutboxStoreAdapter) QueueEnvelope(ctx context.Context, env api.TaskEnvelope) error {
	return ErrorToAPI(a.inner.QueueEnvelope(ctx, TaskEnvelopeToModel(env)))
}

func (a coreMailboxOutboxStoreAdapter) LoadEnvelope(ctx context.Context, envelopeID string) (api.TaskEnvelope, error) {
	env, err := a.inner.LoadEnvelope(ctx, envelopeID)
	if err != nil {
		return api.TaskEnvelope{}, ErrorToAPI(err)
	}
	return TaskEnvelopeFromModel(env), nil
}

func (a coreMailboxOutboxStoreAdapter) UpdateEnvelope(ctx context.Context, env api.TaskEnvelope) error {
	return ErrorToAPI(a.inner.UpdateEnvelope(ctx, TaskEnvelopeToModel(env)))
}

func (a coreMailboxOutboxStoreAdapter) ListEnvelopes(ctx context.Context, runID string) ([]api.TaskEnvelope, error) {
	envelopes, err := a.inner.ListEnvelopes(ctx, runID)
	if err != nil {
		return nil, ErrorToAPI(err)
	}
	return TaskEnvelopesFromModel(envelopes), nil
}

type coreUserMessageStoreAdapter struct{ inner core.UserMessageStore }

func (a coreUserMessageStoreAdapter) QueueMessage(ctx context.Context, message api.UserMessage) error {
	return ErrorToAPI(a.inner.QueueMessage(ctx, UserMessageToModel(message)))
}

func (a coreUserMessageStoreAdapter) LoadMessage(ctx context.Context, runID, messageID string) (api.UserMessage, error) {
	message, err := a.inner.LoadMessage(ctx, runID, messageID)
	if err != nil {
		return api.UserMessage{}, ErrorToAPI(err)
	}
	return UserMessageFromModel(message), nil
}

func (a coreUserMessageStoreAdapter) UpdateMessage(ctx context.Context, message api.UserMessage) error {
	return ErrorToAPI(a.inner.UpdateMessage(ctx, UserMessageToModel(message)))
}

func (a coreUserMessageStoreAdapter) ListMessages(ctx context.Context, runID string) ([]api.UserMessage, error) {
	messages, err := a.inner.ListMessages(ctx, runID)
	if err != nil {
		return nil, ErrorToAPI(err)
	}
	return UserMessagesFromModel(messages), nil
}

func (a coreUserMessageStoreAdapter) ListPendingFor(ctx context.Context, sel api.UserMessageSelector) ([]api.UserMessage, error) {
	messages, err := a.inner.ListPendingFor(ctx, UserMessageSelectorToModel(sel))
	if err != nil {
		return nil, ErrorToAPI(err)
	}
	return UserMessagesFromModel(messages), nil
}

func (a coreUserMessageStoreAdapter) ListQueuedMessages(ctx context.Context) ([]api.UserMessage, error) {
	scanner, ok := a.inner.(core.UserMessageOutboxScanner)
	if !ok {
		return nil, ErrorToAPI(fmt.Errorf("user message store does not support queued outbox scanning: %w", model.ErrInvalidConfiguration))
	}
	messages, err := scanner.ListQueuedMessages(ctx)
	if err != nil {
		return nil, ErrorToAPI(err)
	}
	return UserMessagesFromModel(messages), nil
}

type coreLeaseStoreAdapter struct{ inner core.LeaseStore }

func (a coreLeaseStoreAdapter) SaveLease(ctx context.Context, lease api.TaskExecutionLease) error {
	return ErrorToAPI(a.inner.SaveLease(ctx, TaskExecutionLeaseToModel(lease)))
}

func (a coreLeaseStoreAdapter) LoadLease(ctx context.Context, leaseID string) (api.TaskExecutionLease, error) {
	lease, err := a.inner.LoadLease(ctx, leaseID)
	if err != nil {
		return api.TaskExecutionLease{}, ErrorToAPI(err)
	}
	return TaskExecutionLeaseFromModel(lease), nil
}

func (a coreLeaseStoreAdapter) ActiveLeaseForTask(ctx context.Context, runID, taskID string) (api.TaskExecutionLease, bool, error) {
	lease, ok, err := a.inner.ActiveLeaseForTask(ctx, runID, taskID)
	if err != nil {
		return api.TaskExecutionLease{}, false, ErrorToAPI(err)
	}
	return TaskExecutionLeaseFromModel(lease), ok, nil
}

func (a coreLeaseStoreAdapter) AcquireWithExpectedVersion(ctx context.Context, lease api.TaskExecutionLease, expectedVersion uint64) (bool, error) {
	ok, err := a.inner.AcquireWithExpectedVersion(ctx, TaskExecutionLeaseToModel(lease), expectedVersion)
	if err != nil {
		return false, ErrorToAPI(err)
	}
	return ok, nil
}

func (a coreLeaseStoreAdapter) ExtendLease(ctx context.Context, leaseID string, workerID string, newExpiry time.Time) (bool, error) {
	ok, err := a.inner.ExtendLease(ctx, leaseID, workerID, newExpiry)
	if err != nil {
		return false, ErrorToAPI(err)
	}
	return ok, nil
}

type coreApprovalStoreAdapter struct{ inner core.ApprovalStore }

func (a coreApprovalStoreAdapter) SaveApproval(ctx context.Context, approval api.ApprovalRequest) error {
	return ErrorToAPI(a.inner.SaveApproval(ctx, ApprovalRequestToModel(approval)))
}

func (a coreApprovalStoreAdapter) LoadApproval(ctx context.Context, approvalID string) (api.ApprovalRequest, error) {
	approval, err := a.inner.LoadApproval(ctx, approvalID)
	if err != nil {
		return api.ApprovalRequest{}, ErrorToAPI(err)
	}
	return ApprovalRequestFromModel(approval), nil
}

type coreResumeTokenStoreAdapter struct{ inner core.ResumeTokenStore }

func (a coreResumeTokenStoreAdapter) SaveResumeToken(ctx context.Context, token api.ResumeToken) error {
	return ErrorToAPI(a.inner.SaveResumeToken(ctx, ResumeTokenToModel(token)))
}

func (a coreResumeTokenStoreAdapter) LoadResumeToken(ctx context.Context, tokenID string) (api.ResumeToken, error) {
	token, err := a.inner.LoadResumeToken(ctx, tokenID)
	if err != nil {
		return api.ResumeToken{}, ErrorToAPI(err)
	}
	return ResumeTokenFromModel(token), nil
}

func (a coreResumeTokenStoreAdapter) ListPending(ctx context.Context, sel api.ResumeTokenSelector) ([]api.ResumeToken, error) {
	tokens, err := a.inner.ListPending(ctx, ResumeTokenSelectorToModel(sel))
	if err != nil {
		return nil, ErrorToAPI(err)
	}
	return ResumeTokensFromModel(tokens), nil
}

type coreActionAttemptStoreAdapter struct{ inner core.ActionAttemptStore }

func (a coreActionAttemptStoreAdapter) SaveActionAttempt(ctx context.Context, attempt api.ActionAttempt) error {
	return ErrorToAPI(a.inner.SaveActionAttempt(ctx, ActionAttemptToModel(attempt)))
}

func (a coreActionAttemptStoreAdapter) LoadActionAttempt(ctx context.Context, attemptID string) (api.ActionAttempt, error) {
	attempt, err := a.inner.LoadActionAttempt(ctx, attemptID)
	if err != nil {
		return api.ActionAttempt{}, ErrorToAPI(err)
	}
	return ActionAttemptFromModel(attempt), nil
}

func (a coreActionAttemptStoreAdapter) LoadActionAttemptByIdempotencyKey(ctx context.Context, runID string, taskID string, toolName string, key string) (api.ActionAttempt, error) {
	attempt, err := a.inner.LoadActionAttemptByIdempotencyKey(ctx, runID, taskID, toolName, key)
	if err != nil {
		return api.ActionAttempt{}, ErrorToAPI(err)
	}
	return ActionAttemptFromModel(attempt), nil
}

type coreAgentProfileStoreAdapter struct{ inner core.AgentProfileStore }

func (a coreAgentProfileStoreAdapter) SaveAgentProfile(ctx context.Context, profile api.AgentProfile) error {
	return ErrorToAPI(a.inner.SaveAgentProfile(ctx, AgentProfileToModel(profile)))
}

func (a coreAgentProfileStoreAdapter) LoadAgentProfile(ctx context.Context, id string) (api.AgentProfile, error) {
	profile, err := a.inner.LoadAgentProfile(ctx, id)
	if err != nil {
		return api.AgentProfile{}, ErrorToAPI(err)
	}
	return AgentProfileFromModel(profile), nil
}

func (a coreAgentProfileStoreAdapter) ListAgentProfiles(ctx context.Context, sel api.AgentSelector) ([]api.AgentProfile, error) {
	profiles, err := a.inner.ListAgentProfiles(ctx, AgentSelectorToModel(sel))
	if err != nil {
		return nil, ErrorToAPI(err)
	}
	return AgentProfilesFromModel(profiles), nil
}

type coreCapabilityStoreAdapter struct{ inner core.CapabilityStore }

func (a coreCapabilityStoreAdapter) SaveCapability(ctx context.Context, capability api.Capability) error {
	return ErrorToAPI(a.inner.SaveCapability(ctx, CapabilityToModel(capability)))
}

func (a coreCapabilityStoreAdapter) LoadCapability(ctx context.Context, name string, agentID string) (api.Capability, error) {
	capability, err := a.inner.LoadCapability(ctx, name, agentID)
	if err != nil {
		return api.Capability{}, ErrorToAPI(err)
	}
	return CapabilityFromModel(capability), nil
}

func (a coreCapabilityStoreAdapter) ListCapabilities(ctx context.Context, sel api.CapabilitySelector) ([]api.Capability, error) {
	caps, err := a.inner.ListCapabilities(ctx, CapabilitySelectorToModel(sel))
	if err != nil {
		return nil, ErrorToAPI(err)
	}
	return CapabilitiesFromModel(caps), nil
}

type coreUsageStoreAdapter struct{ inner core.UsageStore }

func (a coreUsageStoreAdapter) AppendUsage(ctx context.Context, rec api.UsageRecord) error {
	return ErrorToAPI(a.inner.AppendUsage(ctx, UsageRecordToModel(rec)))
}

func (a coreUsageStoreAdapter) QueryUsage(ctx context.Context, sel api.UsageSelector) ([]api.UsageRecord, error) {
	records, err := a.inner.QueryUsage(ctx, UsageSelectorToModel(sel))
	if err != nil {
		return nil, ErrorToAPI(err)
	}
	return UsageRecordsFromModel(records), nil
}

func (a coreUsageStoreAdapter) SumCredits(ctx context.Context, sel api.UsageSelector) (int64, error) {
	sum, err := a.inner.SumCredits(ctx, UsageSelectorToModel(sel))
	if err != nil {
		return 0, ErrorToAPI(err)
	}
	return sum, nil
}

type coreDeadLetterStoreAdapter struct{ inner core.DeadLetterStore }

func (a coreDeadLetterStoreAdapter) AppendDeadLetter(ctx context.Context, entry api.DeadLetterEntry) error {
	return ErrorToAPI(a.inner.AppendDeadLetter(ctx, DeadLetterEntryToModel(entry)))
}

func (a coreDeadLetterStoreAdapter) ListDeadLetters(ctx context.Context, sel api.DeadLetterSelector) ([]api.DeadLetterEntry, error) {
	entries, err := a.inner.ListDeadLetters(ctx, DeadLetterSelectorToModel(sel))
	if err != nil {
		return nil, ErrorToAPI(err)
	}
	return DeadLetterEntriesFromModel(entries), nil
}

func (a coreDeadLetterStoreAdapter) Requeue(ctx context.Context, deadLetterID string) error {
	return ErrorToAPI(a.inner.Requeue(ctx, deadLetterID))
}
