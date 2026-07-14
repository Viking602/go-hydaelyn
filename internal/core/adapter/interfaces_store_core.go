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

func (a coreStoreProviderAdapter) Capabilities(ctx context.Context) (api.StoreCapabilities, error) {
	reporter, ok := a.inner.(core.CapabilityReporter)
	if !ok {
		return api.DefaultStoreCapabilities(), nil
	}
	capabilities, err := reporter.Capabilities(ctx)
	if err != nil {
		return api.StoreCapabilities{}, ErrorToAPI(err)
	}
	return api.StoreCapabilities{
		SupportsTransactions:        capabilities.SupportsTransactions,
		SupportsBlackboardSubscribe: capabilities.SupportsBlackboardSubscribe,
		SupportsListPending:         capabilities.SupportsListPending,
		SupportsConcurrentWriters:   capabilities.SupportsConcurrentWriters,
		SupportsDeadLetterRequeue:   capabilities.SupportsDeadLetterRequeue,
	}, nil
}

func (a coreStoreProviderAdapter) Close(ctx context.Context) error {
	closer, ok := a.inner.(core.ProviderCloser)
	if !ok {
		return nil
	}
	return ErrorToAPI(closer.Close(ctx))
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

func (a coreUnitOfWorkAdapter) Handoffs() api.HandoffStore {
	return coreHandoffStoreAdapter{inner: a.inner.Handoffs()}
}

func (a coreUnitOfWorkAdapter) TeamStates() api.TeamStateStore {
	return coreTeamStateStoreAdapter{inner: a.inner.TeamStates()}
}

func (a coreUnitOfWorkAdapter) AgentInstances() api.AgentInstanceStore {
	return coreAgentInstanceStoreAdapter{inner: a.inner.AgentInstances()}
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

type coreHandoffStoreAdapter struct{ inner core.HandoffStore }

func (a coreHandoffStoreAdapter) SaveHandoff(ctx context.Context, record api.HandoffRecord) error {
	return ErrorToAPI(a.inner.SaveHandoff(ctx, HandoffRecordToModel(record)))
}

func (a coreHandoffStoreAdapter) LoadHandoff(ctx context.Context, runID, handoffID string) (api.HandoffRecord, error) {
	record, err := a.inner.LoadHandoff(ctx, runID, handoffID)
	if err != nil {
		return api.HandoffRecord{}, ErrorToAPI(err)
	}
	return HandoffRecordFromModel(record), nil
}

func (a coreHandoffStoreAdapter) ListHandoffs(ctx context.Context, sel api.HandoffSelector) ([]api.HandoffRecord, error) {
	records, err := a.inner.ListHandoffs(ctx, HandoffSelectorToModel(sel))
	if err != nil {
		return nil, ErrorToAPI(err)
	}
	return HandoffRecordsFromModel(records), nil
}

type coreTeamStateStoreAdapter struct{ inner core.TeamStateStore }

func (a coreTeamStateStoreAdapter) SaveTeamState(ctx context.Context, record api.TeamStateRecord) error {
	return ErrorToAPI(a.inner.SaveTeamState(ctx, TeamStateRecordToModel(record)))
}

func (a coreTeamStateStoreAdapter) LoadTeamState(ctx context.Context, runID string) (api.TeamStateRecord, error) {
	record, err := a.inner.LoadTeamState(ctx, runID)
	if err != nil {
		return api.TeamStateRecord{}, ErrorToAPI(err)
	}
	return TeamStateRecordFromModel(record), nil
}

type coreAgentInstanceStoreAdapter struct{ inner core.AgentInstanceStore }

func (a coreAgentInstanceStoreAdapter) SaveAgentInstance(ctx context.Context, record api.AgentInstanceRecord) error {
	return ErrorToAPI(a.inner.SaveAgentInstance(ctx, AgentInstanceRecordToModel(record)))
}

func (a coreAgentInstanceStoreAdapter) LoadAgentInstance(ctx context.Context, id string) (api.AgentInstanceRecord, error) {
	record, err := a.inner.LoadAgentInstance(ctx, id)
	if err != nil {
		return api.AgentInstanceRecord{}, ErrorToAPI(err)
	}
	return AgentInstanceRecordFromModel(record), nil
}

func (a coreAgentInstanceStoreAdapter) ListAgentInstances(ctx context.Context, sel api.AgentInstanceSelector) ([]api.AgentInstanceRecord, error) {
	records, err := a.inner.ListAgentInstances(ctx, AgentInstanceSelectorToModel(sel))
	if err != nil {
		return nil, ErrorToAPI(err)
	}
	return AgentInstanceRecordsFromModel(records), nil
}
